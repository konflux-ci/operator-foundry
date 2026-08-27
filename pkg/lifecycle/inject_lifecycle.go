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
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/keilerkonzept/dockerfile-json/pkg/dockerfile"
)

// InjectLifecycle injects pre-generated lifecycle.json files from lifecycleDir
// into the catalog source directories for the given packages. This function
// does not check OCP eligibility; callers are expected to have already
// confirmed eligibility via CheckLifecycleEligibility before calling
// this function.
//
// buildArgs resolves any ARG references used in COPY/ADD source paths (e.g.
// COPY ./${INPUT_DIR}/ /configs/my-operator) so the actual catalog directory
// on disk can be located, and should match the build-args the image is
// actually built with. It may be nil.
//
// Returns an error if any package fails to inject.
func InjectLifecycle(dockerfilePath, buildContextPath, lifecycleDir, packages string, buildArgs map[string]string) error {
	resolvedPath, err := resolveDockerfilePath(dockerfilePath, buildContextPath)
	if err != nil {
		return err
	}

	d, err := dockerfile.Parse(resolvedPath)
	if err != nil {
		return fmt.Errorf("failed to parse dockerfile %q: %w", resolvedPath, err)
	}

	entries, err := ParseCopyInstructionsForConfigs(d, buildArgs)
	if err != nil {
		return fmt.Errorf("failed to parse COPY instructions: %w", err)
	}

	entries = ResolveBuilderStageEntries(d, entries, buildArgs)

	if strings.Trim(packages, " ,") == "" {
		return fmt.Errorf("packages list must contain at least one valid package name")
	}

	rawPackages := strings.Split(packages, ",")
	var cleanedPackages []string
	for _, pkg := range rawPackages {
		trimmed := strings.TrimSpace(pkg)
		if trimmed != "" {
			cleanedPackages = append(cleanedPackages, trimmed)
		}
	}

	if len(cleanedPackages) == 0 {
		return fmt.Errorf("packages list must contain at least one valid package name")
	}

	packageNames := deduplicate(cleanedPackages)
	slog.Info("injecting lifecycle for packages", "packages", packageNames)

	for _, pkg := range packageNames {
		validatedPkg, err := resolveAndValidatePath(lifecycleDir, pkg)
		if err != nil {
			return fmt.Errorf("invalid package name %q: %w", pkg, err)
		}

		lifecycleJSONPath := filepath.Join(validatedPkg, "lifecycle.json")

		if _, err := os.Stat(lifecycleJSONPath); err != nil {
			return fmt.Errorf("lifecycle.json not found for package %q in %q: %w", pkg, lifecycleDir, err)
		}

		injected := false
		for _, entry := range entries {
			if entry.IsFromBuildStage() {
				slog.Info("skipping build stage entry", "package", pkg, "from", entry.From)
				continue
			}

			if !destTargetsPackage(entry.Dest, pkg) {
				slog.Info("skipping COPY entry for different package", "package", pkg, "dest", entry.Dest)
				continue
			}

			slog.Info("injecting lifecycle.json for entry", "package", pkg, "src", entry.Srcs, "dest", entry.Dest)
			ok, err := InjectLifecycleJSON(lifecycleJSONPath, buildContextPath, pkg, entry)
			if err != nil {
				return fmt.Errorf("failed to inject lifecycle.json for package %q: %w", pkg, err)
			}
			if ok {
				injected = true
				break
			}
		}
		if !injected {
			return fmt.Errorf("lifecycle.json was not injected for package %q: no matching catalog directory found under any COPY entry source", pkg)
		}

		slog.Info("injected lifecycle.json", "package", pkg)
	}

	return nil
}

// destTargetsPackage returns true if the entry destination is /configs (applies to all packages)
// or /configs/<pkg> (targets this specific package).
// Returns false for deep sub-paths like /configs/<pkg>/subdir, which are not valid FBC paths.
func destTargetsPackage(dest, pkg string) bool {
	d := strings.Trim(dest, "/")
	if d == "configs" {
		return true
	}
	if !strings.HasPrefix(d, "configs/") {
		return false
	}
	parts := strings.SplitN(strings.TrimPrefix(d, "configs/"), "/", 2)
	if len(parts) > 1 {
		return false
	}
	return parts[0] == pkg
}
