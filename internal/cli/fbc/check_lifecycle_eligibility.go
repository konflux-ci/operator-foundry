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
	"fmt"
	"os"
	"strings"

	"github.com/konflux-ci/operator-foundry/pkg/lifecycle"
	"github.com/spf13/cobra"
)

func newCheckLifecycleEligibilityCmd() *cobra.Command {
	var dockerfilePath string
	var buildContextPath string
	var outputFile string
	var buildArgFlags []string

	cmd := &cobra.Command{
		Use:   "check-lifecycle-eligibility",
		Short: "Check whether an FBC is eligible for lifecycle injection",
		Long: `Checks whether the File-Based Catalog (FBC) is eligible for
lifecycle injection, based on whether all OCP versions targeted by
the Dockerfile are >= the minimum supported version.

If the base image tag references a build ARG (e.g.
FROM ...:${CATALOG_VERSION}), pass its value with --build-arg so it can
resolve to the same tag the image is actually built with.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			buildArgs, err := parseBuildArgs(buildArgFlags)
			if err != nil {
				return err
			}

			eligible, err := lifecycle.CheckLifecycleEligibility(dockerfilePath, buildContextPath, buildArgs)
			if err != nil {
				return err
			}

			output := "false"
			if eligible {
				output = "true"
			}

			if outputFile != "" {
				if err := os.WriteFile(outputFile, []byte(output+"\n"), 0644); err != nil {
					return err
				}
			} else {
				fmt.Println(output)
			}
			return nil
		},
	}

	cmd.Flags().StringVar(&dockerfilePath, "dockerfile", "", "Path to the FBC Dockerfile (required)")
	cmd.Flags().StringVar(&buildContextPath, "build-context", "", "Path to the build context directory (required)")
	cmd.Flags().StringVar(&outputFile, "output", "", "Path to write eligibility result (default: stdout)")
	cmd.Flags().StringArrayVar(&buildArgFlags, "build-arg", nil, "Build arg used to resolve ARG references in the base image tag, as KEY=VALUE (may be repeated)")

	for _, flag := range []string{"dockerfile", "build-context"} {
		if err := cmd.MarkFlagRequired(flag); err != nil {
			panic(err)
		}
	}

	return cmd
}

// parseBuildArgs parses "KEY=VALUE" flag values into a map.
func parseBuildArgs(flags []string) (map[string]string, error) {
	if len(flags) == 0 {
		return nil, nil
	}
	buildArgs := make(map[string]string, len(flags))
	for _, flag := range flags {
		key, value, ok := strings.Cut(flag, "=")
		if !ok || key == "" {
			return nil, fmt.Errorf("invalid --build-arg %q: expected KEY=VALUE", flag)
		}
		buildArgs[key] = value
	}
	return buildArgs, nil
}
