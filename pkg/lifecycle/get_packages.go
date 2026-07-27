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

	"github.com/keilerkonzept/dockerfile-json/pkg/dockerfile"
)

// GetPackages parses the Dockerfile and extracts OLM package names from its
// COPY instructions.
//
// buildArgs resolves any ARG references used in COPY/ADD source paths and
// should match the build-args the image is actually built with. It may be nil.
func GetPackages(dockerfilePath, buildContextPath string, buildArgs map[string]string) ([]string, error) {
	resolvedPath, err := resolveDockerfilePath(dockerfilePath, buildContextPath)
	if err != nil {
		return nil, err
	}

	d, err := dockerfile.Parse(resolvedPath)
	if err != nil {
		return nil, fmt.Errorf("failed to parse dockerfile %q: %w", resolvedPath, err)
	}

	entries, err := ParseCopyInstructionsForConfigs(d, buildArgs)
	if err != nil {
		return nil, fmt.Errorf("failed to parse COPY instructions: %w", err)
	}

	return ExtractPackageNames(entries, buildContextPath)
}
