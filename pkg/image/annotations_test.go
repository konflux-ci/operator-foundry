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

// ── Mock inspector ─────────────────────────────────────────────

type mockInspector struct {
	InspectFn      func(ctx context.Context, imageRef string) (*InspectResult, error)
	InspectRawFn   func(ctx context.Context, imageRef string) (json.RawMessage, error)
	GetManifestsFn func(ctx context.Context, imageRef string) (map[string]string, error)
}

func (m *mockInspector) Inspect(ctx context.Context, imageRef string) (*InspectResult, error) {
	return m.InspectFn(ctx, imageRef)
}

func (m *mockInspector) InspectRaw(ctx context.Context, imageRef string) (json.RawMessage, error) {
	return m.InspectRawFn(ctx, imageRef)
}

func (m *mockInspector) GetManifests(ctx context.Context, imageRef string) (map[string]string, error) {
	return m.GetManifestsFn(ctx, imageRef)
}

// ── GetAnnotations tests ───────────────────────────────────────

// Annotations present — both key-value pairs returned.
func TestGetAnnotations_Present(t *testing.T) {
	raw := `{"annotations":{"org.opencontainers.image.base.name":"registry.io/base:v1","custom":"value"}}`
	inspector := &mockInspector{
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
	inspector := &mockInspector{
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
	inspector := &mockInspector{
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

// Empty image URL — returns ErrMissingImageURL.
func TestGetAnnotations_EmptyURL(t *testing.T) {
	inspector := &mockInspector{}

	_, err := GetAnnotations(context.Background(), inspector, "")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, ErrEmptyImageURL) {
		t.Errorf("expected error to wrap ErrEmptyImageURL, got: %v", err)
	}
}

// InspectRaw fails — error propagates with context.
func TestGetAnnotations_InspectRawFails(t *testing.T) {
	inspector := &mockInspector{
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
