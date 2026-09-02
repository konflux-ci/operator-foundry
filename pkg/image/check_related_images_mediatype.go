/*
Copyright 2026.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package image

import (
	"context"
	"log/slog"
	"sync"
	"sync/atomic"

	"github.com/google/go-containerregistry/pkg/v1/types"
	"github.com/konflux-ci/operator-foundry/pkg/ocp"
	"golang.org/x/sync/errgroup"
)

// first OCP version that supports OCI MediaType natively
const ociNativeMinOCPVersion = "4.21"
const goroutinesLimit = 50

// MediaTypeCheckBatchResult holds the outcome of the MediaType and OCP version compatibility check for multiple relatedImages
type MediaTypeCheckBatchResult struct {
	Passed               bool
	WrongMediaTypeImages []string
	BrokenImages         []string
}

// mediaTypeCheckSingleResult holds the outcome of the MediaType and OCP version compatibility check for one relatedImage
type mediaTypeCheckSingleResult struct {
	Passed              bool
	WrongMediaTypeImage string
	BrokenImage         string
}

// CheckRelatedImagesMediaType checks whether all related images
// are compliant with the constraint that OCI MediaType is not present for OCP < v4.21.
// If the ocp version is >= v4.21, this check is skipped, otherwise we pull the MediaType,
// check the values, and if there is DockerManifestList we also check its Image manifests
//
// Images are checked concurrently (up to 50 goroutines). Images that fail to fetch
// are retried once after the first pass completes.
//
// Returns (nil, err) if the OCP version is malformed or comparison fails.
// Returns a MediaTypeCheckBatchResult with Passed=true if all images are compliant or
// the OCP version >= 4.21. Returns a MediaTypeCheckBatchResult with Passed=false and
// WrongMediaTypeImages/BrokenImages populated for every non-compliant or unreachable image.
func CheckRelatedImagesMediaType(
	ctx context.Context, ocpVersion string, relatedImages []string, fetcher ManifestFetcher,
) (*MediaTypeCheckBatchResult, error) {
	// skip the rest of the check if the ocpVersion>=4.21
	gte, err := ocp.OCPVersionGTE(ocpVersion, ociNativeMinOCPVersion)
	if err != nil {
		return nil, err
	}
	if gte {
		slog.Info("ocp version supports OCI media types natively, skipping check",
			"ocpVersion", ocpVersion,
		)
		return &MediaTypeCheckBatchResult{Passed: true}, nil
	}

	relatedImages = deduplicateImages(relatedImages)

	// wrap an existing context (creates a child context, inherits parents cancellation and adds its own)
	g, parallelCtx := errgroup.WithContext(ctx)
	// limit concurrent registry fetches to avoid overwhelming the registry
	g.SetLimit(goroutinesLimit)

	var wrongMediaTypeImages []string
	var mu sync.Mutex
	var brokenImages []string
	var processed atomic.Int64
	total := len(relatedImages)

	for _, relatedImage := range relatedImages {
		g.Go(func() error {
			result := checkRelatedImageMediaType(
				parallelCtx, relatedImage, fetcher, ocpVersion)
			collectResult(result, &mu, &brokenImages, &wrongMediaTypeImages)
			if count := processed.Add(1); count%1000 == 0 {
				slog.Info("mediatype check progress", "checked", count, "total", total)
			}
			return nil
		})
	}
	if err := g.Wait(); err != nil {
		return nil, err
	}

	if ctx.Err() != nil {
		return nil, context.Cause(ctx)
	}

	// Retry - check the inaccessible images once again
	if len(brokenImages) > 0 {
		slog.Info("retrying broken images fetch", "count", len(brokenImages))
		retryImages := brokenImages
		brokenImages = nil // reset

		// new context for the brokenImages retry pass
		g2, retryCtx := errgroup.WithContext(ctx)
		g2.SetLimit(max(goroutinesLimit/2, 1))

		for _, relatedImage := range retryImages {
			g2.Go(func() error {
				result := checkRelatedImageMediaType(
					retryCtx, relatedImage, fetcher, ocpVersion)
				collectResult(result, &mu, &brokenImages, &wrongMediaTypeImages)
				return nil
			})
		}
		if err := g2.Wait(); err != nil {
			return nil, err
		}
	}
	if len(wrongMediaTypeImages) > 0 || len(brokenImages) > 0 {
		return &MediaTypeCheckBatchResult{Passed: false, WrongMediaTypeImages: wrongMediaTypeImages, BrokenImages: brokenImages}, nil
	}

	return &MediaTypeCheckBatchResult{Passed: true}, nil
}

// checkRelatedImageMediaType checks a single image's mediaType compatibility. Safe to call from multiple goroutines.
func checkRelatedImageMediaType(ctx context.Context, relatedImage string, fetcher ManifestFetcher, ocpVersion string) mediaTypeCheckSingleResult {
	imageMediaTypeDescriptor, err := fetcher.FetchManifest(ctx, relatedImage)
	if err != nil {
		slog.Warn(
			"failed to fetch media type from related image",
			"error", err,
			"ocpVersion", ocpVersion,
			"relatedImage", relatedImage,
		)
		return mediaTypeCheckSingleResult{
			Passed:      false,
			BrokenImage: relatedImage,
		}
	}

	switch imageMediaTypeDescriptor.MediaType {

	// application/vnd.docker.distribution.manifest.v2+json
	// Docker V2 — compatible, nothing to do
	case types.DockerManifestSchema2:
		return mediaTypeCheckSingleResult{Passed: true}

	// application/vnd.oci.image.manifest.v1+json, application/vnd.oci.image.index.v1+json
	// OCI — incompatible with OCP < 4.21
	case types.OCIManifestSchema1, types.OCIImageIndex:
		slog.Warn(
			"OCI media type is not supported by ocp version < 4.21, rebuild or re-push the image using Docker V2 manifests",
			"ocpVersion", ocpVersion,
			"relatedImage", relatedImage,
			"MediaType", imageMediaTypeDescriptor.MediaType,
		)
		return mediaTypeCheckSingleResult{Passed: false, WrongMediaTypeImage: relatedImage}

	// application/vnd.docker.distribution.manifest.list.v2+json
	// Multi-arch — need to check each inner manifest
	case types.DockerManifestList:
		idx, err := imageMediaTypeDescriptor.ImageIndex()
		if err != nil {
			slog.Warn(
				"failed to read manifest list as index image",
				"error", err,
				"ocpVersion", ocpVersion,
				"relatedImage", relatedImage,
				"MediaType", imageMediaTypeDescriptor.MediaType,
			)
			return mediaTypeCheckSingleResult{Passed: false, BrokenImage: relatedImage}
		}
		idxManifest, err := idx.IndexManifest()
		if err != nil {
			slog.Warn(
				"failed to parse index manifest",
				"error", err,
				"ocpVersion", ocpVersion,
				"relatedImage", relatedImage,
				"MediaType", imageMediaTypeDescriptor.MediaType,
			)
			return mediaTypeCheckSingleResult{Passed: false, BrokenImage: relatedImage}
		}
		for _, manifest := range idxManifest.Manifests {
			if manifest.MediaType != types.DockerManifestSchema2 {
				slog.Warn(
					"inner manifest media type is incompatible with ocp version < 4.21, rebuild or re-push the image using Docker V2 manifests",
					"ocpVersion", ocpVersion,
					"relatedImage", relatedImage,
					"MediaType", manifest.MediaType,
				)
				return mediaTypeCheckSingleResult{Passed: false, WrongMediaTypeImage: relatedImage}
			}
		}
	default:
		slog.Warn("unexpected media type, treating as incompatible",
			"ocpVersion", ocpVersion,
			"relatedImage", relatedImage,
			"MediaType", imageMediaTypeDescriptor.MediaType,
		)
		return mediaTypeCheckSingleResult{Passed: false, WrongMediaTypeImage: relatedImage}
	}
	return mediaTypeCheckSingleResult{Passed: true}
}

// deduplicateImages removes duplicate image references while preserving original order.
func deduplicateImages(images []string) []string {
	seen := make(map[string]bool, len(images))
	unique := make([]string, 0, len(images))
	for _, img := range images {
		if !seen[img] {
			seen[img] = true
			unique = append(unique, img)
		}
	}
	return unique
}

// collectResult appends a single-image check outcome to the shared result slices under mutex protection.
func collectResult(result mediaTypeCheckSingleResult, mu *sync.Mutex, brokenImages *[]string, wrongMediaTypeImages *[]string) {
	switch {
	case result.BrokenImage != "":
		mu.Lock()
		defer mu.Unlock()
		*brokenImages = append(*brokenImages, result.BrokenImage)

	case result.WrongMediaTypeImage != "":
		mu.Lock()
		defer mu.Unlock()
		*wrongMediaTypeImages = append(*wrongMediaTypeImages, result.WrongMediaTypeImage)
	}
}
