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

package mediatype

import (
	"context"
	"log/slog"

	"github.com/google/go-containerregistry/pkg/v1/remote"
	"github.com/konflux-ci/operator-foundry/pkg/image"
)

// MirroringFetcher wraps a ManifestFetcher and, when the original pullspec
// fails, retries with mirrored pullspecs from an ImageDigestMirrorSet map
// (bash try-original-then-mirrors parity).
type MirroringFetcher struct {
	Inner   ManifestFetcher
	Mirrors image.MirrorConfigMap
}

// FetchManifest tries the original imageRef first. On failure it resolves
// mirrors via image.ResolveMirrors and tries each in order. The first
// successful fetch wins. If there are no mirrors or all attempts fail, the
// last error is returned (original error when no mirrors were attempted).
func (f *MirroringFetcher) FetchManifest(ctx context.Context, imageRef string) (*remote.Descriptor, error) {
	desc, err := f.Inner.FetchManifest(ctx, imageRef)
	if err == nil {
		return desc, nil
	}
	originalErr := err

	mirrors, resolveErr := image.ResolveMirrors(imageRef, f.Mirrors)
	if resolveErr != nil {
		slog.Warn("failed to resolve image mirrors, keeping original fetch error",
			"imageRef", imageRef,
			"error", resolveErr,
		)
		return nil, originalErr
	}
	if len(mirrors) == 0 {
		return nil, originalErr
	}

	slog.Info("original image fetch failed, trying mirrors",
		"imageRef", imageRef,
		"error", originalErr,
		"mirrors", mirrors,
	)

	var lastErr error = originalErr
	for _, mirrorRef := range mirrors {
		desc, err := f.Inner.FetchManifest(ctx, mirrorRef)
		if err == nil {
			slog.Info("fetched image via mirror",
				"imageRef", imageRef,
				"mirrorRef", mirrorRef,
			)
			return desc, nil
		}
		slog.Warn("mirror fetch failed",
			"imageRef", imageRef,
			"mirrorRef", mirrorRef,
			"error", err,
		)
		lastErr = err
	}
	return nil, lastErr
}
