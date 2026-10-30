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

package catalog

import (
	"context"
	"fmt"

	"github.com/konflux-ci/operator-foundry/pkg/image"
)

// OCPVersionResolver returns the target OpenShift versions for an FBC fragment image.
type OCPVersionResolver func(context.Context, string) ([]string, error)

// GetTargetFBCCatalogImages returns the catalog images matching an FBC fragment.
// Images are returned in resolver order.
func GetTargetFBCCatalogImages(
	ctx context.Context,
	fbcFragment string,
	indexImage string,
	resolver OCPVersionResolver,
) ([]string, error) {
	if fbcFragment == "" {
		return nil, ErrEmptyFBCFragment
	}
	if indexImage == "" {
		return nil, ErrEmptyIndexImage
	}
	if resolver == nil {
		return nil, ErrNilOCPVersionResolver
	}

	parsed, err := image.ParseImageURL(indexImage)
	if err != nil {
		return nil, fmt.Errorf("failed to parse index image: %w", err)
	}
	ocpVersions, err := resolver(ctx, fbcFragment)
	if err != nil {
		return nil, fmt.Errorf("failed to get OCP versions for FBC fragment: %w", err)
	}
	if len(ocpVersions) == 0 {
		return nil, ErrEmptyOCPVersions
	}

	targets := []string{}
	for _, ocpVersion := range ocpVersions {
		target := parsed.RegistryRepository + ":" + ocpVersion
		if _, err := image.ParseImageURL(target); err != nil {
			return nil, fmt.Errorf("failed to build target catalog image for OCP version %q: %w", ocpVersion, err)
		}
		targets = append(targets, target)
	}
	return targets, nil
}
