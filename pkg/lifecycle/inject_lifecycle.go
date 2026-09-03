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
// If catalogPath is non-empty, it is used as the parent catalog directory
// (relative to buildContextPath) and Dockerfile parsing is skipped entirely;
// dockerfilePath is ignored. lifecycle.json is injected into catalogPath/<pkg>/
// for each package. Use this when all COPY instructions targeting /configs use
// --from=builder (source is inside a builder stage, not on local disk).
//
// If catalogPath is empty, the Dockerfile is parsed and COPY/ADD instructions
// targeting /configs are used to locate the catalog source directories.
// buildArgs resolves any ARG references in those paths. It may be nil.
//
// Returns an error if any package fails to inject.
func InjectLifecycle(dockerfilePath, buildContextPath, lifecycleDir, packages, catalogPath string, buildArgs map[string]string) error {
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

	for _, pkg := range packageNames {
		if err := validatePackageName(pkg); err != nil {
			return err
		}
	}

	slog.Info("injecting lifecycle for packages", "packages", packageNames)

	if catalogPath != "" {
		return injectLifecycleFromCatalogPath(buildContextPath, lifecycleDir, catalogPath, packageNames)
	}

	return injectLifecycleFromDockerfile(dockerfilePath, buildContextPath, lifecycleDir, packageNames, buildArgs)
}

// injectLifecycleFromCatalogPath injects lifecycle.json using an explicit catalog base directory.
// catalogPath is the parent directory containing per-package subdirectories; lifecycle.json is
// injected into catalogPath/<pkg>/ for each package.
func injectLifecycleFromCatalogPath(buildContextPath, lifecycleDir, catalogPath string, packageNames []string) error {
	for _, pkg := range packageNames {
		lifecycleJSONPath, err := resolveLifecycleJSONPath(lifecycleDir, pkg)
		if err != nil {
			return err
		}

		pkgDir, err := resolveAndValidatePath(buildContextPath, filepath.Join(catalogPath, pkg))
		if err != nil {
			return fmt.Errorf("invalid catalog path for package %q: %w", pkg, err)
		}

		slog.Info("injecting lifecycle.json", "package", pkg, "dir", pkgDir)
		if err := injectLifecycleJSONAtDir(lifecycleJSONPath, pkgDir, pkg); err != nil {
			return fmt.Errorf("failed to inject lifecycle.json for package %q: %w", pkg, err)
		}

		slog.Info("injected lifecycle.json", "package", pkg)
	}
	return nil
}

// injectLifecycleFromDockerfile injects lifecycle.json by parsing the Dockerfile
// to locate catalog source directories from COPY/ADD instructions targeting /configs.
func injectLifecycleFromDockerfile(dockerfilePath, buildContextPath, lifecycleDir string, packageNames []string, buildArgs map[string]string) error {
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

	allFromBuilder := len(entries) > 0
	for _, e := range entries {
		if !e.IsFromBuildStage() {
			allFromBuilder = false
			break
		}
	}
	if allFromBuilder {
		return fmt.Errorf("all COPY/ADD instructions targeting /configs use --from=<stage>: " +
			"the catalog source directory is inside a builder stage and cannot be located on local disk. " +
			"Use --catalog-path to specify the local catalog directory explicitly")
	}

	for _, pkg := range packageNames {
		lifecycleJSONPath, err := resolveLifecycleJSONPath(lifecycleDir, pkg)
		if err != nil {
			return err
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

// resolveLifecycleJSONPath returns the absolute path to lifecycle.json for a package
// and verifies the file exists.
func resolveLifecycleJSONPath(lifecycleDir, pkg string) (string, error) {
	validatedPkg, err := resolveAndValidatePath(lifecycleDir, pkg)
	if err != nil {
		return "", fmt.Errorf("invalid package name %q: %w", pkg, err)
	}
	path := filepath.Join(validatedPkg, "lifecycle.json")
	if _, err := os.Stat(path); err != nil {
		return "", fmt.Errorf("lifecycle.json not found for package %q in %q: %w", pkg, lifecycleDir, err)
	}
	return path, nil
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
