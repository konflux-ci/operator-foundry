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
	"github.com/moby/buildkit/frontend/dockerfile/instructions"
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

// ParseCopyInstructionsForConfigs returns all ADD/COPY instructions targeting /configs
// from a parsed Dockerfile.
//
// buildArgs resolves any ARG references used in COPY/ADD source or destination
// paths (e.g. COPY ./${INPUT_DIR}/ /configs/my-operator) and takes precedence
// over the ARG's own default value declared in the Dockerfile. It may be nil.
//
// Known limitations:
//   - Bash-style variable modifiers (e.g. ${VAR:-default}) are not supported
//   - Wildcard source paths (e.g. catalog/*) targeting /configs are not supported and return an error
func ParseCopyInstructionsForConfigs(d *dockerfile.Dockerfile, buildArgs map[string]string) ([]DockerfileCopyEntry, error) {
	if d == nil {
		return nil, fmt.Errorf("dockerfile is nil")
	}

	globalArgs := buildGlobalArgMap(d)
	var entries []DockerfileCopyEntry

	for _, stage := range d.Stages {
		envMap := make(map[string]string)
		envKeys := make(map[string]bool)

		expand := func(key string) string {
			if val, ok := envMap[key]; ok {
				return val
			}
			slog.Warn("unresolved variable in Dockerfile instruction, expanding to empty string", "variable", key)
			return ""
		}

		for _, cmd := range stage.Commands {
			// 1. Update the environment state for this point in the stage
			updateEnvState(cmd.Command, envMap, envKeys, globalArgs, buildArgs)

			var srcs []string
			var dest, from string

			// 2. Extract raw fields only if it's an ADD or COPY
			switch c := cmd.Command.(type) {
			case *instructions.AddCommand:
				if len(c.SourcePaths) == 0 {
					continue
				}
				srcs, dest = c.SourcePaths, c.DestPath
			case *instructions.CopyCommand:
				if len(c.SourcePaths) == 0 {
					continue
				}
				srcs, dest, from = c.SourcePaths, c.DestPath, c.From
			default:
				continue
			}

			// 3. Expand, validate, and append using the current state snapshot
			entry, err := createConfigsEntry(srcs, dest, from, expand)
			if err != nil {
				return nil, err
			}
			if entry != nil {
				entries = append(entries, *entry)
			}
		}
	}

	if len(entries) == 0 {
		return nil, fmt.Errorf("no ADD or COPY instruction targeting /configs found")
	}

	return entries, nil
}

// buildGlobalArgMap collects ARG instructions declared before the first FROM.
// These are available to all stages as a base set of variables.
func buildGlobalArgMap(d *dockerfile.Dockerfile) map[string]string {
	globalArgs := make(map[string]string)
	for _, metaArg := range d.MetaArgs {
		if metaArg.Value != nil {
			globalArgs[metaArg.Key] = *metaArg.Value
		}
	}
	return globalArgs
}

// updateEnvState mutates the running environment maps based on ENV and ARG commands.
//
// For ARG commands, buildArgs (externally supplied, e.g. via --build-arg) takes
// precedence over the ARG's own default value declared in the Dockerfile, which
// in turn takes precedence over a same-named global ARG's default.
func updateEnvState(command interface{}, envMap map[string]string, envKeys map[string]bool, globalArgs, buildArgs map[string]string) {
	switch c := command.(type) {
	case *instructions.EnvCommand:
		for _, kv := range c.Env {
			envMap[kv.Key] = kv.Value
			envKeys[kv.Key] = true
		}
	case *instructions.ArgCommand:
		for _, arg := range c.Args {
			if envKeys[arg.Key] {
				continue
			}
			if val, ok := buildArgs[arg.Key]; ok {
				envMap[arg.Key] = val
			} else if arg.Value != nil {
				envMap[arg.Key] = *arg.Value
			} else if val, ok := globalArgs[arg.Key]; ok {
				envMap[arg.Key] = val
			}
		}
	}
}

// ResolveBuilderStageEntries attempts to resolve COPY --from=<stage> entries back
// to their build-context source paths by tracing through the named build stage's
// own COPY instructions. Entries that cannot be resolved remain unchanged
// (IsFromBuildStage() still returns true for them).
//
// Limitation: RUN commands in the builder stage that rename or move paths cannot
// be accounted for — the trace assumes build-context paths survive into the stage
// verbatim. If a RUN renames the catalog directory itself, the resolved path will
// not exist on disk and injection will fail with a clear "directory not found" error.
func ResolveBuilderStageEntries(d *dockerfile.Dockerfile, entries []DockerfileCopyEntry, buildArgs map[string]string) []DockerfileCopyEntry {
	result := make([]DockerfileCopyEntry, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsFromBuildStage() {
			result = append(result, entry)
			continue
		}
		if resolved := tryResolveBuildStageEntry(d, entry, buildArgs); resolved != nil {
			slog.Info("resolved build stage entry to build context", "from", entry.From, "srcs", resolved.Srcs)
			result = append(result, *resolved)
		} else {
			result = append(result, entry)
		}
	}
	return result
}

// tryResolveBuildStageEntry traces a single COPY --from=<stage> entry back to a
// build-context DockerfileCopyEntry. Returns nil if the named stage is not found,
// itself copies its catalog path from another stage, or the source path cannot be
// mapped back to any build-context COPY instruction.
func tryResolveBuildStageEntry(d *dockerfile.Dockerfile, entry DockerfileCopyEntry, buildArgs map[string]string) *DockerfileCopyEntry {
	var targetStage *dockerfile.Stage
	for _, stage := range d.Stages {
		if stage.Stage.Name == entry.From {
			targetStage = stage
			break
		}
	}
	if targetStage == nil {
		return nil
	}

	stageCopies := buildContextCopiesFromStage(targetStage, d, buildArgs)

	resolvedSrcs := make([]string, 0, len(entry.Srcs))
	for _, src := range entry.Srcs {
		ctxPath := mapStagePathToBuildContext(src, stageCopies)
		if ctxPath == "" {
			return nil
		}
		resolvedSrcs = append(resolvedSrcs, ctxPath)
	}

	return &DockerfileCopyEntry{
		Srcs: resolvedSrcs,
		Dest: entry.Dest,
		From: "",
	}
}

// buildContextCopiesFromStage collects all COPY/ADD instructions in the given stage
// that source from the build context (no --from flag), with variables resolved.
func buildContextCopiesFromStage(stage *dockerfile.Stage, d *dockerfile.Dockerfile, buildArgs map[string]string) []DockerfileCopyEntry {
	globalArgs := buildGlobalArgMap(d)
	envMap := make(map[string]string)
	envKeys := make(map[string]bool)

	expand := func(key string) string {
		if val, ok := envMap[key]; ok {
			return val
		}
		return ""
	}

	var copies []DockerfileCopyEntry
	for _, cmd := range stage.Commands {
		updateEnvState(cmd.Command, envMap, envKeys, globalArgs, buildArgs)

		var srcs []string
		var dest, from string
		switch c := cmd.Command.(type) {
		case *instructions.AddCommand:
			if len(c.SourcePaths) == 0 {
				continue
			}
			srcs, dest = c.SourcePaths, c.DestPath
		case *instructions.CopyCommand:
			if len(c.SourcePaths) == 0 {
				continue
			}
			srcs, dest, from = c.SourcePaths, c.DestPath, c.From
		default:
			continue
		}

		if os.Expand(from, expand) != "" {
			continue // nested --from= inside the builder stage; skip, can't trace further
		}

		resolvedSrcs := make([]string, len(srcs))
		for i, s := range srcs {
			resolvedSrcs[i] = os.Expand(s, expand)
		}
		copies = append(copies, DockerfileCopyEntry{
			Srcs: resolvedSrcs,
			Dest: os.Expand(dest, expand),
		})
	}
	return copies
}

// mapStagePathToBuildContext maps a path inside a build stage back to its
// corresponding build-context path using the stage's COPY instruction set.
// Returns "" if no COPY entry covers the path or the match is ambiguous
// (multiple sources and the path is a sub-path of the destination).
//
// Example: stage has COPY .konflux/catalog/ /app/.konflux/catalog/
// and stagePath is /app/.konflux/catalog/my-operator →
// returns .konflux/catalog/my-operator
func mapStagePathToBuildContext(stagePath string, stageCopies []DockerfileCopyEntry) string {
	stagePath = filepath.Clean(stagePath)

	for _, c := range stageCopies {
		dest := filepath.Clean(c.Dest)
		rel, err := filepath.Rel(dest, stagePath)
		if err != nil || strings.HasPrefix(rel, "..") {
			continue
		}
		if len(c.Srcs) != 1 {
			// Multiple sources: can't determine which src owns a sub-path.
			if rel == "." {
				return "" // ambiguous
			}
			continue
		}
		src := filepath.Clean(c.Srcs[0])
		if rel == "." {
			return src
		}
		return filepath.Join(src, rel)
	}
	return ""
}

// createConfigsEntry expands variables and validates the COPY/ADD instruction.
// Returns nil, nil if the destination does not target /configs.
func createConfigsEntry(srcs []string, dest, from string, expand func(string) string) (*DockerfileCopyEntry, error) {
	resolvedSrcs := make([]string, len(srcs))
	for i, src := range srcs {
		resolvedSrcs[i] = os.Expand(src, expand)
	}
	resolvedDest := os.Expand(dest, expand)
	resolvedFrom := os.Expand(from, expand)

	if resolvedDest != "/configs" && resolvedDest != "/configs/" && !strings.HasPrefix(resolvedDest, "/configs/") {
		return nil, nil
	}

	for _, src := range resolvedSrcs {
		if strings.ContainsAny(src, "*?[]") {
			return nil, fmt.Errorf("wildcard source paths are not supported: %q", src)
		}
	}

	return &DockerfileCopyEntry{
		Srcs: resolvedSrcs,
		Dest: resolvedDest,
		From: resolvedFrom,
	}, nil
}
