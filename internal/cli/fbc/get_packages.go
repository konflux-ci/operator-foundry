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

func newGetPackagesCmd() *cobra.Command {
	var dockerfilePath string
	var buildContextPath string
	var outputFile string

	cmd := &cobra.Command{
		Use:   "get-packages",
		Short: "Determine OLM package names targeted by an FBC Dockerfile",
		Long: `Determines the OLM packages included in a File-Based Catalog (FBC)
by parsing the COPY/ADD instructions in the provided Dockerfile
and inspecting the corresponding catalog subdirectories in the build context.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			packages, err := lifecycle.GetPackages(dockerfilePath, buildContextPath)
			if err != nil {
				return err
			}
			output := strings.Join(packages, ",")
			if outputFile != "" {
				if err := os.WriteFile(outputFile, []byte(output), 0644); err != nil {
					return err
				}
			} else if len(packages) > 0 {
				fmt.Print(output)
			}
			return nil
		},
	}

	cmd.Flags().StringVar(&dockerfilePath, "dockerfile", "", "Path to the FBC Dockerfile (required)")
	cmd.Flags().StringVar(&buildContextPath, "build-context", "", "Path to the build context directory (required)")
	cmd.Flags().StringVar(&outputFile, "output", "", "Path to write package names (default: stdout)")

	for _, flag := range []string{"dockerfile", "build-context"} {
		if err := cmd.MarkFlagRequired(flag); err != nil {
			panic(err)
		}
	}

	return cmd
}
