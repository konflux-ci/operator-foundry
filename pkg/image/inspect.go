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
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"

	"github.com/google/go-containerregistry/pkg/v1/remote"
	"github.com/google/go-containerregistry/pkg/v1/types"
)

// InspectResult holds parsed metadata from an image inspection.
type InspectResult struct {
	Digest       string            `json:"digest"`
	Architecture string            `json:"architecture"`
	Labels       map[string]string `json:"labels"`
}

// ImageInspector abstracts image registry inspection.
type ImageInspector interface {
	Inspect(ctx context.Context, imageRef string) (*InspectResult, error)
	InspectRaw(ctx context.Context, imageRef string) (json.RawMessage, error)
	GetManifests(ctx context.Context, imageRef string) (map[string]string, error)
}

// RemoteInspector implements ImageInspector using go-containerregistry
// to talk directly to OCI/Docker registries over HTTP.
type RemoteInspector struct {
	remoteOpts []remote.Option
}

// NewRemoteInspector creates a RemoteInspector. Callers provide
// remote.Option values to configure authentication, transport, etc.
func NewRemoteInspector(opts ...remote.Option) *RemoteInspector {
	return &RemoteInspector{remoteOpts: opts}
}

// withContext builds a per-call option slice: the shared auth/transport options
// plus the caller's context. A copy is made so concurrent calls don't race on
// the underlying remoteOpts slice.
func (r *RemoteInspector) withContext(ctx context.Context) []remote.Option {
	return append(append([]remote.Option{}, r.remoteOpts...), remote.WithContext(ctx))
}

// Inspect fetches image metadata (digest, architecture, labels) for the given
// image reference. If the reference points to a multi-arch index, the
// linux/amd64 child image is selected automatically (the default platform
// used by go-containerregistry).
func (r *RemoteInspector) Inspect(ctx context.Context, imageRef string) (*InspectResult, error) {
	ref, err := resolveRef(imageRef)
	if err != nil {
		return nil, err
	}

	slog.Debug("fetching image metadata", "imageRef", ref.String())
	img, err := remote.Image(ref, r.withContext(ctx)...)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrImageFetchFailed, err)
	}

	cf, err := img.ConfigFile()
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrImageFetchFailed, err)
	}

	digest, err := img.Digest()
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrImageFetchFailed, err)
	}

	slog.Debug("inspected image metadata", "imageRef", ref.String(), "digest", digest.String(), "architecture", cf.Architecture)
	return &InspectResult{
		Digest:       digest.String(),
		Architecture: cf.Architecture,
		Labels:       cf.Config.Labels,
	}, nil
}

// InspectRaw fetches the raw manifest bytes for the given image reference.
// For an image index this returns the index manifest; for a single image
// it returns the image manifest.
func (r *RemoteInspector) InspectRaw(ctx context.Context, imageRef string) (json.RawMessage, error) {
	ref, err := resolveRef(imageRef)
	if err != nil {
		return nil, err
	}

	slog.Debug("fetching image manifest", "imageRef", ref.String())
	desc, err := remote.Get(ref, r.withContext(ctx)...)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrRawInspectFailed, err)
	}

	return json.RawMessage(desc.Manifest), nil
}

// GetManifests resolves manifest digests keyed by lowercase os/architecture.
// It includes all operating systems; callers choose the platform they support.
// Entries without an OS or architecture are skipped in image indexes.
func (r *RemoteInspector) GetManifests(ctx context.Context, imageRef string) (map[string]string, error) {
	ref, err := resolveRef(imageRef)
	if err != nil {
		return nil, err
	}

	slog.Debug("fetching image manifest", "imageRef", ref.String())
	desc, err := remote.Get(ref, r.withContext(ctx)...)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrRawInspectFailed, err)
	}

	slog.Debug("resolving image manifests", "imageRef", ref.String(), "mediaType", desc.MediaType)
	switch desc.MediaType {
	case types.OCIImageIndex, types.DockerManifestList: // multi-arch index
		idx, err := desc.ImageIndex()
		if err != nil {
			return nil, fmt.Errorf("%w: %w", ErrRawManifestParseFailed, err)
		}
		idxManifest, err := idx.IndexManifest()
		if err != nil {
			return nil, fmt.Errorf("%w: %w", ErrRawManifestParseFailed, err)
		}

		result := make(map[string]string, len(idxManifest.Manifests))
		for _, m := range idxManifest.Manifests {
			if m.Platform == nil {
				continue
			}
			key := platformKey(m.Platform.OS, m.Platform.Architecture)
			if key != "" {
				result[key] = m.Digest.String()
				slog.Debug("found image manifest", "imageRef", ref.String(), "platform", key, "digest", m.Digest.String())
			}
		}
		if len(result) == 0 {
			return nil, ErrNoUsableManifests
		}
		return result, nil

	default:
		// single-arch manifest
		img, err := desc.Image()
		if err != nil {
			return nil, fmt.Errorf("%w: %w", ErrManifestInspectFailed, err)
		}
		cf, err := img.ConfigFile()
		if err != nil {
			return nil, fmt.Errorf("%w: %w", ErrManifestInspectFailed, err)
		}
		digest, err := img.Digest()
		if err != nil {
			return nil, fmt.Errorf("%w: %w", ErrManifestInspectFailed, err)
		}

		key := platformKey(cf.OS, cf.Architecture)
		d := digest.String()
		if key == "" || d == "" {
			return nil, ErrMissingArchDigest
		}
		slog.Debug("found image manifest", "imageRef", ref.String(), "platform", key, "digest", d)
		return map[string]string{key: d}, nil
	}
}

// GetAnnotations extracts OCI annotations from a raw image manifest.
func GetAnnotations(ctx context.Context, inspector ImageInspector, imageRef string) (map[string]string, error) {
	slog.Debug("reading image annotations", "imageRef", imageRef)
	raw, err := inspector.InspectRaw(ctx, imageRef)
	if err != nil {
		return nil, err
	}

	var manifest struct {
		Annotations map[string]string `json:"annotations"`
	}
	if err := json.Unmarshal(raw, &manifest); err != nil {
		return nil, fmt.Errorf("%w: %w", ErrRawManifestParseFailed, err)
	}

	slog.Debug("read image annotations", "imageRef", imageRef, "count", len(manifest.Annotations))
	if manifest.Annotations == nil {
		return map[string]string{}, nil
	}
	return manifest.Annotations, nil
}

// platformKey combines the OS and architecture.
func platformKey(os, arch string) string {
	if os == "" || arch == "" {
		return ""
	}
	return strings.ToLower(os + "/" + arch)
}
