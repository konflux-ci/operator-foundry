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

// Package fbcvalidation provides workflow helpers for validating FBC images.
package fbcvalidation

import (
	"context"
	"fmt"
	"slices"
	"strings"

	imagepkg "github.com/konflux-ci/operator-foundry/pkg/image"
)

// requiredSourcePlatform selects the Linux amd64 manifest matching the digest selected by validate-fbc bash.
const requiredSourcePlatform = "linux/amd64"

// ImageSetupInput contains the FBC image reference and digest produced by the build pipeline.
type ImageSetupInput struct {
	ImageURL    string
	ImageDigest string
}

// ImageSetupResult contains the source FBC image state needed by validation.
type ImageSetupResult struct {
	FBCRepository       string
	InitialSourceRef    string
	ManifestsByPlatform map[string]string
	ReferenceDigest     string
	SourceImageRef      string
	ImagesProcessed     ImagesProcessed
}

// ImagesProcessed is the Tekton result data that records which FBC image digests were processed.
type ImagesProcessed struct {
	Image ProcessedImage `json:"image"`
}

// ProcessedImage contains an image pullspec and the manifest digests processed for it.
type ProcessedImage struct {
	// Pullspec describes where to pull the FBC image from, without tag or digest.
	Pullspec string `json:"pullspec"`
	// Digests contains processed digests for the repository, including platform manifests and the input digest.
	Digests []string `json:"digests"`
}

// PrepareImageSetup resolves the source FBC image state used by validation.
func PrepareImageSetup(ctx context.Context, inspector imagepkg.ImageInspector, input ImageSetupInput) (*ImageSetupResult, error) {
	fbcRepository, initialSourceRef, err := resolveValidationImageSource(input.ImageURL, input.ImageDigest)
	if err != nil {
		return nil, err
	}

	manifests, err := inspector.GetManifests(ctx, initialSourceRef)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve source manifests %q: %w", initialSourceRef, err)
	}

	referenceDigest, err := selectRequiredPlatformDigest(manifests, requiredSourcePlatform)
	if err != nil {
		return nil, err
	}

	sourceImageRef, err := imagepkg.GetImageRegistryRepositoryDigest(fbcRepository + "@" + referenceDigest)
	if err != nil {
		return nil, fmt.Errorf("failed to build source image reference: %w", err)
	}

	return &ImageSetupResult{
		FBCRepository:       fbcRepository,
		InitialSourceRef:    initialSourceRef,
		ManifestsByPlatform: manifests,
		ReferenceDigest:     referenceDigest,
		SourceImageRef:      sourceImageRef,
		ImagesProcessed:     buildImagesProcessed(fbcRepository, manifests, input.ImageDigest),
	}, nil
}

func buildImagesProcessed(fbcRepository string, manifests map[string]string, imageDigest string) ImagesProcessed {
	platforms := make([]string, 0, len(manifests))
	for platform := range manifests {
		platforms = append(platforms, platform)
	}
	slices.Sort(platforms)

	digests := make([]string, 0, len(manifests)+1)
	for _, platform := range platforms {
		digest := manifests[platform]
		if !slices.Contains(digests, digest) {
			digests = append(digests, digest)
		}
	}
	if imageDigest != "" && !slices.Contains(digests, imageDigest) {
		digests = append(digests, imageDigest)
	}

	return ImagesProcessed{
		Image: ProcessedImage{
			Pullspec: fbcRepository,
			Digests:  digests,
		},
	}
}

func resolveValidationImageSource(imageURL, imageDigest string) (string, string, error) {
	fbcRepository, err := imagepkg.GetImageRegistryAndRepository(imageURL)
	if err != nil {
		return "", "", fmt.Errorf("invalid image URL: %w", err)
	}
	if imageDigest == "" {
		return "", "", fmt.Errorf("image digest is empty")
	}

	sourceRef, err := imagepkg.GetImageRegistryRepositoryDigest(fbcRepository + "@" + imageDigest)
	if err != nil {
		return "", "", fmt.Errorf("invalid image digest %q: %w", imageDigest, err)
	}
	return fbcRepository, sourceRef, nil
}

func selectRequiredPlatformDigest(manifests map[string]string, platform string) (string, error) {
	digest, ok := manifests[strings.ToLower(platform)]
	if !ok || digest == "" {
		return "", fmt.Errorf("source image requires %q manifest: %w", platform, imagepkg.ErrManifestDigestNotFound)
	}
	return digest, nil
}
