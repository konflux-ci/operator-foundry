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

const lifecycleMinOCPVersion = "5.0"

// CheckLifecycleEligibility parses dockerfilePath and reports whether all
// targeted OCP versions are >= lifecycleMinOCPVersion, i.e. whether the FBC
// is eligible for lifecycle injection.
//
// buildArgs resolves any ARG references used in the base image tag (e.g.
// FROM ...:${CATALOG_VERSION}) and should match the build-args the image is
// actually built with. It may be nil.
//
// Returns (false, err) if the Dockerfile cannot be parsed, if OCP versions
// cannot be determined from the Dockerfile (e.g. malformed or missing
// version label), or if version comparison fails (e.g. invalid version
// format). Returns (false, nil) if the Dockerfile parses successfully but
// at least one targeted OCP version is below lifecycleMinOCPVersion.
// Returns (true, nil) if all targeted OCP versions are >= lifecycleMinOCPVersion.
func CheckLifecycleEligibility(dockerfilePath, buildContextPath string, buildArgs map[string]string) (bool, error) {
	resolvedPath, err := resolveDockerfilePath(dockerfilePath, buildContextPath)
	if err != nil {
		return false, err
	}

	d, err := dockerfile.Parse(resolvedPath)
	if err != nil {
		return false, fmt.Errorf("failed to parse dockerfile %q: %w", resolvedPath, err)
	}

	ocpVersions, err := ocp.GetOCPVersionsFromDockerfile(d, buildArgs)
	if err != nil {
		return false, fmt.Errorf("failed to get OCP versions: %w", err)
	}

	gte, err := ocp.AllOCPVersionsGTE(ocpVersions, lifecycleMinOCPVersion)
	if err != nil {
		return false, fmt.Errorf("failed to compare OCP versions: %w", err)
	}

	if !gte {
		slog.Info("not all OCP versions >= minimum version, not eligible for lifecycle injection",
			"min_version", lifecycleMinOCPVersion,
			"versions", ocpVersions,
			"dockerfile", resolvedPath,
		)
	}

	return gte, nil
}
