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
	"errors"
	"log/slog"

	"github.com/google/go-containerregistry/pkg/v1/remote"
	"github.com/konflux-ci/operator-foundry/pkg/image"
)

// MirroringFetcher tries the original pullspec first, then falls back to
// mirrors from an ImageDigestMirrorSet map when the original fetch fails
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
	if f.Inner == nil {
		return nil, errors.New("mirroring fetcher has no inner ManifestFetcher")
	}
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

	lastErr := originalErr
	for _, mirrorRef := range mirrors {
		select {
		case <-ctx.Done():
			return nil, context.Cause(ctx)
		default:
		}
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
