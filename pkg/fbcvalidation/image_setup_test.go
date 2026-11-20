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

package fbcvalidation

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"

	imagepkg "github.com/konflux-ci/operator-foundry/pkg/image"
)

const (
	testFBCRepository = "registry.example.com/ns/fbc"
	testDigestAmd64   = "sha256:1111111111111111111111111111111111111111111111111111111111111111"
	testDigestArm64   = "sha256:2222222222222222222222222222222222222222222222222222222222222222"
	testDigestIndex   = "sha256:3333333333333333333333333333333333333333333333333333333333333333"
)

type mockInspector struct {
	t              *testing.T
	inspectFn      func(context.Context, string) (*imagepkg.InspectResult, error)
	inspectRawFn   func(context.Context, string) (json.RawMessage, error)
	getManifestsFn func(context.Context, string) (map[string]string, error)
}

func (m mockInspector) Inspect(ctx context.Context, imageRef string) (*imagepkg.InspectResult, error) {
	if m.inspectFn == nil {
		m.t.Fatal("unexpected Inspect call")
	}
	return m.inspectFn(ctx, imageRef)
}

func (m mockInspector) InspectRaw(ctx context.Context, imageRef string) (json.RawMessage, error) {
	if m.inspectRawFn == nil {
		m.t.Fatal("unexpected InspectRaw call")
	}
	return m.inspectRawFn(ctx, imageRef)
}

func (m mockInspector) GetManifests(ctx context.Context, imageRef string) (map[string]string, error) {
	if m.getManifestsFn == nil {
		m.t.Fatal("unexpected GetManifests call")
	}
	return m.getManifestsFn(ctx, imageRef)
}

func TestResolveValidationImageSource_RemovesTagAndUsesInputDigest(t *testing.T) {
	fbcRepository, sourceRef, err := resolveValidationImageSource(testFBCRepository+":v1@"+testDigestAmd64, testDigestIndex)
	if err != nil {
		t.Fatalf("resolveValidationImageSource() unexpected error: %v", err)
	}
	if fbcRepository != testFBCRepository {
		t.Fatalf("fbcRepository = %q, want %q", fbcRepository, testFBCRepository)
	}
	if sourceRef != testFBCRepository+"@"+testDigestIndex {
		t.Fatalf("sourceRef = %q, want %q", sourceRef, testFBCRepository+"@"+testDigestIndex)
	}
}

func TestResolveValidationImageSource_Errors(t *testing.T) {
	tests := []struct {
		name        string
		imageURL    string
		imageDigest string
		wantText    string
	}{
		{
			name:        "empty image URL",
			imageURL:    "",
			imageDigest: testDigestIndex,
			wantText:    "image URL",
		},
		{
			name:        "unqualified image URL",
			imageURL:    "fbc:v1",
			imageDigest: testDigestIndex,
			wantText:    "image URL",
		},
		{
			name:        "empty image digest",
			imageURL:    testFBCRepository + ":v1",
			imageDigest: "",
			wantText:    "image digest",
		},
		{
			name:        "malformed image digest",
			imageURL:    testFBCRepository + ":v1",
			imageDigest: "sha256:bad",
			wantText:    "image digest",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, _, err := resolveValidationImageSource(tc.imageURL, tc.imageDigest)
			if err == nil {
				t.Fatal("expected error, got nil")
			}
			if !strings.Contains(err.Error(), tc.wantText) {
				t.Fatalf("error = %q, want text %q", err.Error(), tc.wantText)
			}
		})
	}
}

func TestPrepareImageSetup_RequiresLinuxAmd64Manifest(t *testing.T) {
	inspector := mockInspector{
		t: t,
		getManifestsFn: func(_ context.Context, _ string) (map[string]string, error) {
			return map[string]string{"linux/arm64": testDigestArm64, "windows/amd64": testDigestAmd64}, nil
		},
	}

	_, err := PrepareImageSetup(t.Context(), inspector, ImageSetupInput{
		ImageURL:    testFBCRepository + ":v1",
		ImageDigest: testDigestIndex,
	})
	if !errors.Is(err, imagepkg.ErrManifestDigestNotFound) {
		t.Fatalf("error = %v, want %v", err, imagepkg.ErrManifestDigestNotFound)
	}
}

func TestPrepareImageSetup_PropagatesManifestResolutionError(t *testing.T) {
	wantErr := errors.New("manifest unavailable")
	inspector := mockInspector{
		t: t,
		getManifestsFn: func(_ context.Context, _ string) (map[string]string, error) {
			return nil, wantErr
		},
	}

	_, err := PrepareImageSetup(t.Context(), inspector, ImageSetupInput{
		ImageURL:    testFBCRepository + ":v1",
		ImageDigest: testDigestIndex,
	})
	if !errors.Is(err, wantErr) {
		t.Fatalf("error = %v, want wrapped %v", err, wantErr)
	}
	if !strings.Contains(err.Error(), "failed to resolve source manifests") {
		t.Fatalf("error = %q, want resolve manifests context", err.Error())
	}
}

func TestSelectRequiredPlatformDigest_ReturnsRequiredPlatform(t *testing.T) {
	manifests := map[string]string{"linux/amd64": testDigestAmd64}
	got, err := selectRequiredPlatformDigest(manifests, "linux/amd64")
	if err != nil {
		t.Fatalf("selectRequiredPlatformDigest() unexpected error: %v", err)
	}
	if got != testDigestAmd64 {
		t.Fatalf("digest = %q, want %q", got, testDigestAmd64)
	}
}

func TestSelectRequiredPlatformDigest_ErrorsWhenPlatformMissing(t *testing.T) {
	_, err := selectRequiredPlatformDigest(map[string]string{"linux/arm64": testDigestArm64}, "linux/amd64")
	if !errors.Is(err, imagepkg.ErrManifestDigestNotFound) {
		t.Fatalf("error = %v, want %v", err, imagepkg.ErrManifestDigestNotFound)
	}
}

func TestBuildImagesProcessed_SortsDigestsByPlatform(t *testing.T) {
	manifests := map[string]string{"linux/arm64": testDigestArm64, "linux/amd64": testDigestAmd64}

	got := buildImagesProcessed(testFBCRepository, manifests, testDigestIndex)
	wantDigests := []string{testDigestAmd64, testDigestArm64, testDigestIndex}
	if got.Image.Pullspec != testFBCRepository || !reflect.DeepEqual(got.Image.Digests, wantDigests) {
		t.Fatalf("buildImagesProcessed() = %+v, want pullspec %q and digests %v", got, testFBCRepository, wantDigests)
	}

	data, err := json.Marshal(got)
	if err != nil {
		t.Fatalf("json.Marshal() unexpected error: %v", err)
	}
	var decoded ImagesProcessed
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("json.Unmarshal() unexpected error: %v", err)
	}
	if !reflect.DeepEqual(decoded, got) {
		t.Fatalf("marshaled ImagesProcessed = %+v, want %+v", decoded, got)
	}
}

func TestBuildImagesProcessed_DoesNotDuplicateInputDigest(t *testing.T) {
	manifests := map[string]string{"linux/arm64": testDigestArm64, "linux/amd64": testDigestAmd64}

	got := buildImagesProcessed(testFBCRepository, manifests, testDigestArm64)
	wantDigests := []string{testDigestAmd64, testDigestArm64}
	if !reflect.DeepEqual(got.Image.Digests, wantDigests) {
		t.Fatalf("digests = %v, want %v", got.Image.Digests, wantDigests)
	}
}

func TestBuildImagesProcessed_DoesNotDuplicateManifestDigest(t *testing.T) {
	manifests := map[string]string{"linux/amd64": testDigestAmd64, "linux/arm64": testDigestAmd64}

	got := buildImagesProcessed(testFBCRepository, manifests, testDigestIndex)
	wantDigests := []string{testDigestAmd64, testDigestIndex}
	if !reflect.DeepEqual(got.Image.Digests, wantDigests) {
		t.Fatalf("digests = %v, want %v", got.Image.Digests, wantDigests)
	}
}

func TestBuildImagesProcessed_IgnoresEmptyInputDigest(t *testing.T) {
	manifests := map[string]string{"linux/amd64": testDigestAmd64}

	got := buildImagesProcessed(testFBCRepository, manifests, "")
	wantDigests := []string{testDigestAmd64}
	if !reflect.DeepEqual(got.Image.Digests, wantDigests) {
		t.Fatalf("digests = %v, want %v", got.Image.Digests, wantDigests)
	}
}

func TestPrepareImageSetup_ResolvesSourceImageAndImagesProcessed(t *testing.T) {
	inspector := mockInspector{
		t: t,
		getManifestsFn: func(_ context.Context, ref string) (map[string]string, error) {
			if ref != testFBCRepository+"@"+testDigestIndex {
				t.Fatalf("GetManifests ref = %q, want %q", ref, testFBCRepository+"@"+testDigestIndex)
			}
			return map[string]string{"linux/amd64": testDigestAmd64, "linux/arm64": testDigestArm64}, nil
		},
	}

	got, err := PrepareImageSetup(t.Context(), inspector, ImageSetupInput{
		ImageURL:    testFBCRepository + ":v1",
		ImageDigest: testDigestIndex,
	})
	if err != nil {
		t.Fatalf("PrepareImageSetup() unexpected error: %v", err)
	}

	if got.FBCRepository != testFBCRepository {
		t.Fatalf("FBCRepository = %q, want %q", got.FBCRepository, testFBCRepository)
	}
	if got.InitialSourceRef != testFBCRepository+"@"+testDigestIndex {
		t.Fatalf("InitialSourceRef = %q, want %q", got.InitialSourceRef, testFBCRepository+"@"+testDigestIndex)
	}
	if got.ReferenceDigest != testDigestAmd64 {
		t.Fatalf("ReferenceDigest = %q, want %q", got.ReferenceDigest, testDigestAmd64)
	}
	if got.SourceImageRef != testFBCRepository+"@"+testDigestAmd64 {
		t.Fatalf("SourceImageRef = %q, want %q", got.SourceImageRef, testFBCRepository+"@"+testDigestAmd64)
	}
	wantProcessed := buildImagesProcessed(testFBCRepository, map[string]string{"linux/amd64": testDigestAmd64, "linux/arm64": testDigestArm64}, testDigestIndex)
	if !reflect.DeepEqual(got.ImagesProcessed, wantProcessed) {
		t.Fatalf("ImagesProcessed = %+v, want %+v", got.ImagesProcessed, wantProcessed)
	}
}
