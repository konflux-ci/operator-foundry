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

package lifecycle

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// hasLifecycleSchema reports whether data contains the lifecycle schema
// 'io.openshift.operators.lifecycles.v1alpha1'. Supports JSON, JSON-lines, and YAML formats.
func hasLifecycleSchema(data []byte) bool {
	var obj struct {
		Schema string `json:"schema" yaml:"schema"`
	}

	dec := json.NewDecoder(bytes.NewReader(data))
	for {
		if err := dec.Decode(&obj); err != nil {
			break
		}
		if obj.Schema == "io.openshift.operators.lifecycles.v1alpha1" {
			return true
		}
	}

	yamlDec := yaml.NewDecoder(bytes.NewReader(data))
	for {
		if err := yamlDec.Decode(&obj); err != nil {
			break
		}
		if obj.Schema == "io.openshift.operators.lifecycles.v1alpha1" {
			return true
		}
	}

	return false
}

// lifecycleSchemaExistsInDir reports whether any .json, .yaml, or .yml file
// in pkgDir contains the lifecycle schema 'io.openshift.operators.lifecycles.v1alpha1'.
func lifecycleSchemaExistsInDir(pkgDir string) (bool, error) {
	entries, err := os.ReadDir(pkgDir)
	if err != nil {
		return false, fmt.Errorf("failed to read directory %q: %w", pkgDir, err)
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		ext := strings.ToLower(filepath.Ext(e.Name()))
		if ext != ".json" && ext != ".yaml" && ext != ".yml" {
			continue
		}
		data, err := os.ReadFile(filepath.Join(pkgDir, e.Name()))
		if err != nil {
			slog.Warn("failed to read file while checking lifecycle schema", "file", e.Name(), "error", err)
			continue
		}
		if hasLifecycleSchema(data) {
			return true, nil
		}
	}
	return false, nil
}

// InjectLifecycleJSON copies a pre-generated lifecycle.json file into the
// catalog directory for a given package within the build context.
// It handles three COPY patterns:
//   - COPY catalog /configs                         → writes to <buildContextPath>/catalog/<pkg>/lifecycle.json
//   - COPY catalog/my-operator /configs/my-operator → writes to <buildContextPath>/catalog/my-operator/lifecycle.json
//   - COPY catalog /configs/my-operator             → writes to <buildContextPath>/catalog/my-operator/lifecycle.json
//
// Return values:
//   - (true, nil)  — lifecycle.json was successfully injected
//   - (false, nil) — no matching catalog directory found; not an error,
//     the caller is responsible for deciding whether to treat this as a failure
//   - (false, err) — an error occurred during injection
//
// entry must already have variables resolved — use ParseCopyInstructionsForConfigs to obtain them.
//
// The source lifecycle.json is validated to contain the expected schema
// 'io.openshift.operators.lifecycles.v1alpha1' before injection. If the
// schema is missing or invalid, an error is returned.
//
// Note: this function is not idempotent. If a file containing the lifecycle
// schema 'io.openshift.operators.lifecycles.v1alpha1' already exists in the
// destination directory (regardless of filename), it returns an error rather
// than overwriting existing lifecycle data.
//
// Known constraint: destination paths deeper than /configs/<package-name> (e.g.,
// /configs/my-operator/subdir) are rejected. IIB requires the catalog structure
// to be exactly /configs/<package-name>/.
func InjectLifecycleJSON(lifecycleJSONPath, buildContextPath, pkg string, entry DockerfileCopyEntry) (bool, error) {
	if entry.IsFromBuildStage() {
		return false, fmt.Errorf("cannot inject lifecycle.json into build stage dependencies (COPY --from=%s)", entry.From)
	}

	if pkg == "" || pkg == "." || pkg == ".." || strings.ContainsAny(pkg, "/\\") {
		return false, fmt.Errorf("invalid package name %q: must not be empty, '.', '..', or contain path separators", pkg)
	}

	data, err := os.ReadFile(lifecycleJSONPath)
	if err != nil {
		return false, fmt.Errorf("failed to read lifecycle.json from %q: %w", lifecycleJSONPath, err)
	}

	if !hasLifecycleSchema(data) {
		return false, fmt.Errorf("lifecycle.json at %q does not contain expected schema 'io.openshift.operators.lifecycles.v1alpha1'", lifecycleJSONPath)
	}

	dest := strings.Trim(entry.Dest, "/")

	var pkgFromDest string
	if strings.HasPrefix(dest, "configs/") {
		parts := strings.SplitN(strings.TrimPrefix(dest, "configs/"), "/", 2)
		if len(parts) > 1 {
			return false, fmt.Errorf("destination %q is not a valid FBC path: expected /configs or /configs/<package-name>", entry.Dest)
		}
		pkgFromDest = parts[0]
	}

	if pkgFromDest != "" && pkgFromDest != pkg {
		return false, fmt.Errorf("entry destination %q targets package %q, not %q", entry.Dest, pkgFromDest, pkg)
	}

	injected := false

	for _, src := range entry.Srcs {
		subPath := filepath.Join(src, pkg)
		// Cross-reference pkgFromDest to prevent injecting into the catalog root
		// when the source basename coincidentally matches the package name.
		if pkgFromDest != "" && filepath.Base(filepath.Clean(src)) == pkg {
			subPath = src
		}

		pkgDir, err := resolveAndValidatePath(buildContextPath, subPath)
		if err != nil {
			return false, fmt.Errorf("invalid source path detected: %w", err)
		}

		info, err := os.Stat(pkgDir)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			return false, fmt.Errorf("failed to stat package directory %q: %w", pkgDir, err)
		}

		if !info.IsDir() {
			continue
		}

		exists, err := lifecycleSchemaExistsInDir(pkgDir)
		if err != nil {
			return false, fmt.Errorf("failed to check lifecycle schema in %q: %w", pkgDir, err)
		}
		if exists {
			return false, fmt.Errorf("lifecycle data already exists for package %q in %q, refusing to overwrite", pkg, pkgDir)
		}

		destPath := filepath.Join(pkgDir, "lifecycle.json")
		f, err := os.OpenFile(destPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0644)
		if err != nil {
			if errors.Is(err, os.ErrExist) {
				return false, fmt.Errorf("lifecycle.json already exists for package %q at %q, refusing to overwrite", pkg, destPath)
			}
			return false, fmt.Errorf("failed to create lifecycle.json for package %q: %w", pkg, err)
		}

		_, writeErr := f.Write(data)
		closeErr := f.Close()

		if writeErr != nil {
			_ = os.Remove(destPath)
			return false, fmt.Errorf("failed to write lifecycle.json for package %q: %w", pkg, writeErr)
		}

		if closeErr != nil {
			_ = os.Remove(destPath)
			return false, fmt.Errorf("failed to close lifecycle.json for package %q: %w", pkg, closeErr)
		}

		injected = true
	}
	if !injected {
		return false, nil
	}

	return true, nil
}
