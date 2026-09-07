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
	"testing"
)

// sourceRef is used as the input image reference across base image tests.
const sourceRef = "registry.io/repo@" + digestRef

// ── GetBaseImage tests ─────────────────────────────────────────

// Happy path with both annotations — tag preserved and digest appended.
func TestGetBaseImage_HappyPathBothAnnotations(t *testing.T) {
	manifestRef := "registry.io/repo@" + digestAmd64

	inspector := &mockInspector{t: t,
		GetManifestsFn: func(_ context.Context, _ string) (map[string]string, error) {
			return map[string]string{"linux/amd64": digestAmd64}, nil
		},
		InspectRawFn: func(_ context.Context, ref string) (json.RawMessage, error) {
			if ref != manifestRef {
				t.Errorf("InspectRaw called with %q, want %q", ref, manifestRef)
			}
			return json.RawMessage(`{"annotations":{
				"org.opencontainers.image.base.name":"registry.redhat.io/ose-operator-registry:v4.17",
				"org.opencontainers.image.base.digest":"` + digestSingle + `"
			}}`), nil
		},
	}

	got, err := GetBaseImage(context.Background(), inspector, sourceRef)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := "registry.redhat.io/ose-operator-registry:v4.17@" + digestSingle
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

// Base name without digest annotation — returns base image as-is.
func TestGetBaseImage_BaseNameWithoutDigest(t *testing.T) {
	inspector := &mockInspector{t: t,
		GetManifestsFn: func(_ context.Context, _ string) (map[string]string, error) {
			return map[string]string{"linux/amd64": digestAmd64}, nil
		},
		InspectRawFn: func(_ context.Context, _ string) (json.RawMessage, error) {
			return json.RawMessage(`{"annotations":{
				"org.opencontainers.image.base.name":"registry.io/base:v4.17"
			}}`), nil
		},
	}

	got, err := GetBaseImage(context.Background(), inspector, sourceRef)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "registry.io/base:v4.17" {
		t.Errorf("got %q, want %q", got, "registry.io/base:v4.17")
	}
}

// Missing base name annotation — returns ErrBaseImageAnnotationNotFound.
func TestGetBaseImage_MissingBaseName(t *testing.T) {
	inspector := &mockInspector{t: t,
		GetManifestsFn: func(_ context.Context, _ string) (map[string]string, error) {
			return map[string]string{"linux/amd64": digestAmd64}, nil
		},
		InspectRawFn: func(_ context.Context, _ string) (json.RawMessage, error) {
			return json.RawMessage(`{"annotations":{"other":"value"}}`), nil
		},
	}

	_, err := GetBaseImage(context.Background(), inspector, sourceRef)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, ErrBaseImageAnnotationNotFound) {
		t.Errorf("expected ErrBaseImageAnnotationNotFound, got: %v", err)
	}
}

// amd64 selected from multi-arch — correct digest used for annotation lookup.
func TestGetBaseImage_Amd64SelectedFromMultiArch(t *testing.T) {
	expectedManifestRef := "registry.io/repo@" + digestAmd64

	inspector := &mockInspector{t: t,
		GetManifestsFn: func(_ context.Context, _ string) (map[string]string, error) {
			return map[string]string{"linux/amd64": digestAmd64, "linux/arm64": digestArm64}, nil
		},
		InspectRawFn: func(_ context.Context, ref string) (json.RawMessage, error) {
			if ref != expectedManifestRef {
				t.Errorf("InspectRaw called with %q, want %q", ref, expectedManifestRef)
			}
			return json.RawMessage(`{"annotations":{
				"org.opencontainers.image.base.name":"registry.io/base:v4.17"
			}}`), nil
		},
	}

	got, err := GetBaseImage(context.Background(), inspector, sourceRef)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "registry.io/base:v4.17" {
		t.Errorf("got %q, want %q", got, "registry.io/base:v4.17")
	}
}

// Non-amd64 fallback — uses first available arch alphabetically when amd64 is absent.
func TestGetBaseImage_NonAmd64Fallback(t *testing.T) {
	expectedManifestRef := "registry.io/repo@" + digestArm64

	inspector := &mockInspector{t: t,
		GetManifestsFn: func(_ context.Context, _ string) (map[string]string, error) {
			return map[string]string{"linux/arm64": digestArm64}, nil
		},
		InspectRawFn: func(_ context.Context, ref string) (json.RawMessage, error) {
			if ref != expectedManifestRef {
				t.Errorf("InspectRaw called with %q, want %q", ref, expectedManifestRef)
			}
			return json.RawMessage(`{"annotations":{
				"org.opencontainers.image.base.name":"registry.io/base:v4.17"
			}}`), nil
		},
	}

	got, err := GetBaseImage(context.Background(), inspector, sourceRef)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "registry.io/base:v4.17" {
		t.Errorf("got %q, want %q", got, "registry.io/base:v4.17")
	}
}

// Multiple non-amd64 archs — selection is deterministic (alphabetical).
func TestGetBaseImage_MultiNonAmd64Deterministic(t *testing.T) {
	// "arm64" is alphabetically before "s390x", so arm64 digest should be selected.
	expectedManifestRef := "registry.io/repo@" + digestArm64

	inspector := &mockInspector{t: t,
		GetManifestsFn: func(_ context.Context, _ string) (map[string]string, error) {
			return map[string]string{
				"linux/s390x": digestSingle,
				"linux/arm64": digestArm64,
			}, nil
		},
		InspectRawFn: func(_ context.Context, ref string) (json.RawMessage, error) {
			if ref != expectedManifestRef {
				t.Errorf("InspectRaw called with %q, want %q", ref, expectedManifestRef)
			}
			return json.RawMessage(`{"annotations":{
				"org.opencontainers.image.base.name":"registry.io/base:v4.17"
			}}`), nil
		},
	}

	got, err := GetBaseImage(context.Background(), inspector, sourceRef)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "registry.io/base:v4.17" {
		t.Errorf("got %q, want %q", got, "registry.io/base:v4.17")
	}
}

// Unqualified base image name without digest — rejected consistently.
// Ensures validation fires even when digest annotation is absent (validation-inconsistency fix).
func TestGetBaseImage_UnqualifiedBaseNameWithoutDigest(t *testing.T) {
	inspector := &mockInspector{t: t,
		GetManifestsFn: func(_ context.Context, _ string) (map[string]string, error) {
			return map[string]string{"linux/amd64": digestAmd64}, nil
		},
		InspectRawFn: func(_ context.Context, _ string) (json.RawMessage, error) {
			return json.RawMessage(`{"annotations":{
				"org.opencontainers.image.base.name":"ubuntu:22.04"
			}}`), nil
		},
	}

	_, err := GetBaseImage(context.Background(), inspector, sourceRef)
	if err == nil {
		t.Fatal("expected error for unqualified base image name, got nil")
	}
	if !errors.Is(err, ErrInvalidImageReference) {
		t.Errorf("expected ErrInvalidImageReference, got: %v", err)
	}
}

// Unqualified base image name with digest — also rejected (consistent validation).
func TestGetBaseImage_UnqualifiedBaseNameWithDigest(t *testing.T) {
	inspector := &mockInspector{t: t,
		GetManifestsFn: func(_ context.Context, _ string) (map[string]string, error) {
			return map[string]string{"linux/amd64": digestAmd64}, nil
		},
		InspectRawFn: func(_ context.Context, _ string) (json.RawMessage, error) {
			return json.RawMessage(`{"annotations":{
				"org.opencontainers.image.base.name":"ubuntu:22.04",
				"org.opencontainers.image.base.digest":"` + digestSingle + `"
			}}`), nil
		},
	}

	_, err := GetBaseImage(context.Background(), inspector, sourceRef)
	if err == nil {
		t.Fatal("expected error for unqualified base image name, got nil")
	}
	if !errors.Is(err, ErrInvalidImageReference) {
		t.Errorf("expected ErrInvalidImageReference, got: %v", err)
	}
}

// Truncated base image digest annotation — rejected as an invalid reference.
func TestGetBaseImage_TruncatedDigestRejected(t *testing.T) {
	inspector := &mockInspector{t: t,
		GetManifestsFn: func(_ context.Context, _ string) (map[string]string, error) {
			return map[string]string{"linux/amd64": digestAmd64}, nil
		},
		InspectRawFn: func(_ context.Context, _ string) (json.RawMessage, error) {
			return json.RawMessage(`{"annotations":{
				"org.opencontainers.image.base.name":"registry.io/base:v4.17",
				"org.opencontainers.image.base.digest":"sha256:ab"
			}}`), nil
		},
	}

	_, err := GetBaseImage(context.Background(), inspector, sourceRef)
	if err == nil {
		t.Fatal("expected error for truncated digest, got nil")
	}
	if !errors.Is(err, ErrInvalidImageReference) {
		t.Errorf("expected ErrInvalidImageReference, got: %v", err)
	}
}

// GetManifests fails — error propagates.
func TestGetBaseImage_GetManifestsFails(t *testing.T) {
	inspector := &mockInspector{t: t,
		GetManifestsFn: func(_ context.Context, _ string) (map[string]string, error) {
			return nil, fmt.Errorf("network timeout")
		},
	}

	_, err := GetBaseImage(context.Background(), inspector, sourceRef)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

// Empty manifest map — returns ErrManifestDigestNotFound.
func TestGetBaseImage_EmptyManifestMap(t *testing.T) {
	inspector := &mockInspector{t: t,
		GetManifestsFn: func(_ context.Context, _ string) (map[string]string, error) {
			return map[string]string{}, nil
		},
	}

	_, err := GetBaseImage(context.Background(), inspector, sourceRef)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, ErrManifestDigestNotFound) {
		t.Errorf("expected ErrManifestDigestNotFound, got: %v", err)
	}
}

// Digest annotation overrides existing digest in base name.
func TestGetBaseImage_DigestAnnotationOverridesExisting(t *testing.T) {
	inspector := &mockInspector{t: t,
		GetManifestsFn: func(_ context.Context, _ string) (map[string]string, error) {
			return map[string]string{"linux/amd64": digestAmd64}, nil
		},
		InspectRawFn: func(_ context.Context, _ string) (json.RawMessage, error) {
			return json.RawMessage(`{"annotations":{
				"org.opencontainers.image.base.name":"registry.io/base:v4.17@` + digestArm64 + `",
				"org.opencontainers.image.base.digest":"` + digestSingle + `"
			}}`), nil
		},
	}

	got, err := GetBaseImage(context.Background(), inspector, sourceRef)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := "registry.io/base:v4.17@" + digestSingle
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

// Base image selection prefers amd64 across operating systems with sorted ties and fallback.
func TestGetBaseImage_PlatformSelection(t *testing.T) {
	for _, tc := range []struct {
		name       string
		manifests  map[string]string
		wantDigest string
	}{
		{"amd64-tie-sorted", map[string]string{"windows/amd64": digestSingle, "linux/amd64": digestAmd64, "linux/arm64": digestArm64}, digestAmd64},
		{"prefers-windows-amd64-over-linux-arm64", map[string]string{"windows/amd64": digestSingle, "linux/arm64": digestArm64}, digestSingle},
		{"windows-only", map[string]string{"windows/amd64": digestSingle}, digestSingle},
		{"non-linux-fallback", map[string]string{"windows/arm64": digestArm64, "windows/s390x": digestSingle}, digestArm64},
		{"mixed-os-fallback", map[string]string{"windows/arm64": digestSingle, "linux/arm64": digestArm64}, digestArm64},
		{"empty", map[string]string{}, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			inspector := &mockInspector{t: t,
				GetManifestsFn: func(context.Context, string) (map[string]string, error) { return tc.manifests, nil },
			}
			if tc.wantDigest != "" {
				inspector.InspectRawFn = func(_ context.Context, ref string) (json.RawMessage, error) {
					if want := "registry.io/repo@" + tc.wantDigest; ref != want {
						t.Errorf("got %q, want %q", ref, want)
					}
					return json.RawMessage(`{"annotations":{"org.opencontainers.image.base.name":"registry.io/base:v1"}}`), nil
				}
			}
			got, err := GetBaseImage(context.Background(), inspector, sourceRef)
			if tc.wantDigest == "" {
				if !errors.Is(err, ErrManifestDigestNotFound) {
					t.Fatalf("got %v, want ErrManifestDigestNotFound", err)
				}
			} else if err != nil || got != "registry.io/base:v1" {
				t.Fatalf("got %q, %v", got, err)
			}
		})
	}
}
