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
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// InjectLifecycleJSON copies a pre-generated lifecycle.json file into the
// catalog directory for a given package within the build context.
// It handles three COPY patterns:
//   - COPY catalog /configs                         → writes to <buildContextPath>/catalog/<pkg>/lifecycle.json
//   - COPY catalog/my-operator /configs/my-operator → writes to <buildContextPath>/catalog/my-operator/lifecycle.json
//   - COPY catalog /configs/my-operator             → writes to <buildContextPath>/catalog/my-operator/lifecycle.json
//
// entries must already have variables resolved — use ParseCopyInstructionsForConfigs to obtain them.
func InjectLifecycleJSON(lifecycleJSONPath, buildContextPath, pkg string, entry DockerfileCopyEntry) error {
	if entry.IsFromBuildStage() {
		return fmt.Errorf("cannot inject lifecycle.json into build stage dependencies (COPY --from=%s)", entry.From)
	}

	if pkg == "" || pkg == "." || pkg == ".." || strings.ContainsAny(pkg, "/\\") {
		return fmt.Errorf("invalid package name %q: must not be empty, '.', '..', or contain path separators", pkg)
	}

	data, err := os.ReadFile(lifecycleJSONPath)
	if err != nil {
		return fmt.Errorf("failed to read lifecycle.json from %q: %w", lifecycleJSONPath, err)
	}

	dest := strings.Trim(entry.Dest, "/")

	var pkgFromDest string
	if strings.HasPrefix(dest, "configs/") {
		pkgFromDest = strings.SplitN(strings.TrimPrefix(dest, "configs/"), "/", 2)[0]
	}

	if pkgFromDest != "" && pkgFromDest != pkg {
		return fmt.Errorf("entry destination %q targets package %q, not %q", entry.Dest, pkgFromDest, pkg)
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
			return fmt.Errorf("invalid source path detected: %w", err)
		}

		info, err := os.Stat(pkgDir)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			return fmt.Errorf("failed to stat package directory %q: %w", pkgDir, err)
		}

		if !info.IsDir() {
			continue
		}

		if err := os.WriteFile(filepath.Join(pkgDir, "lifecycle.json"), data, os.FileMode(0644)); err != nil {
			return fmt.Errorf("failed to write lifecycle.json for package %q: %w", pkg, err)
		}

		injected = true
	}
	if !injected {
		return fmt.Errorf("could not find catalog directory for package %q in entry srcs %v", pkg, entry.Srcs)
	}

	return nil
}
