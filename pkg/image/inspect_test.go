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
	"slices"
	"testing"
)

// Digests used across inspect test cases. Each is exactly 64 hex chars
// after "sha256:" to satisfy the distribution/reference parser.
const (
	digestAmd64  = "sha256:1111111111111111111111111111111111111111111111111111111111111111"
	digestArm64  = "sha256:2222222222222222222222222222222222222222222222222222222222222222"
	digestSingle = "sha256:3333333333333333333333333333333333333333333333333333333333333333"
	digestRef    = "sha256:4444444444444444444444444444444444444444444444444444444444444444"
)

// ── Mock runner ────────────────────────────────────────────────

type mockRunner struct {
	RunFn func(ctx context.Context, name string, args ...string) ([]byte, error)
}

func (m *mockRunner) Run(ctx context.Context, name string, args ...string) ([]byte, error) {
	return m.RunFn(ctx, name, args...)
}

// ── Inspect tests ──────────────────────────────────────────────

// Happy path — verify all three fields parsed correctly.
func TestInspect_HappyPath(t *testing.T) {
	payload := InspectResult{
		Digest:       digestAmd64,
		Architecture: "amd64",
		Labels:       map[string]string{"key": "val"},
	}
	jsonBytes, _ := json.Marshal(payload)

	inspector := NewSkopeoInspector(&mockRunner{
		RunFn: func(_ context.Context, _ string, _ ...string) ([]byte, error) {
			return jsonBytes, nil
		},
	})

	got, err := inspector.Inspect(context.Background(), "registry.io/repo@"+digestRef)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Digest != digestAmd64 {
		t.Errorf("Digest = %q, want %q", got.Digest, digestAmd64)
	}
	if got.Architecture != "amd64" {
		t.Errorf("Architecture = %q, want %q", got.Architecture, "amd64")
	}
	if got.Labels["key"] != "val" {
		t.Errorf("Labels[key] = %q, want %q", got.Labels["key"], "val")
	}
}

// Runner error — verify the error propagates with wrapping.
func TestInspect_RunnerError(t *testing.T) {
	runnerErr := fmt.Errorf("connection refused")
	inspector := NewSkopeoInspector(&mockRunner{
		RunFn: func(_ context.Context, _ string, _ ...string) ([]byte, error) {
			return nil, runnerErr
		},
	})

	_, err := inspector.Inspect(context.Background(), "registry.io/repo@"+digestRef)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, runnerErr) {
		t.Errorf("expected error to wrap runner error, got: %v", err)
	}
}

// Invalid JSON — verify error wraps ErrParseInspectOutput.
func TestInspect_InvalidJSON(t *testing.T) {
	inspector := NewSkopeoInspector(&mockRunner{
		RunFn: func(_ context.Context, _ string, _ ...string) ([]byte, error) {
			return []byte("not-json"), nil
		},
	})

	_, err := inspector.Inspect(context.Background(), "registry.io/repo@"+digestRef)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, ErrParseInspectOutput) {
		t.Errorf("expected error to wrap ErrParseInspectOutput, got: %v", err)
	}
}

// ── InspectRaw tests ───────────────────────────────────────────

// Runner error — verify error wraps ErrRawInspectFailed.
func TestInspectRaw_RunnerError(t *testing.T) {
	inspector := NewSkopeoInspector(&mockRunner{
		RunFn: func(_ context.Context, _ string, _ ...string) ([]byte, error) {
			return nil, fmt.Errorf("network timeout")
		},
	})

	_, err := inspector.InspectRaw(context.Background(), "registry.io/repo@"+digestRef)
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
	index := map[string]interface{}{
		"schemaVersion": 2,
		"manifests": []map[string]interface{}{
			{"digest": digestAmd64, "platform": map[string]string{"architecture": "amd64"}},
			{"digest": digestArm64, "platform": map[string]string{"architecture": "arm64"}},
		},
	}
	indexJSON, _ := json.Marshal(index)

	inspector := NewSkopeoInspector(&mockRunner{
		RunFn: func(_ context.Context, _ string, _ ...string) ([]byte, error) {
			return indexJSON, nil
		},
	})

	got, err := inspector.GetManifests(context.Background(), "registry.io/repo@"+digestRef)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d entries, want 2", len(got))
	}
	if got["amd64"] != digestAmd64 {
		t.Errorf("amd64 = %q, want %q", got["amd64"], digestAmd64)
	}
	if got["arm64"] != digestArm64 {
		t.Errorf("arm64 = %q, want %q", got["arm64"], digestArm64)
	}
}

// Single-manifest fallback.
func TestGetManifests_SingleManifestFallback(t *testing.T) {
	rawJSON, _ := json.Marshal(map[string]interface{}{"schemaVersion": 2})
	inspectJSON, _ := json.Marshal(InspectResult{
		Digest:       digestSingle,
		Architecture: "amd64",
	})

	inspector := NewSkopeoInspector(&mockRunner{
		RunFn: func(_ context.Context, _ string, args ...string) ([]byte, error) {
			if slices.Contains(args, "--raw") {
				return rawJSON, nil
			}
			return inspectJSON, nil
		},
	})

	got, err := inspector.GetManifests(context.Background(), "registry.io/repo@"+digestRef)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("got %d entries, want 1", len(got))
	}
	if got["amd64"] != digestSingle {
		t.Errorf("amd64 = %q, want %q", got["amd64"], digestSingle)
	}
}

// Raw inspect fails — error wrapping ErrRawInspectFailed.
func TestGetManifests_RawInspectFails(t *testing.T) {
	inspector := NewSkopeoInspector(&mockRunner{
		RunFn: func(_ context.Context, _ string, _ ...string) ([]byte, error) {
			return nil, fmt.Errorf("connection refused")
		},
	})

	_, err := inspector.GetManifests(context.Background(), "registry.io/repo@"+digestRef)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, ErrRawInspectFailed) {
		t.Errorf("expected error to wrap ErrRawInspectFailed, got: %v", err)
	}
}

// Fallback inspect fails — error wrapping ErrImageInspectFailed.
func TestGetManifests_FallbackInspectFails(t *testing.T) {
	rawJSON, _ := json.Marshal(map[string]interface{}{"schemaVersion": 2})

	inspector := NewSkopeoInspector(&mockRunner{
		RunFn: func(_ context.Context, _ string, args ...string) ([]byte, error) {
			if slices.Contains(args, "--raw") {
				return rawJSON, nil
			}
			return nil, fmt.Errorf("auth failed")
		},
	})

	_, err := inspector.GetManifests(context.Background(), "registry.io/repo@"+digestRef)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, ErrImageInspectFailed) {
		t.Errorf("expected error to wrap ErrImageInspectFailed, got: %v", err)
	}
}

// Fallback missing Architecture — returns ErrMissingArchDigest.
func TestGetManifests_FallbackMissingArchitecture(t *testing.T) {
	rawJSON, _ := json.Marshal(map[string]interface{}{"schemaVersion": 2})
	inspectJSON, _ := json.Marshal(map[string]interface{}{
		"Digest":       digestSingle,
		"Architecture": "",
	})

	inspector := NewSkopeoInspector(&mockRunner{
		RunFn: func(_ context.Context, _ string, args ...string) ([]byte, error) {
			if slices.Contains(args, "--raw") {
				return rawJSON, nil
			}
			return inspectJSON, nil
		},
	})

	_, err := inspector.GetManifests(context.Background(), "registry.io/repo@"+digestRef)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, ErrMissingArchDigest) {
		t.Errorf("expected error to wrap ErrMissingArchDigest, got: %v", err)
	}
}

// Fallback missing Digest — returns ErrMissingArchDigest.
func TestGetManifests_FallbackMissingDigest(t *testing.T) {
	rawJSON, _ := json.Marshal(map[string]interface{}{"schemaVersion": 2})
	inspectJSON, _ := json.Marshal(map[string]interface{}{
		"Digest":       "",
		"Architecture": "amd64",
	})

	inspector := NewSkopeoInspector(&mockRunner{
		RunFn: func(_ context.Context, _ string, args ...string) ([]byte, error) {
			if slices.Contains(args, "--raw") {
				return rawJSON, nil
			}
			return inspectJSON, nil
		},
	})

	_, err := inspector.GetManifests(context.Background(), "registry.io/repo@"+digestRef)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, ErrMissingArchDigest) {
		t.Errorf("expected error to wrap ErrMissingArchDigest, got: %v", err)
	}
}

// Empty manifests array — returns ErrNoUsableManifests.
func TestGetManifests_EmptyManifestsArray(t *testing.T) {
	rawJSON, _ := json.Marshal(map[string]interface{}{"manifests": []interface{}{}})

	inspector := NewSkopeoInspector(&mockRunner{
		RunFn: func(_ context.Context, _ string, _ ...string) ([]byte, error) {
			return rawJSON, nil
		},
	})

	_, err := inspector.GetManifests(context.Background(), "registry.io/repo@"+digestRef)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, ErrNoUsableManifests) {
		t.Errorf("expected error to wrap ErrNoUsableManifests, got: %v", err)
	}
}
