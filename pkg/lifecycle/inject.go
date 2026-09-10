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
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// hasLifecycleSchema reports whether data contains the lifecycle schema
// 'io.openshift.operators.lifecycles.v1alpha1'. Supports JSON, JSON-lines, and YAML formats.
func hasLifecycleSchema(data []byte) bool {
	jsonMatched := false
	jsonFullyParsed := true

	dec := json.NewDecoder(bytes.NewReader(data))
	for {
		var obj struct {
			Schema string `json:"schema" yaml:"schema"`
		}
		if err := dec.Decode(&obj); err != nil {
			if !errors.Is(err, io.EOF) {
				slog.Debug("failed to decode JSON while checking lifecycle schema", "error", err)
				jsonFullyParsed = false
			}
			break
		}
		if obj.Schema == "io.openshift.operators.lifecycles.v1alpha1" {
			return true
		}
		jsonMatched = true
	}

	// skip YAML pass if JSON successfully parsed all content
	if jsonMatched || jsonFullyParsed {
		return false
	}

	yamlDec := yaml.NewDecoder(bytes.NewReader(data))
	for {
		var obj struct {
			Schema string `json:"schema" yaml:"schema"`
		}
		if err := yamlDec.Decode(&obj); err != nil {
			if !errors.Is(err, io.EOF) {
				slog.Debug("failed to decode YAML while checking lifecycle schema", "error", err)
			}
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

// InjectLifecycleJSON copies a pre-generated lifecycle.json into the catalog directory
// for a given package within the build context. Handles two COPY dest patterns:
//   - dest=/configs: src must contain a subdirectory named <pkg>; lifecycle.json is written there.
//   - dest=/configs/<pkg>: src IS the catalog directory for the package regardless of its name;
//     lifecycle.json is written directly into src. A guard rejects src that degenerates to "."
//     (e.g. when a build ARG is unresolved).
//
// Returns (true, nil) on success, (false, nil) if no matching directory was found,
// or (false, err) on failure.
//
// Errors if: the source file lacks the lifecycle schema, a file with the lifecycle
// schema already exists in the destination directory, or the dest path is deeper
// than /configs/<package-name>. Not idempotent.
//
// entry must have variables resolved — use ParseCopyInstructionsForConfigs.
func InjectLifecycleJSON(lifecycleJSONPath, buildContextPath, pkg string, entry DockerfileCopyEntry) (bool, error) {
	if entry.IsFromBuildStage() {
		return false, fmt.Errorf("cannot inject lifecycle.json into build stage dependencies (COPY --from=%s)", entry.From)
	}

	if err := validatePackageName(pkg); err != nil {
		return false, err
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
		if pkgFromDest != "" && filepath.Clean(src) != "." {
			// dest = /configs/<pkg>: src IS the catalog directory regardless of its name.
			// e.g. COPY ./catalog-4-22/ /configs/gatekeeper-operator-product
			// Guard against src degenerating to "." when a build ARG is unresolved.
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
			return false, fmt.Errorf("lifecycle data already exists for package %q at %q, refusing to overwrite", pkg, pkgDir)
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

// injectLifecycleJSONAtDir copies lifecycle.json into pkgDir, which must already exist.
// Returns an error if the source file lacks the lifecycle schema, if lifecycle data
// already exists in pkgDir, or if the write fails.
func injectLifecycleJSONAtDir(lifecycleJSONPath, pkgDir, pkg string) error {
	if info, err := os.Stat(pkgDir); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("catalog directory %q does not exist", pkgDir)
		}
		return fmt.Errorf("failed to stat catalog directory %q: %w", pkgDir, err)
	} else if !info.IsDir() {
		return fmt.Errorf("catalog path %q is not a directory", pkgDir)
	}

	data, err := os.ReadFile(lifecycleJSONPath)
	if err != nil {
		return fmt.Errorf("failed to read lifecycle.json from %q: %w", lifecycleJSONPath, err)
	}

	if !hasLifecycleSchema(data) {
		return fmt.Errorf("lifecycle.json at %q does not contain expected schema 'io.openshift.operators.lifecycles.v1alpha1'", lifecycleJSONPath)
	}

	exists, err := lifecycleSchemaExistsInDir(pkgDir)
	if err != nil {
		return fmt.Errorf("failed to check lifecycle schema in %q: %w", pkgDir, err)
	}
	if exists {
		return fmt.Errorf("lifecycle data already exists for package %q at %q, refusing to overwrite", pkg, pkgDir)
	}

	destPath := filepath.Join(pkgDir, "lifecycle.json")
	f, err := os.OpenFile(destPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0644)
	if err != nil {
		if errors.Is(err, os.ErrExist) {
			return fmt.Errorf("lifecycle.json already exists at %q, refusing to overwrite", destPath)
		}
		return fmt.Errorf("failed to create lifecycle.json at %q: %w", destPath, err)
	}

	_, writeErr := f.Write(data)
	closeErr := f.Close()

	if writeErr != nil {
		_ = os.Remove(destPath)
		return fmt.Errorf("failed to write lifecycle.json for package %q: %w", pkg, writeErr)
	}
	if closeErr != nil {
		_ = os.Remove(destPath)
		return fmt.Errorf("failed to close lifecycle.json for package %q: %w", pkg, closeErr)
	}

	return nil
}
