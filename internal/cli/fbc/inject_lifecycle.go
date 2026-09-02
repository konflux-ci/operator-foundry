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

package fbc

import (
	"github.com/konflux-ci/operator-foundry/pkg/lifecycle"
	"github.com/spf13/cobra"
)

func newInjectLifecycleCmd() *cobra.Command {
	var dockerfilePath string
	var buildContextPath string
	var lifecycleDir string
	var packages string
	var catalogPath string
	var buildArgFlags []string

	cmd := &cobra.Command{
		Use:   "inject-lifecycle",
		Short: "Inject lifecycle.json into FBC catalog source directories",
		Long: `Injects pre-generated lifecycle.json files into the catalog source
directories for the given OLM packages.

This command does not check OCP eligibility. Callers are expected to have
already confirmed the Dockerfile is eligible for lifecycle injection via
"fbc check-lifecycle-eligibility" before calling this command.

If --catalog-path is provided, it is used as the catalog directory for the
package (relative to --build-context) and Dockerfile parsing is skipped
entirely. lifecycle.json is injected directly into that directory. Use this
when the only COPY instruction targeting /configs uses --from=builder (the
source is inside a builder stage, not on local disk).

If --catalog-path is omitted, the Dockerfile is parsed and COPY/ADD
instructions targeting /configs are used to locate the catalog source
directories. If a source path references a build ARG (e.g.
COPY ./${INPUT_DIR}/ /configs/my-operator), pass its value with --build-arg
so the actual catalog directory on disk can be located.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			buildArgs, err := parseBuildArgs(buildArgFlags)
			if err != nil {
				return err
			}

			return lifecycle.InjectLifecycle(dockerfilePath, buildContextPath, lifecycleDir, packages, catalogPath, buildArgs)
		},
	}

	cmd.Flags().StringVar(&dockerfilePath, "dockerfile", "", "Path to the FBC Dockerfile (required)")
	cmd.Flags().StringVar(&buildContextPath, "build-context", "", "Path to the build context directory (required)")
	cmd.Flags().StringVar(&lifecycleDir, "lifecycle-dir", "", "Directory containing per-package lifecycle.json files, structured as <dir>/<package>/lifecycle.json (required)")
	cmd.Flags().StringVar(&packages, "packages", "", "Comma-separated list of package names (required)")
	cmd.Flags().StringVar(&catalogPath, "catalog-path", "", "Catalog directory relative to --build-context (e.g. .konflux/catalog/my-operator). When set, Dockerfile parsing is skipped and lifecycle.json is injected directly into this directory, which is created if absent.")
	cmd.Flags().StringArrayVar(&buildArgFlags, "build-arg", nil, "Build arg used to resolve ARG references in COPY/ADD source paths, as KEY=VALUE (may be repeated)")

	for _, flag := range []string{"dockerfile", "build-context", "lifecycle-dir", "packages"} {
		if err := cmd.MarkFlagRequired(flag); err != nil {
			panic(err)
		}
	}

	return cmd
}
