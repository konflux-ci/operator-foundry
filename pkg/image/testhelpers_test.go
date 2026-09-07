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
)

// mockInspector is a shared test double implementing ImageInspector.
// Each function field can be set per-test to control behaviour.
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
