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
	"errors"
	"slices"
	"testing"

	"github.com/konflux-ci/operator-foundry/pkg/image"
)

// If the index image is "registry.io/index:v4.17" and the fragment targets
// "v4.18", the matching catalog image is "registry.io/index:v4.18".
func TestGetTargetFBCCatalogImages(t *testing.T) {
	ctx := context.Background()
	const fbcFragment = "fbc-fragment-render-target"
	const digest = "sha256:abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789"

	cases := []struct {
		name        string
		indexImage  string
		ocpVersions []string
		want        []string
	}{
		{
			name:        "index without tag or digest",
			indexImage:  "registry.redhat.io/redhat/redhat-operator-index",
			ocpVersions: []string{"v4.18"},
			want:        []string{"registry.redhat.io/redhat/redhat-operator-index:v4.18"},
		},
		{
			name:        "strips tag",
			indexImage:  "registry.redhat.io/redhat/redhat-operator-index:v4.17",
			ocpVersions: []string{"v4.18"},
			want:        []string{"registry.redhat.io/redhat/redhat-operator-index:v4.18"},
		},
		{
			name:        "strips digest",
			indexImage:  "registry.redhat.io/redhat/redhat-operator-index@" + digest,
			ocpVersions: []string{"v4.18"},
			want:        []string{"registry.redhat.io/redhat/redhat-operator-index:v4.18"},
		},
		{
			name:        "strips tag and digest",
			indexImage:  "registry.redhat.io/redhat/redhat-operator-index:v4.17@" + digest,
			ocpVersions: []string{"v4.18"},
			want:        []string{"registry.redhat.io/redhat/redhat-operator-index:v4.18"},
		},
		{
			name:        "preserves registry port and repository path",
			indexImage:  "registry.example:5000/team/redhat-operator-index:v4.17",
			ocpVersions: []string{"4.18"},
			want:        []string{"registry.example:5000/team/redhat-operator-index:4.18"},
		},
		{
			name:        "preserves OCP version exactly",
			indexImage:  "registry.redhat.io/redhat/redhat-operator-index:v4.17",
			ocpVersions: []string{"candidate"},
			want:        []string{"registry.redhat.io/redhat/redhat-operator-index:candidate"},
		},
		{
			name:        "returns one target image per OCP version",
			indexImage:  "registry.redhat.io/redhat/redhat-operator-index:v4.17",
			ocpVersions: []string{"v4.18", "v4.19"},
			want: []string{
				"registry.redhat.io/redhat/redhat-operator-index:v4.18",
				"registry.redhat.io/redhat/redhat-operator-index:v4.19",
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var gotFragment string
			got, err := GetTargetFBCCatalogImages(ctx, fbcFragment, tc.indexImage, func(_ context.Context, fragment string) ([]string, error) {
				gotFragment = fragment
				return tc.ocpVersions, nil
			})
			if err != nil {
				t.Fatalf("GetTargetFBCCatalogImages() unexpected error: %v", err)
			}
			if !slices.Equal(got, tc.want) {
				t.Fatalf("GetTargetFBCCatalogImages() = %q, want %q", got, tc.want)
			}
			if gotFragment != fbcFragment {
				t.Fatalf("OCP resolver fragment = %q, want %q", gotFragment, fbcFragment)
			}
		})
	}
}

// Missing required inputs and invalid index references are reported before any
// target image is produced.
func TestGetTargetFBCCatalogImages_ValidationErrors(t *testing.T) {
	ctx := context.Background()
	resolver := func(context.Context, string) ([]string, error) { return []string{"v4.18"}, nil }

	cases := []struct {
		name       string
		fragment   string
		indexImage string
		resolver   OCPVersionResolver
		wantErr    error
	}{
		{
			name:       "empty fragment",
			fragment:   "",
			indexImage: "registry.example/index",
			resolver:   resolver,
			wantErr:    ErrEmptyFBCFragment,
		},
		{
			name:       "empty index image",
			fragment:   "fbc-fragment-render-target",
			indexImage: "",
			resolver:   resolver,
			wantErr:    ErrEmptyIndexImage,
		},
		{
			name:       "nil resolver",
			fragment:   "fbc-fragment-render-target",
			indexImage: "registry.example/index",
			resolver:   nil,
			wantErr:    ErrNilOCPVersionResolver,
		},
		{
			name:       "invalid index image",
			fragment:   "fbc-fragment-render-target",
			indexImage: "index",
			resolver:   resolver,
			wantErr:    image.ErrInvalidImageReference,
		},
		{
			name:       "empty resolved OCP versions",
			fragment:   "fbc-fragment-render-target",
			indexImage: "registry.example/index",
			resolver: func(context.Context, string) ([]string, error) {
				return nil, nil
			},
			wantErr: ErrEmptyOCPVersions,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := GetTargetFBCCatalogImages(ctx, tc.fragment, tc.indexImage, tc.resolver)
			if got != nil || !errors.Is(err, tc.wantErr) {
				t.Fatalf("GetTargetFBCCatalogImages() = (%q, %v), want empty result and %v", got, err, tc.wantErr)
			}
		})
	}
}

// Resolver results must still produce a valid target image reference.
func TestGetTargetFBCCatalogImages_InvalidResolvedOCPVersion(t *testing.T) {
	ctx := context.Background()
	cases := []struct {
		name       string
		ocpVersion string
	}{
		{
			name:       "empty version",
			ocpVersion: "",
		},
		{
			name:       "version with slash",
			ocpVersion: "v4/18",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := GetTargetFBCCatalogImages(
				ctx,
				"fbc-fragment-render-target",
				"registry.example/index:v4.17",
				func(context.Context, string) ([]string, error) {
					return []string{tc.ocpVersion}, nil
				},
			)
			if got != nil || !errors.Is(err, image.ErrInvalidImageReference) {
				t.Fatalf("GetTargetFBCCatalogImages() = (%q, %v), want empty result and %v", got, err, image.ErrInvalidImageReference)
			}
		})
	}
}

// Resolver failures are surfaced to the caller without producing a target image.
func TestGetTargetFBCCatalogImages_PropagatesOCPVersionError(t *testing.T) {
	wantErr := errors.New("inspect labels failed")
	got, err := GetTargetFBCCatalogImages(
		context.Background(),
		"fbc-fragment-render-target",
		"registry.example/index:v4.17",
		func(context.Context, string) ([]string, error) {
			return nil, wantErr
		},
	)
	if got != nil || !errors.Is(err, wantErr) {
		t.Fatalf("GetTargetFBCCatalogImages() = (%q, %v), want empty result and %v", got, err, wantErr)
	}
}
