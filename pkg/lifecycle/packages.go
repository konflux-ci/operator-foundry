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
)

// DockerfileCopyEntry represents an ADD or COPY instruction targeting /configs
type DockerfileCopyEntry struct {
	Srcs []string // source paths — local (relative to Dockerfile) or builder stage paths
	Dest string   // destination path inside the built image
	From string   // non-empty if COPY --from=<stage> — Srcs are inside a build stage, not local
}

// IsFromBuildStage returns true if this entry copies from a build stage
// rather than from the local source tree.
func (e DockerfileCopyEntry) IsFromBuildStage() bool {
	return e.From != ""
}

// ExtractPackageNames attempts to find all OLM package names targeted by the
// FBC Dockerfile. It tries two options in order:
//   - Option A: extract directly from Dest paths (e.g. /configs/my-operator)
//   - Option B: collect subdirectory names from resolved local source directories,
//     assuming the structure <catalogDir>/<package-name>/catalog.[yaml|yml|json]
//
// Returns an error only if both options fail to find any package names.
func ExtractPackageNames(entries []DockerfileCopyEntry, buildContextPath string) ([]string, error) {
	// Option A: extract package name directly from Dest path.
	packagesFromDest := extractPackageNamesFromDest(entries)
	if len(packagesFromDest) > 0 {
		return deduplicate(packagesFromDest), nil
	}

	slog.Info("no package names found in COPY/ADD destinations, scanning source directories")

	// Option B: subdirectory names under resolved source dirs are the package names.
	// Build-stage entries are skipped — their Srcs are inside a build stage, not local.
	var packagesFromSubdirs []string
	for _, entry := range entries {
		if entry.IsFromBuildStage() {
			continue
		}
		for _, src := range entry.Srcs {
			candidateAbs, err := resolveAndValidatePath(buildContextPath, src)
			if err != nil {
				return nil, err
			}

			info, err := os.Stat(candidateAbs)
			if err != nil {
				return nil, fmt.Errorf("failed to stat path %q: %w", candidateAbs, err)
			}
			if !info.IsDir() {
				slog.Debug("skipping non-directory source path", "path", candidateAbs)
				continue
			}

			slog.Debug("scanning catalog directory one level deep", "dir", candidateAbs)
			subdirs, err := os.ReadDir(candidateAbs)
			if err != nil {
				return nil, fmt.Errorf("failed to read catalog directory %q: %w", candidateAbs, err)
			}
			for _, subdir := range subdirs {
				if !subdir.IsDir() || strings.HasPrefix(subdir.Name(), ".") {
					continue
				}
				slog.Debug("found package subdirectory", "name", subdir.Name())
				packagesFromSubdirs = append(packagesFromSubdirs, subdir.Name())
			}
		}
	}
	if len(packagesFromSubdirs) > 0 {
		return deduplicate(packagesFromSubdirs), nil
	}

	return nil, fmt.Errorf("could not determine package name(s) from Dockerfile")
}

// extractPackageNamesFromDest extracts package names from Dest paths of the
// form /configs/<package-name>. Entries with Dest exactly /configs or /configs/
// are skipped — they require scanning the source directories.
func extractPackageNamesFromDest(entries []DockerfileCopyEntry) []string {
	var packages []string
	for _, entry := range entries {
		dest := strings.Trim(entry.Dest, "/")
		if dest == "configs" {
			continue
		}
		if !strings.HasPrefix(dest, "configs/") {
			continue
		}
		pkgName := strings.SplitN(strings.TrimPrefix(dest, "configs/"), "/", 2)[0]
		if pkgName != "" {
			packages = append(packages, pkgName)
		}
	}
	return packages
}

// deduplicate returns a new slice with duplicate strings removed,
// preserving the order of first occurrence.
func deduplicate(s []string) []string {
	seen := make(map[string]bool, len(s))
	var result []string
	for _, v := range s {
		if !seen[v] {
			seen[v] = true
			result = append(result, v)
		}
	}
	return result
}

// resolveAndValidatePath securely joins subPath to baseContext,
// returning the resolved absolute path. Returns an error if subPath is absolute
// or if the resolved path escapes baseContext via directory traversal.
func resolveAndValidatePath(baseContext, subPath string) (string, error) {
	if filepath.IsAbs(subPath) {
		return "", fmt.Errorf("sub path %q must be relative to the build context", subPath)
	}

	ctxAbs, err := filepath.Abs(baseContext)
	if err != nil {
		return "", fmt.Errorf("failed to resolve base context path %q: %w", baseContext, err)
	}

	candidateAbs, err := filepath.Abs(filepath.Join(ctxAbs, subPath))
	if err != nil {
		return "", fmt.Errorf("failed to resolve target path %q: %w", subPath, err)
	}

	rel, err := filepath.Rel(ctxAbs, candidateAbs)
	if err != nil || strings.HasPrefix(rel, "..") {
		return "", fmt.Errorf("path %q (resolved: %q) escapes build context %q", subPath, candidateAbs, ctxAbs)
	}

	return candidateAbs, nil
}
