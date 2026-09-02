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
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/konflux-ci/operator-foundry/pkg/image"
	"github.com/konflux-ci/operator-foundry/pkg/testoutput"
	"github.com/spf13/cobra"
)

const tektonOutputNoteLimit = 3500

func newCheckRelatedImagesMediatypeCmd() *cobra.Command {
	var ocpVersion string
	var relatedImagesPath string
	var outputFile string

	cmd := &cobra.Command{
		Use:   "check-related-images-mediatype",
		Short: "Check related images for incompatible OCI mediatypes",
		Long: `Checks whether the list of related bundle images saved as JSON is compliant 
with the condition that OCI mediaType is not present for OCP < v4.21.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			// Read the JSON list with related image references
			relatedImages, err := loadRelatedImages(relatedImagesPath)
			if err != nil {
				return err
			}

			// Run the MediaType compatibility check against the group of images saved in relatedImages
			fetcher := &image.RegistryFetcher{Timeout: 120 * time.Second}
			result, err := image.CheckRelatedImagesMediaType(cmd.Context(), ocpVersion, relatedImages, fetcher)
			if err != nil {
				return err
			}

			// Format the result as Tekton TEST_OUTPUT JSON
			var output string
			if result.Passed {
				output, err = testoutput.MakeResultJSON(
					testoutput.ResultSuccess,
					testoutput.WithSuccesses(1),
					testoutput.WithNote("All related images use compatible mediatypes."),
				)
			} else {
				var parts []string
				if len(result.WrongMediaTypeImages) > 0 {
					parts = append(parts, fmt.Sprintf("Related images with incompatible mediatypes: %s",
						strings.Join(result.WrongMediaTypeImages, ", ")))
				}
				if len(result.BrokenImages) > 0 {
					parts = append(parts, fmt.Sprintf("Broken images: %s",
						strings.Join(result.BrokenImages, ", ")))
				}
				note := strings.Join(parts, "; ")
				if len(note) > tektonOutputNoteLimit {
					// crop the note, so we do not hit the limit of MakeResultJSON
					// actual limit is the Tekton result size of 4096 bytes for the entire JSON output
					note = string([]rune(note)[:tektonOutputNoteLimit]) + "..."
				}
				output, err = testoutput.MakeResultJSON(
					testoutput.ResultFailure,
					testoutput.WithFailures(1),
					testoutput.WithNote(note),
				)
			}
			if err != nil {
				return err
			}

			// Write result to file or stdout
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

	cmd.Flags().StringVar(&ocpVersion, "ocp-version", "", "OCP version to check the related bundle images against (required)")
	cmd.Flags().StringVar(&relatedImagesPath, "related-images-json-path", "", "Path to the file with saved JSON list of related bundle images (required)")
	cmd.Flags().StringVar(&outputFile, "output", "", "Path to write the result (default: stdout)")

	for _, flag := range []string{"ocp-version", "related-images-json-path"} {
		if err := cmd.MarkFlagRequired(flag); err != nil {
			panic(err)
		}
	}

	return cmd
}

func loadRelatedImages(path string) ([]string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var images []string
	if err := json.Unmarshal(data, &images); err != nil {
		return nil, err
	}
	if images == nil {
		return nil, fmt.Errorf("related images JSON must be a non-null array")
	}
	return images, nil
}
