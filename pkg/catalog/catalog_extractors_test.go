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
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// Only olm.package names are returned, in input order, from a mixed catalog.
// 1. Read the fixture containing packages, channels, bundles and custom metadata.
// 2. Extract package names from the rendered JSON stream.
// 3. Verify zeta precedes alpha and names from other schemas are excluded.
func TestExtractUniquePackageNamesFromCatalog_Mixed(t *testing.T) {
	got, err := ExtractUniquePackageNamesFromCatalog(strings.NewReader(extractorFixture(t, "mixed.json")))
	assertExtractedStrings(t, got, err, []string{"zeta", "alpha"})
}

// A bundle's name and package are not package records; no match returns a non-nil empty slice.
func TestExtractUniquePackageNamesFromCatalog_NoPackages(t *testing.T) {
	got, err := ExtractUniquePackageNamesFromCatalog(strings.NewReader(alphaBundle))
	assertExtractedStrings(t, got, err, []string{})
}

// Duplicate olm.package records are collapsed while preserving first-seen order.
func TestExtractUniquePackageNamesFromCatalog_DeduplicatesPackages(t *testing.T) {
	input := zetaPackage + `{
    "schema": "olm.package",
    "name": "alpha",
    "defaultChannel": "stable"
}
` + zetaPackage
	got, err := ExtractUniquePackageNamesFromCatalog(strings.NewReader(input))
	assertExtractedStrings(t, got, err, []string{"zeta", "alpha"})
}

// A read failure after a matching package must discard the name already extracted.
// 1. Supply a complete alpha package followed by a reader returning a known error.
// 2. Extract package names.
// 3. Verify a nil result and errors.Is preserves the original failure.
func TestExtractUniquePackageNamesFromCatalog_ReadError(t *testing.T) {
	wantErr := errors.New("catalog read failed")
	r := io.MultiReader(strings.NewReader(`{
    "schema": "olm.package",
    "name": "alpha",
    "defaultChannel": "stable"
}

`), failingCatalogReader{wantErr})
	got, err := ExtractUniquePackageNamesFromCatalog(r)
	if got != nil || !errors.Is(err, wantErr) {
		t.Fatalf("extract = (%q, %v), want nil and %v", got, err, wantErr)
	}
}

// Catalog metadata is collected in one pass while preserving the focused
// extractors' ordering and deduplication behavior.
func TestExtractCatalogMetadata_Mixed(t *testing.T) {
	got, err := ExtractCatalogMetadata(strings.NewReader(extractorFixture(t, "mixed.json")))
	if err != nil {
		t.Fatalf("extract metadata: %v", err)
	}
	assertStringSlice(t, got.PackageNames, []string{"zeta", "alpha"})
	assertStringSlice(t, got.BundleImagesByPackage["zeta"], []string{
		"registry.example/zeta-bundle:1.0.0",
		"registry.example/another-zeta-bundle:2.0.0",
	})
	assertStringSlice(t, got.BundleImagesByPackage["alpha"], []string{
		"registry.example/alpha-bundle:1.0.0",
	})
	assertStringSlice(t, got.RelatedImagesByPackage["zeta"], []string{
		"registry.example/zeta-helper:1.0.0",
		"registry.example/zeta-operator:1.0.0",
	})
	assertStringSlice(t, got.RelatedImagesByPackage["alpha"], []string{
		"registry.example/alpha-operator:1.0.0",
	})
}

// An active bundle without relatedImages still creates an empty related image
// set for its package.
func TestExtractCatalogMetadata_MissingRelatedImages(t *testing.T) {
	got, err := ExtractCatalogMetadata(strings.NewReader(extractorFixture(t, "no-related-images.json")))
	if err != nil {
		t.Fatalf("extract metadata: %v", err)
	}
	assertStringSlice(t, got.PackageNames, []string{"zeta"})
	assertStringSlice(t, got.BundleImagesByPackage["zeta"], []string{"registry.example/zeta-bundle:1.0.0"})
	assertStringSlice(t, got.RelatedImagesByPackage["zeta"], []string{})
}

// A bundle seen before its olm.package record still makes the package visible
// through PackageNames so callers can iterate PackageNames and read the maps.
func TestExtractCatalogMetadata_BundleBeforePackage(t *testing.T) {
	input := alphaBundle + `{
    "schema": "olm.package",
    "name": "alpha",
    "defaultChannel": "stable"
}
`
	got, err := ExtractCatalogMetadata(strings.NewReader(input))
	if err != nil {
		t.Fatalf("extract metadata: %v", err)
	}
	assertStringSlice(t, got.PackageNames, []string{"alpha"})
	assertStringSlice(t, got.BundleImagesByPackage["alpha"], []string{"registry.example/alpha-bundle:1.0.0"})
	assertStringSlice(t, got.RelatedImagesByPackage["alpha"], []string{"registry.example/alpha-operator:1.0.0"})
}

// A package whose bundles are all deprecated still has empty image sets.
func TestExtractCatalogMetadata_AllDeprecatedBundles(t *testing.T) {
	input := zetaPackage + deprecatedZetaBundle
	got, err := ExtractCatalogMetadata(strings.NewReader(input))
	if err != nil {
		t.Fatalf("extract metadata: %v", err)
	}
	assertStringSlice(t, got.PackageNames, []string{"zeta"})
	assertStringSlice(t, got.BundleImagesByPackage["zeta"], []string{})
	assertStringSlice(t, got.RelatedImagesByPackage["zeta"], []string{})
}

// A reader failure after valid records must not return partial metadata.
func TestExtractCatalogMetadata_ReadErrorDiscardsPartialResults(t *testing.T) {
	wantErr := errors.New("catalog read failed")
	r := io.MultiReader(strings.NewReader(alphaBundle), failingCatalogReader{wantErr})
	got, err := ExtractCatalogMetadata(r)
	if got.PackageNames != nil || got.BundleImagesByPackage != nil || got.RelatedImagesByPackage != nil || !errors.Is(err, wantErr) {
		t.Fatalf("extract metadata = (%#v, %v), want zero metadata and %v", got, err, wantErr)
	}
}

// Active zeta bundles retain first-seen input order and deduplicate image references.
// 1. Read the mixed fixture containing other schemas, another package and a deprecated bundle.
// 2. Extract bundles for zeta.
// 3. Verify only the two unique active images remain, without sorting.
func TestExtractUniqueBundlesFromCatalog_Mixed(t *testing.T) {
	got, err := ExtractUniqueBundlesFromCatalog(strings.NewReader(extractorFixture(t, "mixed.json")), "zeta")
	assertExtractedStrings(t, got, err, []string{
		"registry.example/zeta-bundle:1.0.0",
		"registry.example/another-zeta-bundle:2.0.0",
	})
}

// Package matching is exact: selecting foo must not include the foobar bundle.
func TestExtractUniqueBundlesFromCatalog_ExactPackage(t *testing.T) {
	input := strings.ReplaceAll(alphaBundle, "alpha", "foo") + strings.ReplaceAll(alphaBundle, "alpha", "foobar")
	got, err := ExtractUniqueBundlesFromCatalog(strings.NewReader(input), "foo")
	assertExtractedStrings(t, got, err, []string{"registry.example/foo-bundle:1.0.0"})
}

// An active alpha bundle does not match zeta; no match is a successful non-nil empty slice.
func TestExtractUniqueBundlesFromCatalog_MissingPackage(t *testing.T) {
	got, err := ExtractUniqueBundlesFromCatalog(strings.NewReader(alphaBundle), "zeta")
	assertExtractedStrings(t, got, err, []string{})
}

// Two deprecated zeta bundles contribute no images, yielding a non-nil empty slice.
func TestExtractUniqueBundlesFromCatalog_AllDeprecated(t *testing.T) {
	input := deprecatedZetaBundle + strings.ReplaceAll(deprecatedZetaBundle, "1.0.0", "2.0.0")
	got, err := ExtractUniqueBundlesFromCatalog(strings.NewReader(input), "zeta")
	assertExtractedStrings(t, got, err, []string{})
}

// Deprecation between olm.package and olm.gvk still excludes the bundle.
func TestExtractUniqueBundlesFromCatalog_DeprecatedInMiddle(t *testing.T) {
	input := strings.Replace(deprecatedZetaBundle,
		`{"type": "olm.deprecated", "value": {}}`,
		`{"type": "olm.deprecated", "value": {}},
        {
            "type": "olm.gvk",
            "value": {"group": "example.com", "version": "v1", "kind": "Zeta"}
        }`, 1)
	got, err := ExtractUniqueBundlesFromCatalog(strings.NewReader(input), "zeta")
	assertExtractedStrings(t, got, err, []string{})
}

// Empty packageName is an API error even when the input contains a valid active bundle.
func TestExtractUniqueBundlesFromCatalog_EmptyPackageName(t *testing.T) {
	got, err := ExtractUniqueBundlesFromCatalog(strings.NewReader(alphaBundle), "")
	if got != nil || err == nil {
		t.Fatalf("extract = (%q, %v), want nil and an error", got, err)
	}
}

// A reader failure after a matching bundle must discard its already extracted image.
// 1. Supply an active alpha bundle followed by a known reader error.
// 2. Extract bundle images for alpha.
// 3. Verify nil and an error wrapping the original failure.
func TestExtractUniqueBundlesFromCatalog_ReadError(t *testing.T) {
	wantErr := errors.New("catalog read failed")
	r := io.MultiReader(strings.NewReader(alphaBundle), failingCatalogReader{wantErr})
	got, err := ExtractUniqueBundlesFromCatalog(r, "alpha")
	if got != nil || !errors.Is(err, wantErr) {
		t.Fatalf("extract = (%q, %v), want nil and %v", got, err, wantErr)
	}
}

// Related images are unique by image string and sorted, regardless of their descriptive names.
// 1. Read the mixed fixture with duplicates within/across bundles and distinct bundle images.
// 2. Extract related images for zeta and verify exactly the two sorted active references.
// 3. Reuse the two same-name bundles alone so other names cannot mask loss of an image.
func TestExtractUniqueRelatedImagesFromCatalog_Mixed(t *testing.T) {
	input := extractorFixture(t, "mixed.json")
	want := []string{
		"registry.example/zeta-helper:1.0.0",
		"registry.example/zeta-operator:1.0.0",
	}
	got, err := ExtractUniqueRelatedImagesFromCatalog(strings.NewReader(input), "zeta")
	assertExtractedStrings(t, got, err, want)

	// These existing bundles both use name "operator" for different images.
	// Checking this fragment prevents the other fixture entries from hiding name-based deduplication.
	fragment := fixtureRecordsNamed(t, input, "zeta.v2.0.0", "zeta.v3.0.0")
	got, err = ExtractUniqueRelatedImagesFromCatalog(strings.NewReader(fragment), "zeta")
	assertExtractedStrings(t, got, err, want)
}

// A complete catalog whose only active bundle omits relatedImages yields a non-nil empty slice.
func TestExtractUniqueRelatedImagesFromCatalog_MissingRelatedImages(t *testing.T) {
	input := extractorFixture(t, "no-related-images.json")
	got, err := ExtractUniqueRelatedImagesFromCatalog(strings.NewReader(input), "zeta")
	assertExtractedStrings(t, got, err, []string{})
}

// Alpha's related image must not appear when zeta is requested; no match is a successful empty slice.
func TestExtractUniqueRelatedImagesFromCatalog_MissingPackage(t *testing.T) {
	got, err := ExtractUniqueRelatedImagesFromCatalog(strings.NewReader(alphaBundle), "zeta")
	assertExtractedStrings(t, got, err, []string{})
}

// Empty packageName is rejected with nil even when a valid zeta bundle has related images.
func TestExtractUniqueRelatedImagesFromCatalog_EmptyPackageName(t *testing.T) {
	input := strings.ReplaceAll(alphaBundle, "alpha", "zeta")
	got, err := ExtractUniqueRelatedImagesFromCatalog(strings.NewReader(input), "")
	if got != nil || err == nil {
		t.Fatalf("extract = (%q, %v), want nil and an error", got, err)
	}
}

// A read error after a matching bundle must discard all related images already extracted.
// 1. Supply an active alpha bundle with a related image followed by a known reader error.
// 2. Extract related images for alpha.
// 3. Verify nil and errors.Is matches the original failure.
func TestExtractUniqueRelatedImagesFromCatalog_ReadError(t *testing.T) {
	wantErr := errors.New("catalog read failed")
	r := io.MultiReader(strings.NewReader(alphaBundle), failingCatalogReader{wantErr})
	got, err := ExtractUniqueRelatedImagesFromCatalog(r, "alpha")
	if got != nil || !errors.Is(err, wantErr) {
		t.Fatalf("extract = (%q, %v), want nil and %v", got, err, wantErr)
	}
}

// Missing properties on a matching bundle fails extraction and discards earlier results.
// 1. Supply a valid alpha bundle followed by a matching bundle without properties.
// 2. Run both the bundle and related image extractors for alpha.
// 3. Verify nil results and a properties error identifying the second record.
func TestExtractCatalog_MissingBundleProperties(t *testing.T) {
	input := alphaBundle + invalidAlphaBundleMissingProperties
	for _, tc := range []struct {
		name    string
		extract func(io.Reader, string) ([]string, error)
	}{
		{"bundles", ExtractUniqueBundlesFromCatalog},
		{"related images", ExtractUniqueRelatedImagesFromCatalog},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := tc.extract(strings.NewReader(input), "alpha")
			if got != nil || err == nil {
				t.Fatalf("extract = (%q, %v), want nil and an error", got, err)
			}
			if !strings.Contains(err.Error(), "catalog record 2: properties must be present") {
				t.Fatalf("error = %q, want missing properties failure in record 2", err)
			}
		})
	}
}

// Missing properties on a bundle fails metadata extraction without returning
// data collected from earlier valid records.
func TestExtractCatalogMetadata_MissingBundlePropertiesDiscardsPartialResults(t *testing.T) {
	input := alphaBundle + invalidAlphaBundleMissingProperties
	got, err := ExtractCatalogMetadata(strings.NewReader(input))
	if got.PackageNames != nil || got.BundleImagesByPackage != nil || got.RelatedImagesByPackage != nil || err == nil {
		t.Fatalf("extract metadata = (%#v, %v), want zero metadata and an error", got, err)
	}
	if !strings.Contains(err.Error(), "catalog record 2: properties must be present") {
		t.Fatalf("metadata error = %q, want missing properties failure in record 2", err)
	}
}

// An olm.deprecations record never excludes a bundle; only an olm.deprecated property does.
// 1. Load the fixture referencing v1.0.0 in a record and marking v2.0.0 with a property.
// 2. Run all three extractors with the deprecations record before and after the bundles.
// 3. Verify zeta and only v1.0.0's bundle/related image remain in either order.
func TestExtractCatalog_DeprecationsRecord(t *testing.T) {
	input := extractorFixture(t, "deprecations.json")
	records := decodeFixtureRecords(t, input)
	if len(records) != 5 {
		t.Fatalf("deprecations fixture has %d records, want 5", len(records))
	}
	for _, tc := range []struct{ name, input string }{
		// A preceding deprecations record must not suppress the referenced bundle.
		{"before bundles", input},
		// A following record must not retroactively remove the referenced bundle.
		{"after bundles", strings.Join([]string{records[0], records[1], records[3], records[4], records[2]}, "\n")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ExtractUniquePackageNamesFromCatalog(strings.NewReader(tc.input))
			assertExtractedStrings(t, got, err, []string{"zeta"})
			got, err = ExtractUniqueBundlesFromCatalog(strings.NewReader(tc.input), "zeta")
			assertExtractedStrings(t, got, err, []string{"registry.example/zeta-bundle:1.0.0"})
			got, err = ExtractUniqueRelatedImagesFromCatalog(strings.NewReader(tc.input), "zeta")
			assertExtractedStrings(t, got, err, []string{"registry.example/zeta-operator:1.0.0"})
			metadata, err := ExtractCatalogMetadata(strings.NewReader(tc.input))
			if err != nil {
				t.Fatalf("extract metadata: %v", err)
			}
			assertStringSlice(t, metadata.PackageNames, []string{"zeta"})
			assertStringSlice(t, metadata.BundleImagesByPackage["zeta"], []string{"registry.example/zeta-bundle:1.0.0"})
			assertStringSlice(t, metadata.RelatedImagesByPackage["zeta"], []string{"registry.example/zeta-operator:1.0.0"})
		})
	}
}

// fixtureRecordsNamed reuses records without creating a second fixture or relying on the parser under test.
func fixtureRecordsNamed(t *testing.T, input string, names ...string) string {
	t.Helper()
	var output strings.Builder
	count := 0
	for _, raw := range decodeFixtureRecords(t, input) {
		var record struct{ Name string }
		if err := json.Unmarshal([]byte(raw), &record); err != nil {
			t.Fatal(err)
		}
		if slices.Contains(names, record.Name) {
			output.WriteString(raw)
			output.WriteByte('\n')
			count++
		}
	}
	if count != len(names) {
		t.Fatalf("fixture has %d requested records, want %d", count, len(names))
	}
	return output.String()
}

// decodeFixtureRecords decodes concatenated JSON records from input in fixture order.
func decodeFixtureRecords(t *testing.T, input string) []string {
	t.Helper()
	decoder := json.NewDecoder(strings.NewReader(input))
	var records []string
	for {
		var raw json.RawMessage
		if err := decoder.Decode(&raw); err != nil {
			if errors.Is(err, io.EOF) {
				return records
			}
			t.Fatal(err)
		}
		records = append(records, string(raw))
	}
}

const zetaPackage = `{
    "schema": "olm.package",
    "name": "zeta",
    "defaultChannel": "stable"
}
`

const deprecatedZetaBundle = `{
    "schema": "olm.bundle",
    "name": "zeta.v1.0.0",
    "package": "zeta",
    "image": "registry.example/zeta-bundle:1.0.0",
    "properties": [
        {
            "type": "olm.package",
            "value": {"packageName": "zeta", "version": "1.0.0"}
        },
        {"type": "olm.deprecated", "value": {}}
    ]
}
`

const alphaBundle = `{
    "schema": "olm.bundle",
    "name": "alpha.v1.0.0",
    "package": "alpha",
    "image": "registry.example/alpha-bundle:1.0.0",
    "properties": [
        {
            "type": "olm.package",
            "value": {"packageName": "alpha", "version": "1.0.0"}
        }
    ],
    "relatedImages": [
        {"name": "operator", "image": "registry.example/alpha-operator:1.0.0"}
    ]
}
`

// invalidAlphaBundleMissingProperties omits required bundle properties.
const invalidAlphaBundleMissingProperties = `{
    "schema": "olm.bundle",
    "name": "alpha.v2.0.0",
    "package": "alpha",
    "image": "registry.example/alpha-bundle:2.0.0",
    "relatedImages": [
        {"name": "operator", "image": "registry.example/alpha-operator:2.0.0"}
    ]
}`

type failingCatalogReader struct{ err error }

func (r failingCatalogReader) Read([]byte) (int, error) { return 0, r.err }

func extractorFixture(t *testing.T, name string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", "extractors", name))
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func assertExtractedStrings(t *testing.T, got []string, err error, want []string) {
	t.Helper()
	if err != nil {
		t.Fatalf("extract: %v", err)
	}
	assertStringSlice(t, got, want)
}

func assertStringSlice(t *testing.T, got, want []string) {
	t.Helper()
	if got == nil || !slices.Equal(got, want) {
		t.Fatalf("extracted = %#v, want non-nil %q", got, want)
	}
}
