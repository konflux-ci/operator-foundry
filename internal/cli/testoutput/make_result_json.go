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

package testoutput

import (
	"fmt"

	"github.com/konflux-ci/operator-foundry/pkg/testoutput"
	"github.com/spf13/cobra"
)

func NewMakeResultJSONCmd() *cobra.Command {
	var result string
	var note string
	var namespace string
	var successes int
	var failures int
	var warnings int

	cmd := &cobra.Command{
		Use:   "make-result-json",
		Short: "Generate a Tekton TEST_OUTPUT JSON result",
		Long: `Generates a TEST_OUTPUT JSON result for Tekton tasks.
The result field must be one of: SUCCESS, FAILURE, ERROR, WARNING, SKIPPED.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			opts := []testoutput.Option{
				testoutput.WithSuccesses(successes),
				testoutput.WithFailures(failures),
				testoutput.WithWarnings(warnings),
			}
			if cmd.Flags().Changed("note") {
				opts = append(opts, testoutput.WithNote(note))
			}
			if cmd.Flags().Changed("namespace") {
				opts = append(opts, testoutput.WithNamespace(namespace))
			}

			out, err := testoutput.MakeResultJSON(testoutput.Result(result), opts...)
			if err != nil {
				return err
			}
			fmt.Println(out)
			return nil
		},
	}

	cmd.Flags().StringVar(&result, "result", "", "Result value: SUCCESS, FAILURE, ERROR, WARNING, SKIPPED (required)")
	cmd.Flags().StringVar(&note, "note", "", "Note to include in the result")
	cmd.Flags().StringVar(&namespace, "namespace", "", "Namespace to include in the result")
	cmd.Flags().IntVar(&successes, "successes", 0, "Number of successes")
	cmd.Flags().IntVar(&failures, "failures", 0, "Number of failures")
	cmd.Flags().IntVar(&warnings, "warnings", 0, "Number of warnings")

	for _, flag := range []string{"result"} {
		if err := cmd.MarkFlagRequired(flag); err != nil {
			panic(err)
		}
	}

	return cmd
}
