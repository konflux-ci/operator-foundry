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
)

// CommandRunner abstracts external command execution (skopeo, etc.)
// Concrete implementation comes in a later task. Tests use a mock.
type CommandRunner interface {
	Run(ctx context.Context, name string, args ...string) ([]byte, error)
}

// InspectResult holds parsed metadata from a non-raw image inspection.
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

// SkopeoInspector implements ImageInspector
type SkopeoInspector struct {
	runner CommandRunner
}

func NewSkopeoInspector(runner CommandRunner) *SkopeoInspector {
	return &SkopeoInspector{runner: runner}
}

// Inspect runs "skopeo inspect --no-tags docker://imageRef" and parses the
// output into an InspectResult.
func (s *SkopeoInspector) Inspect(ctx context.Context, imageRef string) (*InspectResult, error) {
	out, err := s.runner.Run(ctx, "skopeo", "inspect", "--no-tags", "docker://"+imageRef)
	if err != nil {
		return nil, fmt.Errorf("%w", err)
	}

	var result InspectResult
	if err := json.Unmarshal(out, &result); err != nil {
		return nil, fmt.Errorf("%w: %w", ErrParseInspectOutput, err)
	}
	return &result, nil
}

// InspectRaw runs "skopeo inspect --no-tags --raw docker://imageRef" and
// returns the raw JSON output without parsing.
func (s *SkopeoInspector) InspectRaw(ctx context.Context, imageRef string) (json.RawMessage, error) {
	out, err := s.runner.Run(ctx, "skopeo", "inspect", "--no-tags", "--raw", "docker://"+imageRef)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrRawInspectFailed, err)
	}
	return json.RawMessage(out), nil
}

// GetManifests resolves per-architecture manifest digests for the given image
// reference. For an OCI image index it returns one entry per platform; for a
// single-arch manifest it falls back to a non-raw inspect and returns one entry
// keyed by the image's architecture.
func (s *SkopeoInspector) GetManifests(ctx context.Context, imageRef string) (map[string]string, error) {
	digestRef, err := GetImageRegistryRepositoryDigest(imageRef)
	if err != nil {
		return nil, err
	}

	raw, err := s.InspectRaw(ctx, digestRef)
	if err != nil {
		return nil, err
	}

	// Anonymous struct: only used here to cherry-pick digest + architecture from the JSON.
	var index struct {
		Manifests []struct {
			Digest   string `json:"digest"`
			Platform struct {
				Architecture string `json:"architecture"`
			} `json:"platform"`
		} `json:"manifests"`
	}
	if err := json.Unmarshal(raw, &index); err == nil && index.Manifests != nil {
		result := make(map[string]string, len(index.Manifests))
		for _, m := range index.Manifests {
			arch := strings.ToLower(m.Platform.Architecture)
			if arch != "" && m.Digest != "" {
				result[arch] = m.Digest
			}
		}
		if len(result) == 0 {
			return nil, fmt.Errorf("%w", ErrNoUsableManifests)
		}
		return result, nil
	}

	// Not an image index — fall back to single-manifest inspection.
	inspectResult, err := s.Inspect(ctx, digestRef)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrImageInspectFailed, err)
	}

	if inspectResult.Architecture == "" || inspectResult.Digest == "" {
		return nil, fmt.Errorf("%w", ErrMissingArchDigest)
	}

	return map[string]string{
		strings.ToLower(inspectResult.Architecture): inspectResult.Digest,
	}, nil
}

