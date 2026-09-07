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
	"errors"
	"fmt"
	"net/http/httptest"
	"testing"

	"github.com/google/go-containerregistry/pkg/name"
	"github.com/google/go-containerregistry/pkg/registry"
	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/empty"
	"github.com/google/go-containerregistry/pkg/v1/mutate"
	"github.com/google/go-containerregistry/pkg/v1/remote"
)

// ── Test helpers ───────────────────────────────────────────────

// setupRegistry starts an in-memory OCI registry and returns its base URL
// (e.g. "localhost:12345"). The server is shut down when the test finishes.
func setupRegistry(t *testing.T) string {
	t.Helper()
	s := httptest.NewServer(registry.New())
	t.Cleanup(s.Close)
	// Strip "http://" — go-containerregistry refs don't include the scheme.
	return s.Listener.Addr().String()
}

// buildImage creates a v1.Image with the given OS, architecture, and labels.
func buildImage(t *testing.T, os, arch string, labels map[string]string) v1.Image {
	t.Helper()
	img, err := mutate.ConfigFile(empty.Image, &v1.ConfigFile{
		Architecture: arch,
		OS:           os,
		RootFS:       v1.RootFS{Type: "layers"},
		Config: v1.Config{
			Labels: labels,
		},
	})
	if err != nil {
		t.Fatalf("buildImage: %v", err)
	}
	return img
}

// pushImage pushes a single-arch image to the registry and returns its digest.
func pushImage(t *testing.T, ref name.Reference, img v1.Image) string {
	t.Helper()
	if err := remote.Write(ref, img); err != nil {
		t.Fatalf("pushImage: %v", err)
	}
	d, err := img.Digest()
	if err != nil {
		t.Fatalf("pushImage digest: %v", err)
	}
	return d.String()
}

// buildAndPushIndex pushes a multi-arch index with images for each given
// platform. Returns the pushed index digest and a map of platform->image digest.
func buildAndPushIndex(t *testing.T, ref name.Reference, platforms []v1.Platform) (string, map[string]string) {
	t.Helper()
	var adds []mutate.IndexAddendum
	platformDigests := make(map[string]string)

	for _, p := range platforms {
		img := buildImage(t, p.OS, p.Architecture, nil)
		d, err := img.Digest()
		if err != nil {
			t.Fatalf("buildAndPushIndex img digest: %v", err)
		}
		platformDigests[p.String()] = d.String()
		adds = append(adds, mutate.IndexAddendum{
			Add: img,
			Descriptor: v1.Descriptor{
				Platform: &v1.Platform{
					OS:           p.OS,
					Architecture: p.Architecture,
				},
			},
		})
	}

	idx := mutate.AppendManifests(empty.Index, adds...)
	if err := remote.WriteIndex(ref, idx); err != nil {
		t.Fatalf("buildAndPushIndex: %v", err)
	}

	idxDigest, err := idx.Digest()
	if err != nil {
		t.Fatalf("buildAndPushIndex index digest: %v", err)
	}
	return idxDigest.String(), platformDigests
}

// mustParseRef parses a reference or fails the test.
func mustParseRef(t *testing.T, s string) name.Reference {
	t.Helper()
	ref, err := name.ParseReference(s, parseOpts...)
	if err != nil {
		t.Fatalf("mustParseRef(%q): %v", s, err)
	}
	return ref
}

// ── Inspect tests ──────────────────────────────────────────────

// Happy path — verify all three fields parsed correctly.
func TestInspect_HappyPath(t *testing.T) {
	base := setupRegistry(t)
	ref := mustParseRef(t, base+"/test/repo:v1")
	labels := map[string]string{"key": "val"}
	img := buildImage(t, "linux", "amd64", labels)
	wantDigest := pushImage(t, ref, img)

	inspector := NewRemoteInspector()
	got, err := inspector.Inspect(context.Background(), ref.String())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Digest != wantDigest {
		t.Errorf("Digest = %q, want %q", got.Digest, wantDigest)
	}
	if got.Architecture != "amd64" {
		t.Errorf("Architecture = %q, want %q", got.Architecture, "amd64")
	}
	if got.Labels["key"] != "val" {
		t.Errorf("Labels[key] = %q, want %q", got.Labels["key"], "val")
	}
}

// Empty image reference — verify error wraps ErrEmptyImageURL.
func TestInspect_InvalidRef(t *testing.T) {
	inspector := NewRemoteInspector()
	_, err := inspector.Inspect(context.Background(), "")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, ErrEmptyImageURL) {
		t.Errorf("expected error to wrap ErrEmptyImageURL, got: %v", err)
	}
}

// Unqualified Docker Hub shorthand — verify error wraps ErrInvalidImageReference.
func TestInspect_UnqualifiedRef(t *testing.T) {
	inspector := NewRemoteInspector()
	_, err := inspector.Inspect(context.Background(), "ubuntu:22.04")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, ErrInvalidImageReference) {
		t.Errorf("expected error to wrap ErrInvalidImageReference, got: %v", err)
	}
}

// Unreachable registry — verify error wraps ErrImageFetchFailed.
func TestInspect_UnreachableRegistry(t *testing.T) {
	inspector := NewRemoteInspector()
	_, err := inspector.Inspect(context.Background(), "localhost:1/nonexistent/repo:v1")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, ErrImageFetchFailed) {
		t.Errorf("expected error to wrap ErrImageFetchFailed, got: %v", err)
	}
}

// ── InspectRaw tests ───────────────────────────────────────────

// Happy path — verify raw manifest is valid JSON containing the config digest.
func TestInspectRaw_HappyPath(t *testing.T) {
	base := setupRegistry(t)
	ref := mustParseRef(t, base+"/test/repo:v1")
	img := buildImage(t, "linux", "amd64", nil)
	pushImage(t, ref, img)

	inspector := NewRemoteInspector()
	raw, err := inspector.InspectRaw(context.Background(), ref.String())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(raw) == 0 {
		t.Fatal("expected non-empty raw manifest")
	}
}

// Unqualified Docker Hub shorthand — verify error wraps ErrInvalidImageReference.
func TestInspectRaw_UnqualifiedRef(t *testing.T) {
	inspector := NewRemoteInspector()
	_, err := inspector.InspectRaw(context.Background(), "ubuntu:22.04")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, ErrInvalidImageReference) {
		t.Errorf("expected error to wrap ErrInvalidImageReference, got: %v", err)
	}
}

// Unreachable registry — verify error wraps ErrRawInspectFailed.
func TestInspectRaw_UnreachableRegistry(t *testing.T) {
	inspector := NewRemoteInspector()
	_, err := inspector.InspectRaw(context.Background(), "localhost:1/nonexistent/repo:v1")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, ErrRawInspectFailed) {
		t.Errorf("expected error to wrap ErrRawInspectFailed, got: %v", err)
	}
}

// ── GetManifests tests ─────────────────────────────────────────

// Multi-arch image index (happy path).
func TestGetManifests_MultiArchIndex(t *testing.T) {
	base := setupRegistry(t)
	tagRef := mustParseRef(t, base+"/test/repo:v1")

	platforms := []v1.Platform{
		{OS: "linux", Architecture: "amd64"},
		{OS: "linux", Architecture: "arm64"},
	}
	idxDigest, platformDigests := buildAndPushIndex(t, tagRef, platforms)

	testRef := fmt.Sprintf("%s/test/repo@%s", base, idxDigest)
	inspector := NewRemoteInspector()
	got, err := inspector.GetManifests(context.Background(), testRef)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d entries, want 2", len(got))
	}
	if got["linux/amd64"] != platformDigests["linux/amd64"] {
		t.Errorf("amd64 = %q, want %q", got["linux/amd64"], platformDigests["linux/amd64"])
	}
	if got["linux/arm64"] != platformDigests["linux/arm64"] {
		t.Errorf("arm64 = %q, want %q", got["linux/arm64"], platformDigests["linux/arm64"])
	}
}

// Single-manifest fallback.
func TestGetManifests_SingleManifestFallback(t *testing.T) {
	base := setupRegistry(t)
	tagRef := mustParseRef(t, base+"/test/repo:v1")
	img := buildImage(t, "linux", "amd64", nil)
	imgDigest := pushImage(t, tagRef, img)

	testRef := fmt.Sprintf("%s/test/repo@%s", base, imgDigest)
	inspector := NewRemoteInspector()
	got, err := inspector.GetManifests(context.Background(), testRef)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("got %d entries, want 1", len(got))
	}
	if got["linux/amd64"] != imgDigest {
		t.Errorf("amd64 = %q, want %q", got["linux/amd64"], imgDigest)
	}
}

// Unreachable registry — error wrapping ErrRawInspectFailed.
func TestGetManifests_RegistryUnreachable(t *testing.T) {
	inspector := NewRemoteInspector()
	_, err := inspector.GetManifests(context.Background(), "localhost:1/nonexistent/repo@sha256:0000000000000000000000000000000000000000000000000000000000000000")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, ErrRawInspectFailed) {
		t.Errorf("expected error to wrap ErrRawInspectFailed, got: %v", err)
	}
}

// Empty manifests array — returns ErrNoUsableManifests.
func TestGetManifests_EmptyIndex(t *testing.T) {
	base := setupRegistry(t)
	tagRef := mustParseRef(t, base+"/test/repo:empty")

	// Push an empty index (no platform entries).
	idx := empty.Index
	if err := remote.WriteIndex(tagRef, idx); err != nil {
		t.Fatalf("WriteIndex: %v", err)
	}

	idxDigest, err := idx.Digest()
	if err != nil {
		t.Fatalf("Digest: %v", err)
	}
	testRef := fmt.Sprintf("%s/test/repo@%s", base, idxDigest)

	inspector := NewRemoteInspector()
	_, err = inspector.GetManifests(context.Background(), testRef)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, ErrNoUsableManifests) {
		t.Errorf("expected error to wrap ErrNoUsableManifests, got: %v", err)
	}
}

// Context cancellation — Inspect error wraps both ErrImageFetchFailed and context.Canceled.
func TestInspect_ContextCanceled(t *testing.T) {
	base := setupRegistry(t)
	ref := mustParseRef(t, base+"/test/repo:v1")
	img := buildImage(t, "linux", "amd64", nil)
	pushImage(t, ref, img)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	inspector := NewRemoteInspector()
	_, err := inspector.Inspect(ctx, ref.String())
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, ErrImageFetchFailed) {
		t.Errorf("expected error to wrap ErrImageFetchFailed, got: %v", err)
	}
	if !errors.Is(err, context.Canceled) {
		t.Errorf("expected error chain to include context.Canceled, got: %v", err)
	}
}

// Context cancellation — InspectRaw error wraps both ErrRawInspectFailed and context.Canceled.
func TestInspectRaw_ContextCanceled(t *testing.T) {
	base := setupRegistry(t)
	ref := mustParseRef(t, base+"/test/repo:v1")
	img := buildImage(t, "linux", "amd64", nil)
	pushImage(t, ref, img)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	inspector := NewRemoteInspector()
	_, err := inspector.InspectRaw(ctx, ref.String())
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, ErrRawInspectFailed) {
		t.Errorf("expected error to wrap ErrRawInspectFailed, got: %v", err)
	}
	if !errors.Is(err, context.Canceled) {
		t.Errorf("expected error chain to include context.Canceled, got: %v", err)
	}
}

// Context cancellation — GetManifests error wraps context.Canceled.
func TestGetManifests_ContextCanceled(t *testing.T) {
	base := setupRegistry(t)
	ref := mustParseRef(t, base+"/test/repo:v1")
	img := buildImage(t, "linux", "amd64", nil)
	pushImage(t, ref, img)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	inspector := NewRemoteInspector()
	_, err := inspector.GetManifests(ctx, ref.String())
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, context.Canceled) {
		t.Errorf("expected error chain to include context.Canceled, got: %v", err)
	}
}

// Architecture names lowercased — uppercase platform arch should map to lowercase key.
func TestGetManifests_ArchLowercased(t *testing.T) {
	base := setupRegistry(t)
	tagRef := mustParseRef(t, base+"/test/repo:upper")

	img := buildImage(t, "linux", "AMD64", nil)
	imgDigest, err := img.Digest()
	if err != nil {
		t.Fatalf("Digest: %v", err)
	}

	// Build index with uppercase architecture in platform descriptor.
	idx := mutate.AppendManifests(empty.Index, mutate.IndexAddendum{
		Add: img,
		Descriptor: v1.Descriptor{
			Platform: &v1.Platform{
				OS:           "linux",
				Architecture: "AMD64",
			},
		},
	})
	if err := remote.WriteIndex(tagRef, idx); err != nil {
		t.Fatalf("WriteIndex: %v", err)
	}

	idxDigestHash, err := idx.Digest()
	if err != nil {
		t.Fatalf("Digest: %v", err)
	}
	testRef := fmt.Sprintf("%s/test/repo@%s", base, idxDigestHash)

	inspector := NewRemoteInspector()
	got, err := inspector.GetManifests(context.Background(), testRef)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, ok := got["linux/amd64"]; !ok {
		t.Errorf("expected key \"linux/amd64\" in result, got keys: %v", got)
	}
	if got["linux/amd64"] != imgDigest.String() {
		t.Errorf("amd64 = %q, want %q", got["linux/amd64"], imgDigest.String())
	}
}

// Single-manifest with empty architecture — returns ErrMissingArchDigest.
func TestGetManifests_SingleManifestMissingArch(t *testing.T) {
	base := setupRegistry(t)
	tagRef := mustParseRef(t, base+"/test/repo:noarch")

	// Build an image with empty architecture.
	img := buildImage(t, "linux", "", nil)
	imgDigest := pushImage(t, tagRef, img)

	testRef := fmt.Sprintf("%s/test/repo@%s", base, imgDigest)
	inspector := NewRemoteInspector()
	_, err := inspector.GetManifests(context.Background(), testRef)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, ErrMissingArchDigest) {
		t.Errorf("expected error to wrap ErrMissingArchDigest, got: %v", err)
	}
}

// Tag-only reference (no digest) — GetManifests must preserve the tag.
func TestGetManifests_TagOnlyRef(t *testing.T) {
	base := setupRegistry(t)
	tagRef := mustParseRef(t, base+"/test/repo:v1")
	img := buildImage(t, "linux", "amd64", nil)
	imgDigest := pushImage(t, tagRef, img)

	inspector := NewRemoteInspector()
	got, err := inspector.GetManifests(context.Background(), base+"/test/repo:v1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("got %d entries, want 1", len(got))
	}
	if got["linux/amd64"] != imgDigest {
		t.Errorf("amd64 = %q, want %q", got["linux/amd64"], imgDigest)
	}
}

// ── GetAnnotations tests ───────────────────────────────────────

// Annotations present — both key-value pairs returned.
func TestGetAnnotations_Present(t *testing.T) {
	raw := `{"annotations":{"org.opencontainers.image.base.name":"registry.io/base:v1","custom":"value"}}`
	inspector := &mockInspector{t: t,
		InspectRawFn: func(_ context.Context, _ string) (json.RawMessage, error) {
			return json.RawMessage(raw), nil
		},
	}

	got, err := GetAnnotations(context.Background(), inspector, "registry.io/repo@"+digestRef)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got["org.opencontainers.image.base.name"] != "registry.io/base:v1" {
		t.Errorf("base.name = %q, want %q", got["org.opencontainers.image.base.name"], "registry.io/base:v1")
	}
	if got["custom"] != "value" {
		t.Errorf("custom = %q, want %q", got["custom"], "value")
	}
}

// No annotations field — returns empty map, no error.
func TestGetAnnotations_NoField(t *testing.T) {
	raw := `{"schemaVersion":2}`
	inspector := &mockInspector{t: t,
		InspectRawFn: func(_ context.Context, _ string) (json.RawMessage, error) {
			return json.RawMessage(raw), nil
		},
	}

	got, err := GetAnnotations(context.Background(), inspector, "registry.io/repo@"+digestRef)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("got %d annotations, want 0", len(got))
	}
}

// Null annotations value — returns empty map, no error.
func TestGetAnnotations_NullValue(t *testing.T) {
	raw := `{"annotations":null}`
	inspector := &mockInspector{t: t,
		InspectRawFn: func(_ context.Context, _ string) (json.RawMessage, error) {
			return json.RawMessage(raw), nil
		},
	}

	got, err := GetAnnotations(context.Background(), inspector, "registry.io/repo@"+digestRef)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("got %d annotations, want 0", len(got))
	}
}

// Empty image URL — returns ErrEmptyImageURL.
func TestGetAnnotations_EmptyURL(t *testing.T) {
	inspector := &mockInspector{t: t,
		InspectRawFn: func(_ context.Context, _ string) (json.RawMessage, error) {
			return nil, ErrEmptyImageURL
		},
	}

	_, err := GetAnnotations(context.Background(), inspector, "")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, ErrEmptyImageURL) {
		t.Errorf("expected error to wrap ErrEmptyImageURL, got: %v", err)
	}
}

// Tag-only reference (no digest) — tag is preserved and passed to InspectRaw.
func TestGetAnnotations_TagOnlyRef(t *testing.T) {
	raw := `{"annotations":{"key":"value"}}`
	var gotRef string
	inspector := &mockInspector{t: t,
		InspectRawFn: func(_ context.Context, ref string) (json.RawMessage, error) {
			gotRef = ref
			return json.RawMessage(raw), nil
		},
	}

	got, err := GetAnnotations(context.Background(), inspector, "registry.io/repo:v1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got["key"] != "value" {
		t.Errorf("key = %q, want %q", got["key"], "value")
	}
	if gotRef != "registry.io/repo:v1" {
		t.Errorf("InspectRaw called with %q, want tag-preserving %q", gotRef, "registry.io/repo:v1")
	}
}

// InspectRaw fails — error propagates with context.
func TestGetAnnotations_InspectRawFails(t *testing.T) {
	inspector := &mockInspector{t: t,
		InspectRawFn: func(_ context.Context, _ string) (json.RawMessage, error) {
			return nil, fmt.Errorf("%w: connection refused", ErrRawInspectFailed)
		},
	}

	_, err := GetAnnotations(context.Background(), inspector, "registry.io/repo@"+digestRef)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, ErrRawInspectFailed) {
		t.Errorf("expected error to wrap ErrRawInspectFailed, got: %v", err)
	}
}

// Platform-qualified entries preserve Windows images alongside Linux images.
func TestGetManifests_Platforms(t *testing.T) {
	for _, tc := range []struct {
		name      string
		platforms []v1.Platform
	}{
		{"windows-only", []v1.Platform{{OS: "windows", Architecture: "amd64"}}},
		{"mixed-os", []v1.Platform{{OS: "linux", Architecture: "amd64"}, {OS: "windows", Architecture: "amd64"}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			base := setupRegistry(t)
			ref := mustParseRef(t, base+"/test/repo:v1")
			_, want := buildAndPushIndex(t, ref, tc.platforms)
			inspector := NewRemoteInspector()
			got, err := inspector.GetManifests(context.Background(), ref.String())
			if err != nil {
				t.Fatal(err)
			}
			if len(got) != len(want) {
				t.Fatalf("got %v, want %v", got, want)
			}
			for platform, digest := range want {
				if got[platform] != digest {
					t.Errorf("%s: got %q, want %q", platform, got[platform], digest)
				}
				// Inspect the child directly to check the single-manifest path too.
				single, err := inspector.GetManifests(context.Background(), base+"/test/repo@"+digest)
				if err != nil {
					t.Fatal(err)
				}
				if len(single) != 1 || single[platform] != digest {
					t.Errorf("single manifest: got %v, want %s: %s", single, platform, digest)
				}
			}
		})
	}
}
