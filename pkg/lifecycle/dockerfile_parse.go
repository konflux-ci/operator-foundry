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
// Known limitations:
//   - Bash-style variable modifiers (e.g. ${VAR:-default}) are not supported
//   - ARG values injected via Tekton BUILD_ARGS or BUILD_ARGS_FILE are not resolved
//   - Wildcard source paths (e.g. catalog/*) targeting /configs are not supported and return an error
func ParseCopyInstructionsForConfigs(d *dockerfile.Dockerfile) ([]DockerfileCopyEntry, error) {
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
			updateEnvState(cmd.Command, envMap, envKeys, globalArgs)

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
func updateEnvState(command interface{}, envMap map[string]string, envKeys map[string]bool, globalArgs map[string]string) {
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
			if arg.Value != nil {
				envMap[arg.Key] = *arg.Value
			} else if val, ok := globalArgs[arg.Key]; ok {
				envMap[arg.Key] = val
			}
		}
	}
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
