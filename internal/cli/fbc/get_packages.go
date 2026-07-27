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
	var buildArgFlags []string

	cmd := &cobra.Command{
		Use:   "get-packages",
		Short: "Determine OLM package names targeted by an FBC Dockerfile",
		Long: `Determines the OLM packages included in a File-Based Catalog (FBC)
by parsing the COPY/ADD instructions in the provided Dockerfile
and inspecting the corresponding catalog subdirectories in the build context.

If a COPY/ADD source path references a build ARG (e.g.
COPY ./${INPUT_DIR}/ /configs/my-operator), pass its value with --build-arg so
it can resolve to the same path the image is actually built with.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			buildArgs, err := parseBuildArgs(buildArgFlags)
			if err != nil {
				return err
			}

			packages, err := lifecycle.GetPackages(dockerfilePath, buildContextPath, buildArgs)
			if err != nil {
				return err
			}
			output := strings.Join(packages, ",")
			if outputFile != "" {
				if err := os.WriteFile(outputFile, []byte(output+"\n"), 0644); err != nil {
					return err
				}
			} else if len(packages) > 0 {
				fmt.Println(output)
			}
			return nil
		},
	}

	cmd.Flags().StringVar(&dockerfilePath, "dockerfile", "", "Path to the FBC Dockerfile (required)")
	cmd.Flags().StringVar(&buildContextPath, "build-context", "", "Path to the build context directory (required)")
	cmd.Flags().StringVar(&outputFile, "output", "", "Path to write package names (default: stdout)")
	cmd.Flags().StringArrayVar(&buildArgFlags, "build-arg", nil, "Build arg used to resolve ARG references in COPY/ADD source paths, as KEY=VALUE (may be repeated)")

	for _, flag := range []string{"dockerfile", "build-context"} {
		if err := cmd.MarkFlagRequired(flag); err != nil {
			panic(err)
		}
	}

	return cmd
}
