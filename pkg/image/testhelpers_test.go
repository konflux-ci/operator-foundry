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
	"testing"
)

// Digests used across inspect and dependent test files (annotations, baseimage).
// Each is exactly 64 hex chars after "sha256:" to satisfy the distribution/reference parser.
const (
	digestAmd64  = "sha256:1111111111111111111111111111111111111111111111111111111111111111"
	digestArm64  = "sha256:2222222222222222222222222222222222222222222222222222222222222222"
	digestSingle = "sha256:3333333333333333333333333333333333333333333333333333333333333333"
	digestRef    = "sha256:4444444444444444444444444444444444444444444444444444444444444444"
)

// mockInspector is a shared test double implementing ImageInspector.
// Each function field can be set per-test; unset methods fail the test
// with a clear message instead of panicking on a nil function call.
type mockInspector struct {
	t              *testing.T
	InspectFn      func(ctx context.Context, imageRef string) (*InspectResult, error)
	InspectRawFn   func(ctx context.Context, imageRef string) (json.RawMessage, error)
	GetManifestsFn func(ctx context.Context, imageRef string) (map[string]string, error)
}

func (m *mockInspector) Inspect(ctx context.Context, imageRef string) (*InspectResult, error) {
	if m.InspectFn == nil {
		m.t.Fatal("unexpected call to Inspect")
	}
	return m.InspectFn(ctx, imageRef)
}

func (m *mockInspector) InspectRaw(ctx context.Context, imageRef string) (json.RawMessage, error) {
	if m.InspectRawFn == nil {
		m.t.Fatal("unexpected call to InspectRaw")
	}
	return m.InspectRawFn(ctx, imageRef)
}

func (m *mockInspector) GetManifests(ctx context.Context, imageRef string) (map[string]string, error) {
	if m.GetManifestsFn == nil {
		m.t.Fatal("unexpected call to GetManifests")
	}
	return m.GetManifestsFn(ctx, imageRef)
}
