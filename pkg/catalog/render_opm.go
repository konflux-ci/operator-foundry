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

// Package catalog provides helpers for rendering and reading FBC catalogs.
package catalog

import (
	"context"
	"errors"
)

// RenderOPM runs the specified OPM binary against an image reference or a local
// FBC file or directory and returns the path to its rendered output.
//
// Successful image renders are cached under cacheDir. Local inputs are rendered
// on every call; the caller must remove the returned local result after use.
// The caller should use a separate cacheDir for each OPM version.
//
// RETRY_COUNT and RETRY_INTERVAL configure retries after the initial attempt and
// the delay in seconds. Unset or empty values default to 3 and 5 respectively.
// Cancellation stops the process and retries. Failed renders leave no result.
func RenderOPM(ctx context.Context, target, opmPath, cacheDir string) (string, error) {
	// TODO: Implement after reviewing the red contract tests.
	return "", errors.New("RenderOPM: not implemented")
}
