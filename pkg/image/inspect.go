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
	"strings"

	"github.com/google/go-containerregistry/pkg/name"
	"github.com/google/go-containerregistry/pkg/v1/remote"
	"github.com/google/go-containerregistry/pkg/v1/types"
)

// InspectResult holds parsed metadata from an image inspection.
type InspectResult struct {
	Digest       string            `json:"Digest"`
	Architecture string            `json:"Architecture"`
	Labels       map[string]string `json:"Labels"`
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
// image reference. The reference must point to a single image, not an index.
//
// Equivalent to: skopeo inspect --no-tags docker://<imageRef>
func (r *RemoteInspector) Inspect(ctx context.Context, imageRef string) (*InspectResult, error) {
	ref, err := name.ParseReference(imageRef, parseOpts...)
	if err != nil {
		return nil, fmt.Errorf("%w: %s", ErrInspectImageFailed, err)
	}

	// Fetches the manifest and config blob from the registry.
	// Equivalent to: skopeo inspect --no-tags docker://<imageRef>
	img, err := remote.Image(ref, r.withContext(ctx)...)
	if err != nil {
		return nil, fmt.Errorf("%w: %s", ErrInspectImageFailed, err)
	}

	cf, err := img.ConfigFile()
	if err != nil {
		return nil, fmt.Errorf("%w: %s", ErrInspectImageFailed, err)
	}

	digest, err := img.Digest()
	if err != nil {
		return nil, fmt.Errorf("%w: %s", ErrInspectImageFailed, err)
	}

	return &InspectResult{
		Digest:       digest.String(),
		Architecture: cf.Architecture,
		Labels:       cf.Config.Labels,
	}, nil
}

// InspectRaw fetches the raw manifest bytes for the given image reference.
// For an image index this returns the index manifest; for a single image
// it returns the image manifest.
//
// Equivalent to: skopeo inspect --raw --no-tags docker://<imageRef>
func (r *RemoteInspector) InspectRaw(ctx context.Context, imageRef string) (json.RawMessage, error) {
	ref, err := name.ParseReference(imageRef, parseOpts...)
	if err != nil {
		return nil, fmt.Errorf("%w: %s", ErrRawInspectFailed, err)
	}

	// Fetches the raw manifest from the registry (no config blob resolution).
	// Equivalent to: skopeo inspect --raw --no-tags docker://<imageRef>
	desc, err := remote.Get(ref, r.withContext(ctx)...)
	if err != nil {
		return nil, fmt.Errorf("%w: %s", ErrRawInspectFailed, err)
	}

	return json.RawMessage(desc.Manifest), nil
}

// GetManifests resolves per-architecture manifest digests for the given image
// reference. For an OCI image index it returns one entry per platform; for a
// single-arch manifest it returns one entry keyed by the image's architecture.
func (r *RemoteInspector) GetManifests(ctx context.Context, imageRef string) (map[string]string, error) {
	digestRef, err := GetImageRegistryRepositoryDigest(imageRef)
	if err != nil {
		return nil, err
	}

	ref, err := name.ParseReference(digestRef, parseOpts...)
	if err != nil {
		return nil, fmt.Errorf("%w: %s", ErrRawInspectFailed, err)
	}

	// Fetch the raw manifest. Equivalent to: skopeo inspect --raw --no-tags docker://<digestRef>
	desc, err := remote.Get(ref, r.withContext(ctx)...)
	if err != nil {
		return nil, fmt.Errorf("%w: %s", ErrRawInspectFailed, err)
	}

	switch desc.MediaType {
	case types.OCIImageIndex, types.DockerManifestList: // multi-arch index
		idx, err := desc.ImageIndex()
		if err != nil {
			return nil, fmt.Errorf("%w: %s", ErrParseRawManifest, err)
		}
		idxManifest, err := idx.IndexManifest()
		if err != nil {
			return nil, fmt.Errorf("%w: %s", ErrParseRawManifest, err)
		}

		result := make(map[string]string, len(idxManifest.Manifests))
		for _, m := range idxManifest.Manifests {
			if m.Platform != nil {
				arch := strings.ToLower(m.Platform.Architecture)
				if arch != "" {
					result[arch] = m.Digest.String()
				}
			}
		}
		if len(result) == 0 {
			return nil, fmt.Errorf("%w", ErrNoUsableManifests)
		}
		return result, nil

	default:
		// single-arch manifest
		img, err := desc.Image()
		if err != nil {
			return nil, fmt.Errorf("%w: %s", ErrImageInspectFailed, err)
		}
		cf, err := img.ConfigFile()
		if err != nil {
			return nil, fmt.Errorf("%w: %s", ErrImageInspectFailed, err)
		}
		digest, err := img.Digest()
		if err != nil {
			return nil, fmt.Errorf("%w: %s", ErrImageInspectFailed, err)
		}

		arch := strings.ToLower(cf.Architecture)
		d := digest.String()
		if arch == "" || d == "" {
			return nil, fmt.Errorf("%w", ErrMissingArchDigest)
		}
		return map[string]string{arch: d}, nil
	}
}
