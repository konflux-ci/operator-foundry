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
	"github.com/konflux-ci/operator-foundry/pkg/fbcvalidation"
	"github.com/spf13/cobra"
)

func newValidateCmd() *cobra.Command {
	var imageURL, imageDigest, outputDir, resultFile, imagesProcessedFile string

	cmd := &cobra.Command{
		Use:   "validate",
		Short: "Validate a file-based catalog image",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return fbcvalidation.Validate(cmd.ErrOrStderr())
		},
	}

	cmd.Flags().StringVar(&imageURL, "image-url", "", "Reference to the FBC image (required)")
	cmd.Flags().StringVar(&imageDigest, "image-digest", "", "Digest of the FBC image (required)")
	cmd.Flags().StringVar(&outputDir, "output-dir", "", "Directory for catalog.json and related-images.json (required)")
	cmd.Flags().StringVar(&resultFile, "result-file", "", "Path for the TEST_OUTPUT result (default: stdout)")
	cmd.Flags().StringVar(&imagesProcessedFile, "images-processed-file", "", "Path for the IMAGES_PROCESSED result (required)")

	for _, flag := range []string{"image-url", "image-digest", "output-dir", "images-processed-file"} {
		if err := cmd.MarkFlagRequired(flag); err != nil {
			panic(err)
		}
	}

	return cmd
}
