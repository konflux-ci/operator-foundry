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
	"errors"
	"fmt"
	"io"
	"slices"
)

// CatalogMetadata contains package-level catalog metadata collected in a
// single pass over a rendered catalog.
type CatalogMetadata struct {
	PackageNames           []string
	BundleImagesByPackage  map[string][]string
	RelatedImagesByPackage map[string][]string
}

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

// ExtractCatalogMetadata reads rendered catalog JSON once and returns package
// names, active bundle image references and active related image references
// grouped by package. Package names are first-seen and retain input order
// whether first found in an olm.package or active olm.bundle record. Bundle
// images are unique per package. Related images are sorted and unique per
// package.
func ExtractCatalogMetadata(r io.Reader) (CatalogMetadata, error) {
	metadata := CatalogMetadata{
		PackageNames:           []string{},
		BundleImagesByPackage:  map[string][]string{},
		RelatedImagesByPackage: map[string][]string{},
	}

	ensurePackage := func(name string) {
		// Keep PackageNames and the per-package maps in sync even if an active
		// bundle appears before its olm.package record.
		metadata.PackageNames = appendUniqueString(metadata.PackageNames, name)
		if metadata.BundleImagesByPackage[name] == nil {
			metadata.BundleImagesByPackage[name] = []string{}
		}
		if metadata.RelatedImagesByPackage[name] == nil {
			metadata.RelatedImagesByPackage[name] = []string{}
		}
	}

	err := readCatalog(r, func(record catalogRecord) error {
		switch {
		case record.matches("schema", "olm.package"):
			name, err := record.nonEmptyString("name")
			if err != nil {
				return err
			}
			ensurePackage(name)

		case record.matches("schema", "olm.bundle"):
			bundle, deprecated, err := decodeBundleMetadata(record)
			if err != nil {
				return err
			}
			if deprecated {
				return nil
			}
			ensurePackage(bundle.Package)
			metadata.BundleImagesByPackage[bundle.Package] =
				appendUniqueString(metadata.BundleImagesByPackage[bundle.Package], bundle.Image)
			metadata.RelatedImagesByPackage[bundle.Package] =
				append(metadata.RelatedImagesByPackage[bundle.Package], bundle.RelatedImages...)
		}
		return nil
	})
	if err != nil {
		return CatalogMetadata{}, err
	}

	sortAndCompactStringSlices(metadata.RelatedImagesByPackage)
	return metadata, nil
}

// ExtractUniqueBundlesFromCatalog returns unique active bundle images for packageName
// in first-seen input order.
// Only an olm.deprecated property excludes a bundle; olm.deprecations records
// are ignored. It reads r to EOF without closing it. No matches returns a
// non-nil empty slice. Empty packageName or any read/extraction error returns nil.
func ExtractUniqueBundlesFromCatalog(r io.Reader, packageName string) ([]string, error) {
	images := []string{}
	seen := map[string]struct{}{}
	err := readActiveBundles(r, packageName, func(record catalogRecord) error {
		image, err := record.nonEmptyString("image")
		if err != nil {
			return err
		}
		if _, ok := seen[image]; ok {
			return nil
		}
		seen[image] = struct{}{}
		images = append(images, image)
		return nil
	})
	if err != nil {
		return nil, err
	}
	return images, nil
}

// ExtractUniqueRelatedImagesFromCatalog returns sorted, unique related image
// references from active bundles of packageName. The bundle image itself is not
// added. It reads r to EOF without closing it. No matches returns a non-nil
// empty slice. Empty packageName or any read/extraction error returns nil,
// never partial results.
func ExtractUniqueRelatedImagesFromCatalog(r io.Reader, packageName string) ([]string, error) {
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
		deprecated, err := bundleDeprecated(record)
		if err != nil {
			return err
		}
		if deprecated {
			return nil
		}

		return consume(record)
	})
}

// catalogBundleMetadata holds the bundle fields used by catalog extractors.
type catalogBundleMetadata struct {
	Package       string
	Image         string
	RelatedImages []string
}

// decodeBundleMetadata extracts the package, bundle image and related images
// from an active bundle record.
func decodeBundleMetadata(record catalogRecord) (catalogBundleMetadata, bool, error) {
	deprecated, err := bundleDeprecated(record)
	if err != nil {
		return catalogBundleMetadata{}, false, err
	}
	if deprecated {
		return catalogBundleMetadata{}, true, nil
	}

	packageName, err := record.nonEmptyString("package")
	if err != nil {
		return catalogBundleMetadata{}, false, err
	}
	image, err := record.nonEmptyString("image")
	if err != nil {
		return catalogBundleMetadata{}, false, err
	}

	bundle := catalogBundleMetadata{
		Package: packageName,
		Image:   image,
	}
	if rawRelatedImages, ok := record["relatedImages"]; ok {
		var related []catalogRecord
		if err := json.Unmarshal(rawRelatedImages, &related); err != nil {
			return catalogBundleMetadata{}, false, fmt.Errorf("decode relatedImages: %w", err)
		}
		for _, item := range related {
			image, err := item.nonEmptyString("image")
			if err != nil {
				return catalogBundleMetadata{}, false, err
			}
			bundle.RelatedImages = append(bundle.RelatedImages, image)
		}
	}

	return bundle, false, nil
}

// bundleDeprecated reports whether a bundle record has an olm.deprecated
// property.
func bundleDeprecated(record catalogRecord) (bool, error) {
	var properties []struct {
		Type string `json:"type"`
	}
	rawProperties, ok := record["properties"]
	if !ok {
		return false, errors.New("properties must be present")
	}
	if err := json.Unmarshal(rawProperties, &properties); err != nil {
		return false, fmt.Errorf("decode properties: %w", err)
	}
	if properties == nil {
		return false, errors.New("properties must be an array")
	}
	for _, property := range properties {
		if property.Type == "olm.deprecated" {
			return true, nil
		}
	}
	return false, nil
}

// appendUniqueString appends value only if it is not already present.
func appendUniqueString(values []string, value string) []string {
	if slices.Contains(values, value) {
		return values
	}
	return append(values, value)
}

// sortAndCompactStringSlices sorts and deduplicates each map value in place.
func sortAndCompactStringSlices(valuesByKey map[string][]string) {
	for key, values := range valuesByKey {
		slices.Sort(values)
		valuesByKey[key] = slices.Compact(values)
	}
}
