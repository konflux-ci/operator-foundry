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
	"fmt"
)

const (
	annotationBaseImageName   = "org.opencontainers.image.base.name"
	annotationBaseImageDigest = "org.opencontainers.image.base.digest"
)

// GetBaseImage resolves the base image reference for the given image.
// It fetches manifests, selects the amd64 architecture (or first available),
// reads OCI annotations from that manifest, and returns the base image
// reference with an optional digest pin.
func GetBaseImage(ctx context.Context, inspector ImageInspector, imageRef string) (string, error) {
	manifests, err := inspector.GetManifests(ctx, imageRef)
	if err != nil {
		return "", err
	}

	digest, err := selectManifestDigest(manifests)
	if err != nil {
		return "", err
	}

	registryRepo, err := GetImageRegistryAndRepository(imageRef)
	if err != nil {
		return "", err
	}
	manifestRef := registryRepo + "@" + digest

	annotations, err := GetAnnotations(ctx, inspector, manifestRef)
	if err != nil {
		return "", err
	}

	baseImageName := annotations[annotationBaseImageName]
	if baseImageName == "" {
		return "", fmt.Errorf("%w", ErrBaseImageAnnotationNotFound)
	}

	baseImageDigest := annotations[annotationBaseImageDigest]
	if baseImageDigest != "" {
		baseTag, err := GetImageRegistryRepositoryTag(baseImageName)
		if err != nil {
			return "", err
		}
		return baseTag + "@" + baseImageDigest, nil
	}

	return baseImageName, nil
}

// selectManifestDigest selects a manifest digest from the given map.
// It prefers amd64 if available, otherwise it returns the first available digest.
// If the map is empty, it returns ErrManifestDigestNotFound.
func selectManifestDigest(manifests map[string]string) (string, error) {
	if digest, ok := manifests["amd64"]; ok {
		return digest, nil
	}
	for _, digest := range manifests {
		return digest, nil
	}
	return "", fmt.Errorf("%w", ErrManifestDigestNotFound)
}
