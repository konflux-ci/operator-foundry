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
	"slices"
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
	InspectFn      func(ctx context.Context, imageRef string) (*imagepkg.InspectResult, error)
	InspectRawFn   func(ctx context.Context, imageRef string) (json.RawMessage, error)
	GetManifestsFn func(ctx context.Context, imageRef string) (map[string]string, error)
}

func (m *mockInspector) Inspect(ctx context.Context, imageRef string) (*imagepkg.InspectResult, error) {
	if m.InspectFn == nil {
		m.t.Fatal("unexpected call to Inspect")
	}
	return m.InspectFn(ctx, imageRef)
}

func (m *mockInspector) InspectRaw(ctx context.Context, imageRef string) (json.RawMessage, error) {
	if m.InspectRawFn == nil {
		m.t.Fatal("unexpected call to InspectRaw")
	}
	return m.InspectRawFn(ctx, imageRef)
}

func (m *mockInspector) GetManifests(ctx context.Context, imageRef string) (map[string]string, error) {
	if m.GetManifestsFn == nil {
		m.t.Fatal("unexpected call to GetManifests")
	}
	return m.GetManifestsFn(ctx, imageRef)
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
	inspector := &mockInspector{
		t: t,
		GetManifestsFn: func(_ context.Context, _ string) (map[string]string, error) {
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
	inspector := &mockInspector{
		t: t,
		GetManifestsFn: func(_ context.Context, _ string) (map[string]string, error) {
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

func TestSelectRequiredPlatformDigest_ErrorsWhenPlatformDigestEmpty(t *testing.T) {
	_, err := selectRequiredPlatformDigest(map[string]string{"linux/amd64": ""}, "linux/amd64")
	if !errors.Is(err, imagepkg.ErrManifestDigestNotFound) {
		t.Fatalf("error = %v, want %v", err, imagepkg.ErrManifestDigestNotFound)
	}
}

func TestBuildProcessedDigests_SortsDigestsByPlatform(t *testing.T) {
	manifests := map[string]string{"linux/arm64": testDigestArm64, "linux/amd64": testDigestAmd64}

	got := buildProcessedDigests(manifests, testDigestIndex)
	wantDigests := []string{testDigestAmd64, testDigestArm64, testDigestIndex}
	if !slices.Equal(got, wantDigests) {
		t.Fatalf("digests = %v, want %v", got, wantDigests)
	}
}

func TestBuildProcessedDigests_DoesNotDuplicateInputDigest(t *testing.T) {
	manifests := map[string]string{"linux/arm64": testDigestArm64, "linux/amd64": testDigestAmd64}

	got := buildProcessedDigests(manifests, testDigestArm64)
	wantDigests := []string{testDigestAmd64, testDigestArm64}
	if !slices.Equal(got, wantDigests) {
		t.Fatalf("digests = %v, want %v", got, wantDigests)
	}
}

func TestBuildProcessedDigests_DoesNotDuplicateManifestDigest(t *testing.T) {
	manifests := map[string]string{"linux/amd64": testDigestAmd64, "linux/arm64": testDigestAmd64}

	got := buildProcessedDigests(manifests, testDigestIndex)
	wantDigests := []string{testDigestAmd64, testDigestIndex}
	if !slices.Equal(got, wantDigests) {
		t.Fatalf("digests = %v, want %v", got, wantDigests)
	}
}

func TestBuildProcessedDigests_IgnoresEmptyInputDigest(t *testing.T) {
	manifests := map[string]string{"linux/amd64": testDigestAmd64}

	got := buildProcessedDigests(manifests, "")
	wantDigests := []string{testDigestAmd64}
	if !slices.Equal(got, wantDigests) {
		t.Fatalf("digests = %v, want %v", got, wantDigests)
	}
}

func TestPrepareImageSetup_ResolvesSourceImageAndProcessedDigests(t *testing.T) {
	const darwinDigest = "sha256:4444444444444444444444444444444444444444444444444444444444444444"
	manifests := map[string]string{
		"darwin/amd64": darwinDigest,
		"linux/amd64":  testDigestAmd64,
		"linux/arm64":  testDigestArm64,
	}
	inspector := &mockInspector{
		t: t,
		InspectRawFn: func(_ context.Context, ref string) (json.RawMessage, error) {
			switch ref {
			case testFBCRepository + "@" + testDigestAmd64:
				return json.RawMessage(`{"annotations":{"org.opencontainers.image.base.name":"registry.redhat.io/openshift4/ose-operator-registry:v4.17","org.opencontainers.image.base.digest":"` + testDigestArm64 + `"}}`), nil
			case testFBCRepository + "@" + darwinDigest:
				return json.RawMessage(`{"annotations":{"org.opencontainers.image.base.name":"registry.example.com/ns/darwin-base:v1"}}`), nil
			default:
				t.Fatalf("InspectRaw reference = %q, want resolved source reference", ref)
				return nil, nil
			}
		},
		GetManifestsFn: func(_ context.Context, ref string) (map[string]string, error) {
			if ref == testFBCRepository+"@"+testDigestAmd64 {
				// The index describes the platform, but the image config lacks it.
				return nil, imagepkg.ErrMissingArchDigest
			}
			if ref != testFBCRepository+"@"+testDigestIndex {
				t.Fatalf("GetManifests ref = %q, want %q", ref, testFBCRepository+"@"+testDigestIndex)
			}
			return manifests, nil
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
	if got.BuildImageRef != testFBCRepository+"@"+testDigestIndex {
		t.Fatalf("BuildImageRef = %q, want %q", got.BuildImageRef, testFBCRepository+"@"+testDigestIndex)
	}
	if got.ReferenceManifestDigest != testDigestAmd64 {
		t.Fatalf("ReferenceManifestDigest = %q, want %q", got.ReferenceManifestDigest, testDigestAmd64)
	}
	if got.SourceImageRef != testFBCRepository+"@"+testDigestAmd64 {
		t.Fatalf("SourceImageRef = %q, want %q", got.SourceImageRef, testFBCRepository+"@"+testDigestAmd64)
	}
	const baseRepo = "registry.redhat.io/openshift4/ose-operator-registry"
	if got.BaseImageRepository != baseRepo || got.BaseImageTag != "v4.17" || got.BaseImagePullRef != baseRepo+"@"+testDigestArm64 {
		t.Fatalf("unexpected validation inputs: %+v", got)
	}
	wantProcessedDigests := []string{darwinDigest, testDigestAmd64, testDigestArm64, testDigestIndex}
	if !slices.Equal(got.ProcessedDigests, wantProcessedDigests) {
		t.Fatalf("ProcessedDigests = %+v, want %+v", got.ProcessedDigests, wantProcessedDigests)
	}
}

func TestPrepareImageSetup_RequiresBaseAnnotation(t *testing.T) {
	inspector := &mockInspector{
		t: t,
		GetManifestsFn: func(ctx context.Context, imageRef string) (map[string]string, error) {
			return map[string]string{"linux/amd64": testDigestAmd64}, nil
		},
		InspectRawFn: func(ctx context.Context, imageRef string) (json.RawMessage, error) {
			return json.RawMessage("{}"), nil
		},
	}
	got, err := PrepareImageSetup(t.Context(), inspector, ImageSetupInput{ImageURL: testFBCRepository, ImageDigest: testDigestIndex})
	if got != nil {
		t.Fatalf("result = %+v, want nil", got)
	}
	if !errors.Is(err, imagepkg.ErrBaseImageAnnotationNotFound) {
		t.Fatalf("error = %v, want %v", err, imagepkg.ErrBaseImageAnnotationNotFound)
	}
	wantRef := testFBCRepository + "@" + testDigestAmd64
	if !strings.Contains(err.Error(), `failed to resolve base image for "`+wantRef+`"`) {
		t.Fatalf("error = %q, want base image resolution context for %q", err, wantRef)
	}
}

func TestPrepareImageSetup_PreservesBaseImageTagWithoutDigest(t *testing.T) {
	const baseRef = "registry.redhat.io/openshift4/ose-operator-registry:v4.17"
	inspector := &mockInspector{
		t: t,
		GetManifestsFn: func(_ context.Context, _ string) (map[string]string, error) {
			return map[string]string{"linux/amd64": testDigestAmd64}, nil
		},
		InspectRawFn: func(_ context.Context, _ string) (json.RawMessage, error) {
			return json.RawMessage(`{"annotations":{"org.opencontainers.image.base.name":"` + baseRef + `"}}`), nil
		},
	}
	got, err := PrepareImageSetup(t.Context(), inspector, ImageSetupInput{
		ImageURL:    testFBCRepository,
		ImageDigest: testDigestIndex,
	})
	if err != nil {
		t.Fatalf("PrepareImageSetup() unexpected error: %v", err)
	}
	if got.BaseImagePullRef != baseRef {
		t.Errorf("BaseImagePullRef = %q, want %q", got.BaseImagePullRef, baseRef)
	}
}
