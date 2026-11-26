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
	FBCRepository           string
	BuildImageRef           string
	ReferenceManifestDigest string
	SourceImageRef          string
	ProcessedDigests        []string
	BaseImageRepository     string
	BaseImageTag            string
	BaseImagePullRef        string
}

// PrepareImageSetup resolves the source manifest and base image reference.
func PrepareImageSetup(ctx context.Context, inspector imagepkg.ImageInspector, input ImageSetupInput) (*ImageSetupResult, error) {
	fbcRepository, buildImageRef, err := resolveValidationImageSource(input.ImageURL, input.ImageDigest)
	if err != nil {
		return nil, err
	}

	manifestDigests, err := inspector.GetManifests(ctx, buildImageRef)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve source manifests %q: %w", buildImageRef, err)
	}

	referenceManifestDigest, err := selectRequiredPlatformDigest(manifestDigests, requiredSourcePlatform)
	if err != nil {
		return nil, err
	}

	imageWithDigest, err := imagepkg.GetImageRegistryRepositoryDigest(fbcRepository + "@" + referenceManifestDigest)
	if err != nil {
		return nil, fmt.Errorf("failed to build source image reference: %w", err)
	}

	baseImageRef, err := imagepkg.GetBaseImageFromManifest(ctx, inspector, imageWithDigest)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve base image for %q: %w", imageWithDigest, err)
	}
	baseImage, err := imagepkg.ParseImageURL(baseImageRef)
	if err != nil {
		return nil, fmt.Errorf("failed to parse base image reference %q: %w", baseImageRef, err)
	}
	baseImagePullRef := baseImageRef
	if baseImage.Digest != "" {
		baseImagePullRef = baseImage.RegistryRepository + "@" + baseImage.Digest
	}

	return &ImageSetupResult{
		FBCRepository:           fbcRepository,
		BuildImageRef:           buildImageRef,
		ReferenceManifestDigest: referenceManifestDigest,
		SourceImageRef:          imageWithDigest,
		ProcessedDigests:        buildProcessedDigests(manifestDigests, input.ImageDigest),
		BaseImageRepository:     baseImage.RegistryRepository,
		BaseImageTag:            baseImage.Tag,
		BaseImagePullRef:        baseImagePullRef,
	}, nil
}

func buildProcessedDigests(manifests map[string]string, imageDigest string) []string {
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

	return digests
}

func resolveValidationImageSource(imageURL, imageDigest string) (string, string, error) {
	fbcRepository, err := imagepkg.GetImageRegistryAndRepository(imageURL)
	if err != nil {
		return "", "", fmt.Errorf("invalid image URL: %w", err)
	}
	if imageDigest == "" {
		return "", "", fmt.Errorf("image digest is empty")
	}

	buildImageRef, err := imagepkg.GetImageRegistryRepositoryDigest(fbcRepository + "@" + imageDigest)
	if err != nil {
		return "", "", fmt.Errorf("invalid image digest %q: %w", imageDigest, err)
	}
	return fbcRepository, buildImageRef, nil
}

func selectRequiredPlatformDigest(manifests map[string]string, platform string) (string, error) {
	digest, ok := manifests[strings.ToLower(platform)]
	if !ok || digest == "" {
		return "", fmt.Errorf("source image requires %q manifest: %w", platform, imagepkg.ErrManifestDigestNotFound)
	}
	return digest, nil
}
