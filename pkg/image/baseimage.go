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
	"log/slog"
	"sort"
	"strings"

	"github.com/google/go-containerregistry/pkg/v1"
)

const (
	annotationBaseImageName   = "org.opencontainers.image.base.name"
	annotationBaseImageDigest = "org.opencontainers.image.base.digest"
)

// GetBaseImage reads OCI base image annotations from a selected manifest
// and returns the base image reference with an optional digest pin.
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
	slog.Debug("selected manifest for base image lookup", "imageRef", imageRef, "digest", digest)

	annotations, err := GetAnnotations(ctx, inspector, manifestRef)
	if err != nil {
		return "", err
	}

	baseImageName := annotations[annotationBaseImageName]
	if baseImageName == "" {
		return "", ErrBaseImageAnnotationNotFound
	}

	baseImageDigest := annotations[annotationBaseImageDigest]
	if baseImageDigest != "" {
		if _, err := v1.NewHash(baseImageDigest); err != nil {
			return "", fmt.Errorf("%w: malformed base image digest annotation %q", ErrInvalidImageReference, baseImageDigest)
		}
		baseTag, err := GetImageRegistryRepositoryTag(baseImageName)
		if err != nil {
			return "", err
		}
		return baseTag + "@" + baseImageDigest, nil
	}

	// Validate the base image name even when no digest annotation is present,
	// so callers always receive a parseable reference.
	if _, err := ParseImageURL(baseImageName); err != nil {
		return "", err
	}
	return baseImageName, nil
}

// selectManifestDigest chooses the alphabetically first os/amd64 key.
// If no amd64 entry exists, it chooses the alphabetically first platform key
// among all entries.
// It returns ErrManifestDigestNotFound if the map is empty.
func selectManifestDigest(manifests map[string]string) (string, error) {
	if len(manifests) == 0 {
		return "", ErrManifestDigestNotFound
	}
	platforms := make([]string, 0, len(manifests))
	for platform := range manifests {
		platforms = append(platforms, platform)
	}
	sort.Strings(platforms)
	for _, platform := range platforms {
		if strings.HasSuffix(platform, "/amd64") {
			return manifests[platform], nil
		}
	}
	return manifests[platforms[0]], nil
}
