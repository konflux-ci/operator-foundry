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

	"github.com/konflux-ci/operator-foundry/pkg/lifecycle"
	"github.com/spf13/cobra"
)

func newCheckLifecycleEligibilityCmd() *cobra.Command {
	var dockerfilePath string
	var buildContextPath string
	var outputFile string

	cmd := &cobra.Command{
		Use:   "check-lifecycle-eligibility",
		Short: "Check whether an FBC is eligible for lifecycle injection",
		Long: `Checks whether the File-Based Catalog (FBC) is eligible for
lifecycle injection, based on whether all OCP versions targeted by
the Dockerfile are >= the minimum supported version.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			eligible, err := lifecycle.CheckLifecycleEligibility(dockerfilePath, buildContextPath)
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
	cmd.Flags().StringVar(&buildContextPath, "build-context", ".", "Path to the build context directory (default: .)")
	cmd.Flags().StringVar(&outputFile, "output", "", "Path to write eligibility result (default: stdout)")

	if err := cmd.MarkFlagRequired("dockerfile"); err != nil {
		panic(err)
	}

	return cmd
}
