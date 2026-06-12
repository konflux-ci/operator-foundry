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

	"github.com/keilerkonzept/dockerfile-json/pkg/dockerfile"
	"github.com/konflux-ci/operator-foundry/pkg/ocp"
)

// GetPackages parses the Dockerfile and extracts OLM package names.
// Returns an empty slice if not all targeted OCP versions are >= 5.0.
func GetPackages(dockerfilePath, buildContextPath string) ([]string, error) {
	d, err := dockerfile.Parse(dockerfilePath)
	if err != nil {
		return nil, fmt.Errorf("failed to parse dockerfile %q: %w", dockerfilePath, err)
	}

	ocpVersions, err := ocp.GetOCPVersionsFromDockerfile(d)
	if err != nil {
		return nil, fmt.Errorf("failed to get OCP versions: %w", err)
	}

	gte, err := ocp.AllOCPVersionsGTE(ocpVersions, lifecycleMinOCPVersion)
	if err != nil {
		return nil, fmt.Errorf("failed to compare OCP versions: %w", err)
	}
	if !gte {
		slog.Info("not all OCP versions >= 5.0, skipping package extraction",
			"versions", ocpVersions,
			"dockerfile", dockerfilePath,
		)
		return []string{}, nil
	}

	entries, err := ParseCopyInstructionsForConfigs(d)
	if err != nil {
		return nil, fmt.Errorf("failed to parse COPY instructions: %w", err)
	}

	return ExtractPackageNames(entries, buildContextPath)
}
