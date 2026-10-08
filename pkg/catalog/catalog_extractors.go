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
	"encoding/json"
	"fmt"
	"io"
	"slices"
)

// ExtractUniquePackageNamesFromCatalog reads rendered catalog JSON and returns
// olm.package names in input order. A valid FBC catalog defines each package once.
// It reads r from its current position to EOF without closing it. No matches
// returns a non-nil empty slice; any read or extraction error returns nil.
func ExtractUniquePackageNamesFromCatalog(r io.Reader) ([]string, error) {
	names := []string{}
	err := readCatalog(r, func(record catalogRecord) error {
		if !record.matches("schema", "olm.package") {
			return nil
		}
		name, err := record.nonEmptyString("name")
		if err != nil {
			return err
		}
		names = append(names, name)
		return nil
	})
	if err != nil {
		return nil, err
	}
	return names, nil
}

// ExtractUniqueBundlesFromCatalog returns active bundle images for packageName
// in input order, preserving duplicate image references as in the Bash helper.
// Only an olm.deprecated property excludes a bundle; olm.deprecations records
// are ignored. It reads r to EOF without closing it. No matches returns a
// non-nil empty slice. Empty packageName or any read/extraction error returns nil.
func ExtractUniqueBundlesFromCatalog(r io.Reader, packageName string) ([]string, error) {
	images := []string{}
	err := readActiveBundles(r, packageName, func(record catalogRecord) error {
		image, err := record.nonEmptyString("image")
		if err != nil {
			return err
		}
		images = append(images, image)
		return nil
	})
	if err != nil {
		return nil, err
	}
	return images, nil
}

// ExtractRelatedImagesFromCatalog returns sorted, unique related image references
// from active bundles of packageName. The bundle image itself is not added.
// It reads r to EOF without closing it. No matches returns a non-nil empty slice.
// Empty packageName or any read/extraction error returns nil, never partial results.
func ExtractRelatedImagesFromCatalog(r io.Reader, packageName string) ([]string, error) {
	images := []string{}
	err := readActiveBundles(r, packageName, func(record catalogRecord) error {
		if _, present := record["relatedImages"]; !present {
			return nil
		}
		var related []catalogRecord
		if err := json.Unmarshal(record["relatedImages"], &related); err != nil {
			return fmt.Errorf("decode relatedImages: %w", err)
		}
		for _, item := range related {
			image, err := item.nonEmptyString("image")
			if err != nil {
				return err
			}
			images = append(images, image)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	slices.Sort(images)
	return slices.Compact(images), nil
}

// readActiveBundles shares schema, exact package and property-based deprecation
// filtering, before either extractor inspects the fields it needs.
func readActiveBundles(r io.Reader, packageName string, consume func(catalogRecord) error) error {
	if packageName == "" {
		return ErrEmptyPackageName
	}
	return readCatalog(r, func(record catalogRecord) error {
		if !record.matches("schema", "olm.bundle") || !record.matches("package", packageName) {
			return nil
		}
		var properties []struct {
			Type string `json:"type"`
		}
		if err := json.Unmarshal(record["properties"], &properties); err != nil {
			return fmt.Errorf("decode properties: %w", err)
		}
		if properties == nil {
			return fmt.Errorf("properties must be an array")
		}
		for _, property := range properties {
			if property.Type == "olm.deprecated" {
				return nil
			}
		}

		return consume(record)
	})
}
