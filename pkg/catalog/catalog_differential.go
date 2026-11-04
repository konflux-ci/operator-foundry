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

package catalog

import (
	"context"
	"fmt"
	"io"
	"os"
	"slices"

	"github.com/konflux-ci/operator-foundry/pkg/image"
)

// UnreleasedRelatedImagesOptions controls rendered-catalog comparison for
// unreleased related images. JSON formatting and default index selection belong
// to the command layer.
type UnreleasedRelatedImagesOptions struct {
	FBCFragment string
	IndexImage  string
	OPMPath     string
	CacheDir    string
	Retry       Config
}

// catalogRenderer renders a catalog image or fragment and returns the rendered catalog file path.
type catalogRenderer func(context.Context, string, string, string, Config) (string, error)

// GetUnreleasedFBCRelatedImages returns sorted, unique related images present
// in the rendered FBC fragment catalog but absent from the comparison index
// catalog, considering only packages declared by the FBC fragment.
//
// If IndexImage has a tag or digest, it is used as-is. Otherwise the target
// indexes are resolved from the FBC fragment and the first resolver result is
// used, preserving validate-fbc's single-index comparison behavior.
func GetUnreleasedFBCRelatedImages(
	ctx context.Context,
	opts UnreleasedRelatedImagesOptions,
	resolver OCPVersionResolver,
) ([]string, error) {
	return getUnreleasedFBCRelatedImages(ctx, opts, resolver, RenderOPM)
}

// getUnreleasedFBCRelatedImages renders the FBC fragment and comparison index,
// then returns related images present only in the fragment's declared packages.
func getUnreleasedFBCRelatedImages(
	ctx context.Context,
	opts UnreleasedRelatedImagesOptions,
	resolver OCPVersionResolver,
	render catalogRenderer,
) ([]string, error) {
	if opts.FBCFragment == "" {
		return nil, ErrEmptyFBCFragment
	}
	if opts.IndexImage == "" {
		return nil, ErrEmptyIndexImage
	}

	renderedFBCPath, err := render(ctx, opts.FBCFragment, opts.OPMPath, opts.CacheDir, opts.Retry)
	if err != nil {
		return nil, fmt.Errorf("failed to render FBC fragment: %w", err)
	}
	// Read package names first so fragments without declared packages fail before bundle decoding.
	packageNames, err := extractPackageNamesFromCatalogFile(renderedFBCPath)
	if err != nil {
		return nil, fmt.Errorf("failed to extract FBC package names: %w", err)
	}
	if len(packageNames) == 0 {
		return nil, ErrEmptyFBCPackageNames
	}
	fbcRelatedImagesByPackage, err := extractRelatedImagesByPackageFromFile(renderedFBCPath, packageNames)
	if err != nil {
		return nil, fmt.Errorf("failed to extract related images from FBC fragment %q: %w", opts.FBCFragment, err)
	}

	indexImage, err := resolveComparisonIndexImage(ctx, opts.FBCFragment, opts.IndexImage, resolver)
	if err != nil {
		return nil, err
	}
	renderedIndexPath, err := render(ctx, indexImage, opts.OPMPath, opts.CacheDir, opts.Retry)
	if err != nil {
		return nil, fmt.Errorf("failed to render index image %q: %w", indexImage, err)
	}
	indexRelatedImagesByPackage, err := extractRelatedImagesByPackageFromFile(renderedIndexPath, packageNames)
	if err != nil {
		return nil, fmt.Errorf("failed to extract index catalog metadata from %q: %w", indexImage, err)
	}

	fbcRelatedImages := collectSortedUniqueRelatedImagesForPackages(fbcRelatedImagesByPackage, packageNames)
	indexRelatedImages := collectSortedUniqueRelatedImagesForPackages(indexRelatedImagesByPackage, packageNames)
	return diffFragmentRelatedImagesAgainstIndex(fbcRelatedImages, indexRelatedImages), nil
}

// resolveComparisonIndexImage returns the index image used for comparison.
// Tagged or digest-pinned indexes are used as-is; unpinned indexes resolve to
// the first target catalog image to preserve validate-fbc's single-index flow.
func resolveComparisonIndexImage(
	ctx context.Context,
	fbcFragment, indexImage string,
	resolver OCPVersionResolver,
) (string, error) {
	parsed, err := image.ParseImageURL(indexImage)
	if err != nil {
		return "", fmt.Errorf("failed to parse index image: %w", err)
	}
	if parsed.Tag != "" || parsed.Digest != "" {
		return indexImage, nil
	}

	targets, err := GetTargetFBCCatalogImages(ctx, fbcFragment, indexImage, resolver)
	if err != nil {
		return "", fmt.Errorf("failed to resolve target catalog images: %w", err)
	}
	if len(targets) == 0 {
		return "", ErrEmptyOCPVersions
	}
	return targets[0], nil
}

// extractPackageNamesFromCatalogFile returns package names declared by
// olm.package records in a rendered catalog file.
func extractPackageNamesFromCatalogFile(path string) ([]string, error) {
	return readCatalogFile(path, ExtractUniquePackageNamesFromCatalog)
}

// extractRelatedImagesByPackageFromFile returns related image metadata for
// active bundles in the requested packages from a rendered catalog file.
func extractRelatedImagesByPackageFromFile(path string, packageNames []string) (map[string][]string, error) {
	return readCatalogFile(path, func(r io.Reader) (map[string][]string, error) {
		return ExtractRelatedImagesByPackageFromCatalog(r, packageNames)
	})
}

// readCatalogFile opens path, passes it to extract, and closes the file before
// returning.
func readCatalogFile[T any](path string, extract func(io.Reader) (T, error)) (T, error) {
	var zero T
	f, err := os.Open(path)
	if err != nil {
		return zero, fmt.Errorf("failed to open rendered catalog %q: %w", path, err)
	}
	defer func() { _ = f.Close() }()
	return extract(f)
}

// collectSortedUniqueRelatedImagesForPackages flattens related images for the
// requested packages and returns them in stable sorted order without duplicates.
func collectSortedUniqueRelatedImagesForPackages(relatedImagesByPackage map[string][]string, packageNames []string) []string {
	imageCount := 0
	for _, packageName := range packageNames {
		imageCount += len(relatedImagesByPackage[packageName])
	}
	images := make([]string, 0, imageCount)
	for _, packageName := range packageNames {
		images = append(images, relatedImagesByPackage[packageName]...)
	}
	slices.Sort(images)
	return slices.Compact(images)
}

// diffFragmentRelatedImagesAgainstIndex returns fragment related images absent
// from the index related image set. Both inputs must be sorted.
func diffFragmentRelatedImagesAgainstIndex(sortedFragmentImages, sortedIndexImages []string) []string {
	difference := make([]string, 0, len(sortedFragmentImages))
	indexImagePosition := 0
	for _, fragmentImage := range sortedFragmentImages {
		// Skip index images that are smaller than the current fragment image.
		for indexImagePosition < len(sortedIndexImages) && sortedIndexImages[indexImagePosition] < fragmentImage {
			indexImagePosition++
		}
		// If both catalogs contain this image, it is already released.
		if indexImagePosition < len(sortedIndexImages) && sortedIndexImages[indexImagePosition] == fragmentImage {
			continue
		}
		// Otherwise, this image exists only in the fragment catalog.
		difference = append(difference, fragmentImage)
	}
	return difference
}
