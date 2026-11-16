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
	"errors"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/konflux-ci/operator-foundry/pkg/image"
)

const differentialIndexDigest = "sha256:abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789"

// A tagged index image is rendered exactly as provided and does not require
// OCP target resolution.
// 1. Create an FBC fragment and tagged comparison index catalog.
// 2. Extract unreleased related images.
// 3. Verify the FBC-only image is returned and the tagged index is rendered as-is.
func TestGetUnreleasedFBCRelatedImages_UsesTaggedIndexAsIs(t *testing.T) {
	const fbcFragment = "registry.example/fbc:latest"
	const taggedIndex = "registry.example/index:v4.18"
	const newRelatedImage = "registry.example/new:1"

	// Given an FBC fragment with one new related image and an explicitly
	// tagged comparison index.
	renderer := newFakeDifferentialCatalogRenderer(t, map[string]string{
		fbcFragment: zetaDifferentialCatalog(newRelatedImage),
		taggedIndex: zetaDifferentialCatalog(),
	})
	opts := UnreleasedRelatedImagesOptions{
		FBCFragment: fbcFragment,
		IndexImage:  taggedIndex,
	}

	// When unreleased related images are extracted.
	got, err := getUnreleasedFBCRelatedImages(t.Context(), opts, ocpResolverMustNotBeCalled(t), renderer.render)
	if err != nil {
		t.Fatalf("unreleased related images: %v", err)
	}

	// Then only the image absent from the index is returned.
	assertStringSlice(t, got, []string{newRelatedImage})

	// And the tagged index is rendered as-is without OCP target resolution.
	renderer.assertRenderedTargets(t, []string{fbcFragment, taggedIndex})
}

// A digest-pinned index image is rendered exactly as provided and does not
// require OCP target resolution.
// 1. Create an FBC fragment and digest-pinned comparison index catalog.
// 2. Extract unreleased related images.
// 3. Verify the FBC-only image is returned and the digest index is rendered as-is.
func TestGetUnreleasedFBCRelatedImages_UsesDigestIndexAsIs(t *testing.T) {
	const fbcFragment = "registry.example/fbc:latest"
	const newRelatedImage = "registry.example/new:1"
	digestIndex := "registry.example/index@" + differentialIndexDigest

	// Given an FBC fragment with one new related image and an explicitly
	// digest-pinned comparison index.
	renderer := newFakeDifferentialCatalogRenderer(t, map[string]string{
		fbcFragment: zetaDifferentialCatalog(newRelatedImage),
		digestIndex: zetaDifferentialCatalog(),
	})
	opts := UnreleasedRelatedImagesOptions{
		FBCFragment: fbcFragment,
		IndexImage:  digestIndex,
	}

	// When unreleased related images are extracted.
	got, err := getUnreleasedFBCRelatedImages(t.Context(), opts, ocpResolverMustNotBeCalled(t), renderer.render)
	if err != nil {
		t.Fatalf("unreleased related images: %v", err)
	}

	// Then the digest index is rendered as-is and the FBC-only image is returned.
	assertStringSlice(t, got, []string{newRelatedImage})
	renderer.assertRenderedTargets(t, []string{fbcFragment, digestIndex})
}

// When target resolution returns several index images, only the first one is
// used so validate-fbc keeps its existing single-index comparison contract.
// 1. Create an untagged index that resolves to multiple target catalogs.
// 2. Extract unreleased related images.
// 3. Verify only the first resolved index is rendered and compared.
func TestGetUnreleasedFBCRelatedImages_UsesFirstResolvedIndex(t *testing.T) {
	const fbcFragment = "registry.example/fbc:latest"
	const untaggedIndex = "registry.example/index"
	const firstResolvedIndex = "registry.example/index:v4.18"
	const secondResolvedIndex = "registry.example/index:v4.19"
	const imageAlreadyPresentInFirstResolvedIndex = "registry.example/already-in-first-index:1"
	const newRelatedImage = "registry.example/new:1"

	// Given an untagged index image that resolves to two target indexes.
	// The second index contains newRelatedImage so the result would be empty if it participated.
	renderer := newFakeDifferentialCatalogRenderer(t, map[string]string{
		fbcFragment:         zetaDifferentialCatalog(imageAlreadyPresentInFirstResolvedIndex, newRelatedImage),
		firstResolvedIndex:  zetaDifferentialCatalog(imageAlreadyPresentInFirstResolvedIndex),
		secondResolvedIndex: zetaDifferentialCatalog(newRelatedImage),
	})
	opts := UnreleasedRelatedImagesOptions{
		FBCFragment: fbcFragment,
		IndexImage:  untaggedIndex,
	}
	resolver := differentialOCPVersions("v4.18", "v4.19")

	// When unreleased related images are extracted.
	got, err := getUnreleasedFBCRelatedImages(t.Context(), opts, resolver, renderer.render)
	if err != nil {
		t.Fatalf("unreleased related images: %v", err)
	}

	// Then only the first resolved index participates in comparison.
	assertStringSlice(t, got, []string{newRelatedImage})

	// And the second resolved index is not rendered.
	renderer.assertRenderedTargets(t, []string{fbcFragment, firstResolvedIndex})
}

// Related images already present in the index are excluded from the result.
// 1. Create FBC fragment and index catalogs with the same related image.
// 2. Extract unreleased related images.
// 3. Verify the released image is omitted from the result.
func TestGetUnreleasedFBCRelatedImages_ExcludesReleasedImages(t *testing.T) {
	const fbcFragment = "registry.example/fbc:latest"
	const index = "registry.example/index:v4.18"
	const imageAlreadyPresentInIndex = "registry.example/already-present-in-index:1"

	// Given the FBC fragment and index both reference the same related image.
	renderer := newFakeDifferentialCatalogRenderer(t, map[string]string{
		fbcFragment: zetaDifferentialCatalog(imageAlreadyPresentInIndex),
		index:       zetaDifferentialCatalog(imageAlreadyPresentInIndex),
	})
	opts := UnreleasedRelatedImagesOptions{
		FBCFragment: fbcFragment,
		IndexImage:  index,
	}

	// When unreleased related images are extracted.
	got, err := getUnreleasedFBCRelatedImages(t.Context(), opts, nil, renderer.render)
	if err != nil {
		t.Fatalf("unreleased related images: %v", err)
	}

	// Then the already released image is excluded.
	assertStringSlice(t, got, []string{})
}

// Comparison is scoped to packages declared by olm.package records in the FBC
// fragment; bundle-only packages in the fragment are ignored.
// 1. Create an FBC fragment with one declared package and one bundle-only package.
// 2. Extract unreleased related images.
// 3. Verify only related images from the declared package are returned.
func TestGetUnreleasedFBCRelatedImages_UsesFragmentPackageRecordsAsScope(t *testing.T) {
	const fbcFragment = "registry.example/fbc:latest"
	const index = "registry.example/index:v4.18"
	const zetaNewImage = "registry.example/zeta-new:1"

	// Given zeta is declared by an olm.package record, while beta only appears
	// as a bundle package in the FBC fragment.
	renderer := newFakeDifferentialCatalogRenderer(t, map[string]string{
		fbcFragment: `
{"schema":"olm.package","name":"zeta"}
{"schema":"olm.bundle","package":"zeta","image":"registry.example/zeta-bundle:1","properties":[],"relatedImages":[
  {"image":"registry.example/zeta-new:1"}
]}
{"schema":"olm.bundle","package":"beta","image":"registry.example/beta-bundle:1","properties":[],"relatedImages":[
  {"image":"registry.example/ignored-beta:1"}
]}
`,
		index: zetaDifferentialCatalog(),
	})
	opts := UnreleasedRelatedImagesOptions{
		FBCFragment: fbcFragment,
		IndexImage:  index,
	}

	// When unreleased related images are extracted.
	got, err := getUnreleasedFBCRelatedImages(t.Context(), opts, nil, renderer.render)
	if err != nil {
		t.Fatalf("unreleased related images: %v", err)
	}

	// Then only zeta's related image is returned; beta's bundle-only image is ignored.
	assertStringSlice(t, got, []string{zetaNewImage})
}

// Index comparison ignores malformed bundles outside the FBC fragment's
// declared package scope.
// 1. Create a zeta FBC fragment and an index with a malformed alpha bundle.
// 2. Extract unreleased related images.
// 3. Verify comparison succeeds and ignores the unscoped alpha bundle.
func TestGetUnreleasedFBCRelatedImages_IgnoresMalformedUnscopedIndexBundles(t *testing.T) {
	const fbcFragment = "registry.example/fbc:latest"
	const index = "registry.example/index:v4.18"
	const zetaNewImage = "registry.example/zeta-new:1"

	renderer := newFakeDifferentialCatalogRenderer(t, map[string]string{
		fbcFragment: zetaDifferentialCatalog(zetaNewImage),
		index:       zetaDifferentialCatalog() + invalidAlphaBundleMissingProperties,
	})
	opts := UnreleasedRelatedImagesOptions{
		FBCFragment: fbcFragment,
		IndexImage:  index,
	}

	got, err := getUnreleasedFBCRelatedImages(t.Context(), opts, nil, renderer.render)
	if err != nil {
		t.Fatalf("unreleased related images: %v", err)
	}

	assertStringSlice(t, got, []string{zetaNewImage})
}

// FBC fragment extraction ignores malformed bundles outside the fragment's
// declared package scope.
// 1. Create a zeta FBC fragment with a malformed alpha bundle and an empty index.
// 2. Extract unreleased related images.
// 3. Verify extraction succeeds and ignores the unscoped alpha bundle.
func TestGetUnreleasedFBCRelatedImages_IgnoresMalformedUnscopedFragmentBundles(t *testing.T) {
	const fbcFragment = "registry.example/fbc:latest"
	const index = "registry.example/index:v4.18"
	const zetaNewImage = "registry.example/zeta-new:1"

	renderer := newFakeDifferentialCatalogRenderer(t, map[string]string{
		fbcFragment: zetaDifferentialCatalog(zetaNewImage) + invalidAlphaBundleMissingProperties,
		index:       zetaDifferentialCatalog(),
	})
	opts := UnreleasedRelatedImagesOptions{
		FBCFragment: fbcFragment,
		IndexImage:  index,
	}

	got, err := getUnreleasedFBCRelatedImages(t.Context(), opts, nil, renderer.render)
	if err != nil {
		t.Fatalf("unreleased related images: %v", err)
	}

	assertStringSlice(t, got, []string{zetaNewImage})
}

// Related images from multiple fragment packages are flattened, sorted and
// deduplicated before the index difference is returned.
// 1. Create multiple declared packages with overlapping related image sets.
// 2. Extract unreleased related images.
// 3. Verify the result is flattened, sorted, and deduplicated.
func TestGetUnreleasedFBCRelatedImages_MultiplePackagesSortedUnique(t *testing.T) {
	const fbcFragment = "registry.example/fbc:latest"
	const index = "registry.example/index:v4.18"

	// Given two declared packages introduce overlapping related image sets.
	fbcCatalog := `
{"schema":"olm.package","name":"zeta"}
{"schema":"olm.bundle","package":"zeta","image":"registry.example/zeta-bundle:1","properties":[],"relatedImages":[
  {"image":"registry.example/z-new:1"},
  {"image":"registry.example/shared-new:1"}
]}
{"schema":"olm.package","name":"alpha"}
{"schema":"olm.bundle","package":"alpha","image":"registry.example/alpha-bundle:1","properties":[],"relatedImages":[
  {"image":"registry.example/shared-new:1"},
  {"image":"registry.example/a-new:1"}
]}
`
	indexCatalog := `
{"schema":"olm.package","name":"zeta"}
{"schema":"olm.bundle","package":"zeta","image":"registry.example/zeta-bundle:old","properties":[],"relatedImages":[
  {"image":"registry.example/z-released:1"}
]}
{"schema":"olm.package","name":"alpha"}
{"schema":"olm.bundle","package":"alpha","image":"registry.example/alpha-bundle:old","properties":[],"relatedImages":[
  {"image":"registry.example/a-released:1"}
]}
`
	renderer := newFakeDifferentialCatalogRenderer(t, map[string]string{
		fbcFragment: fbcCatalog,
		index:       indexCatalog,
	})
	opts := UnreleasedRelatedImagesOptions{
		FBCFragment: fbcFragment,
		IndexImage:  index,
	}

	// When unreleased related images are extracted.
	got, err := getUnreleasedFBCRelatedImages(t.Context(), opts, nil, renderer.render)
	if err != nil {
		t.Fatalf("unreleased related images: %v", err)
	}

	// Then the output is flattened across packages, sorted and deduplicated.
	assertStringSlice(t, got, []string{
		"registry.example/a-new:1",
		"registry.example/shared-new:1",
		"registry.example/z-new:1",
	})
}

// Validation and dependency failures return no partial related image result
// and preserve the underlying error for callers.
// 1. Configure invalid inputs or failing dependencies for each case.
// 2. Extract unreleased related images.
// 3. Verify no partial result is returned and the expected error is preserved.
func TestGetUnreleasedFBCRelatedImages_Errors(t *testing.T) {
	renderErr := errors.New("render failed")
	validFBCFragment := "registry.example/fbc"
	validTaggedIndex := "registry.example/index:v1"

	for _, tc := range []struct {
		name     string
		opts     UnreleasedRelatedImagesOptions
		resolver OCPVersionResolver
		renderer catalogRenderer
		wantErr  error
		wantText string
	}{
		{
			name:     "empty fragment",
			opts:     UnreleasedRelatedImagesOptions{IndexImage: "registry.example/index"},
			resolver: differentialOCPVersions("v4.18"),
			wantErr:  ErrEmptyFBCFragment,
		},
		{
			name:     "empty index",
			opts:     UnreleasedRelatedImagesOptions{FBCFragment: validFBCFragment},
			resolver: differentialOCPVersions("v4.18"),
			wantErr:  ErrEmptyIndexImage,
		},
		{
			name: "untagged index with nil resolver",
			opts: UnreleasedRelatedImagesOptions{
				FBCFragment: validFBCFragment,
				IndexImage:  "registry.example/index",
			},
			renderer: newFakeDifferentialCatalogRenderer(t, map[string]string{
				validFBCFragment: zetaDifferentialCatalog(),
			}).render,
			wantErr: ErrNilOCPVersionResolver,
		},
		{
			name: "invalid index image",
			opts: UnreleasedRelatedImagesOptions{
				FBCFragment: validFBCFragment,
				IndexImage:  "index",
			},
			renderer: newFakeDifferentialCatalogRenderer(t, map[string]string{
				validFBCFragment: zetaDifferentialCatalog(),
			}).render,
			wantErr: image.ErrInvalidImageReference,
		},
		{
			name: "fragment render",
			opts: UnreleasedRelatedImagesOptions{
				FBCFragment: validFBCFragment,
				IndexImage:  validTaggedIndex,
			},
			renderer: func(context.Context, string, string, string, Config) (string, error) {
				return "", renderErr
			},
			wantErr: renderErr,
		},
		{
			name: "index render",
			opts: UnreleasedRelatedImagesOptions{
				FBCFragment: validFBCFragment,
				IndexImage:  validTaggedIndex,
			},
			renderer: newFakeDifferentialResultRenderer(t, map[string]differentialRenderResult{
				validFBCFragment: {catalog: zetaDifferentialCatalog()},
				validTaggedIndex: {err: renderErr},
			}).render,
			wantErr: renderErr,
		},
		{
			name: "invalid fragment catalog",
			opts: UnreleasedRelatedImagesOptions{
				FBCFragment: validFBCFragment,
				IndexImage:  validTaggedIndex,
			},
			renderer: newFakeDifferentialCatalogRenderer(t, map[string]string{
				validFBCFragment: `{"schema":"olm.package","name":}`,
			}).render,
			wantText: "failed to extract FBC package names",
		},
		{
			name: "invalid index catalog",
			opts: UnreleasedRelatedImagesOptions{
				FBCFragment: validFBCFragment,
				IndexImage:  validTaggedIndex,
			},
			renderer: newFakeDifferentialCatalogRenderer(t, map[string]string{
				validFBCFragment: zetaDifferentialCatalog("registry.example/new:1"),
				validTaggedIndex: `{"schema":"olm.bundle","package":"zeta","image":"registry.example/bad:1"}`,
			}).render,
			wantText: "failed to extract index catalog metadata",
		},
		{
			name: "fragment without package records",
			opts: UnreleasedRelatedImagesOptions{
				FBCFragment: validFBCFragment,
				IndexImage:  validTaggedIndex,
			},
			renderer: newFakeDifferentialCatalogRenderer(t, map[string]string{
				validFBCFragment: `{"schema":"olm.bundle","package":"zeta","image":"registry.example/zeta-bundle:1","properties":[]}`,
			}).render,
			wantErr: ErrEmptyFBCPackageNames,
		},
		{
			name: "resolved target invariant violation",
			opts: UnreleasedRelatedImagesOptions{
				FBCFragment: validFBCFragment,
				IndexImage:  "registry.example/index",
			},
			renderer: newFakeDifferentialCatalogRenderer(t, map[string]string{
				validFBCFragment: zetaDifferentialCatalog(),
			}).render,
			resolver: func(context.Context, string) ([]string, error) {
				return []string{}, nil
			},
			wantErr: ErrEmptyOCPVersions,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			// Given the scenario-specific invalid input or failing dependency.
			if tc.renderer == nil {
				tc.renderer = func(context.Context, string, string, string, Config) (string, error) {
					t.Fatal("renderer must not be called")
					return "", nil
				}
			}

			// When unreleased related images are extracted.
			got, err := getUnreleasedFBCRelatedImages(t.Context(), tc.opts, tc.resolver, tc.renderer)

			// Then no partial result is returned and the expected error is preserved.
			if got != nil {
				t.Fatalf("unreleased related images = (%q, %v), want nil and %v", got, err, tc.wantErr)
			}
			if tc.wantErr != nil && !errors.Is(err, tc.wantErr) {
				t.Fatalf("error = %v, want wrapping %v", err, tc.wantErr)
			}
			if tc.wantText != "" && (err == nil || !strings.Contains(err.Error(), tc.wantText)) {
				t.Fatalf("error = %q, want containing %q", err, tc.wantText)
			}
		})
	}
}

func differentialOCPVersions(versions ...string) OCPVersionResolver {
	return func(context.Context, string) ([]string, error) {
		return versions, nil
	}
}

func ocpResolverMustNotBeCalled(t *testing.T) OCPVersionResolver {
	t.Helper()
	return func(context.Context, string) ([]string, error) {
		t.Fatal("OCP version resolver must not be called")
		return nil, nil
	}
}

// zetaDifferentialCatalog returns a minimal rendered catalog for package zeta
// with one active bundle and the provided related image references.
func zetaDifferentialCatalog(images ...string) string {
	var b strings.Builder
	b.WriteString(`{"schema":"olm.package","name":"zeta"}` + "\n")
	b.WriteString(`{"schema":"olm.bundle","package":"zeta","image":"registry.example/zeta-bundle:1","properties":[]`)
	if len(images) == 0 {
		b.WriteString("}\n")
		return b.String()
	}
	b.WriteString(`,"relatedImages":[`)
	for i, img := range images {
		if i > 0 {
			b.WriteByte(',')
		}
		b.WriteString(`{"image":"`)
		b.WriteString(img)
		b.WriteString(`"}`)
	}
	b.WriteString("]}\n")
	return b.String()
}

type fakeDifferentialCatalogRenderer struct {
	catalogPathByTarget map[string]string
	renderedTargets     []string
}

func newFakeDifferentialCatalogRenderer(t *testing.T, catalogsByTarget map[string]string) *fakeDifferentialCatalogRenderer {
	t.Helper()
	renderer := &fakeDifferentialCatalogRenderer{catalogPathByTarget: map[string]string{}}
	for target, catalog := range catalogsByTarget {
		path := filepath.Join(t.TempDir(), "catalog.json")
		writeTestFile(t, path, catalog, testFileMode)
		renderer.catalogPathByTarget[target] = path
	}
	return renderer
}

func (r *fakeDifferentialCatalogRenderer) render(_ context.Context, target, _, _ string, _ Config) (string, error) {
	r.renderedTargets = append(r.renderedTargets, target)
	path, ok := r.catalogPathByTarget[target]
	if !ok {
		return "", errors.New("unexpected render target: " + target)
	}
	return path, nil
}

func (r *fakeDifferentialCatalogRenderer) assertRenderedTargets(t *testing.T, want []string) {
	t.Helper()
	if !slices.Equal(r.renderedTargets, want) {
		t.Fatalf("rendered targets = %q, want %q", r.renderedTargets, want)
	}
}

type differentialRenderResult struct {
	catalog string
	err     error
}

type fakeDifferentialResultRenderer struct {
	catalogPathByTarget map[string]string
	errorsByTarget      map[string]error
}

func newFakeDifferentialResultRenderer(t *testing.T, resultsByTarget map[string]differentialRenderResult) *fakeDifferentialResultRenderer {
	t.Helper()
	renderer := &fakeDifferentialResultRenderer{
		catalogPathByTarget: map[string]string{},
		errorsByTarget:      map[string]error{},
	}
	for target, result := range resultsByTarget {
		if result.err != nil {
			renderer.errorsByTarget[target] = result.err
			continue
		}
		path := filepath.Join(t.TempDir(), "catalog.json")
		writeTestFile(t, path, result.catalog, testFileMode)
		renderer.catalogPathByTarget[target] = path
	}
	return renderer
}

func (r *fakeDifferentialResultRenderer) render(_ context.Context, target, _, _ string, _ Config) (string, error) {
	if err, ok := r.errorsByTarget[target]; ok {
		return "", err
	}
	path, ok := r.catalogPathByTarget[target]
	if !ok {
		return "", errors.New("unexpected render target: " + target)
	}
	return path, nil
}
