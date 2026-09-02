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

package image

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"sync"
	"sync/atomic"
	"testing"

	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/remote"
	"github.com/google/go-containerregistry/pkg/v1/types"
)

// ── Test helpers ───────────────────────────────────────────────────────────────

// mockFetcher implements ManifestFetcher for testing.
type mockFetcher struct {
	results map[string]*remote.Descriptor
	errors  map[string]error
}

func (m *mockFetcher) FetchManifest(_ context.Context, imageRef string) (*remote.Descriptor, error) {
	if err, ok := m.errors[imageRef]; ok {
		return nil, err
	}
	if desc, ok := m.results[imageRef]; ok {
		return desc, nil
	}
	return nil, fmt.Errorf("unexpected image ref in mock: %q", imageRef)
}

// makeDescriptor creates a remote.Descriptor with the given MediaType.
func makeDescriptor(mediaType types.MediaType) *remote.Descriptor {
	return &remote.Descriptor{
		Descriptor: v1.Descriptor{
			MediaType: mediaType,
		},
	}
}

// makeManifestListDescriptor creates a remote.Descriptor for a DockerManifestList
// with inner manifests of the given MediaTypes.
func makeManifestListDescriptor(innerMediaTypes ...types.MediaType) *remote.Descriptor {
	var manifests []v1.Descriptor
	for i, mt := range innerMediaTypes {
		manifests = append(manifests, v1.Descriptor{
			MediaType: mt,
			Size:      100,
			Digest: v1.Hash{
				Algorithm: "sha256",
				Hex:       fmt.Sprintf("%064d", i),
			},
		})
	}
	idx := v1.IndexManifest{
		SchemaVersion: 2,
		MediaType:     types.DockerManifestList,
		Manifests:     manifests,
	}
	raw, _ := json.Marshal(idx)

	return &remote.Descriptor{
		Descriptor: v1.Descriptor{
			MediaType: types.DockerManifestList,
		},
		Manifest: raw,
	}
}

// ── Unit tests: checkRelatedImageMediaType ─────────────────────────────────────

func TestCheckRelatedImageMediaType_DockerV2_Passes(t *testing.T) {
	fetcher := &mockFetcher{
		results: map[string]*remote.Descriptor{
			"quay.io/example/img:v1": makeDescriptor(types.DockerManifestSchema2),
		},
	}
	result := checkRelatedImageMediaType(context.Background(), "quay.io/example/img:v1", fetcher, "4.20")
	if !result.Passed {
		t.Error("got Passed=false, want true for Docker V2 manifest")
	}
}

func TestCheckRelatedImageMediaType_OCIManifest_WrongMediaType(t *testing.T) {
	fetcher := &mockFetcher{
		results: map[string]*remote.Descriptor{
			"quay.io/example/oci:v1": makeDescriptor(types.OCIManifestSchema1),
		},
	}
	result := checkRelatedImageMediaType(context.Background(), "quay.io/example/oci:v1", fetcher, "4.20")
	if result.Passed {
		t.Error("got Passed=true, want false for OCI manifest")
	}
	if result.WrongMediaTypeImage != "quay.io/example/oci:v1" {
		t.Errorf("got WrongMediaTypeImage=%q, want quay.io/example/oci:v1", result.WrongMediaTypeImage)
	}
}

func TestCheckRelatedImageMediaType_OCIImageIndex_WrongMediaType(t *testing.T) {
	fetcher := &mockFetcher{
		results: map[string]*remote.Descriptor{
			"quay.io/example/idx:v1": makeDescriptor(types.OCIImageIndex),
		},
	}
	result := checkRelatedImageMediaType(context.Background(), "quay.io/example/idx:v1", fetcher, "4.20")
	if result.Passed {
		t.Error("got Passed=true, want false for OCI image index")
	}
	if result.WrongMediaTypeImage != "quay.io/example/idx:v1" {
		t.Errorf("got WrongMediaTypeImage=%q, want quay.io/example/idx:v1", result.WrongMediaTypeImage)
	}
}

func TestCheckRelatedImageMediaType_DockerManifestList_AllDockerV2_Passes(t *testing.T) {
	fetcher := &mockFetcher{
		results: map[string]*remote.Descriptor{
			"quay.io/example/multi:v1": makeManifestListDescriptor(
				types.DockerManifestSchema2,
				types.DockerManifestSchema2,
			),
		},
	}
	result := checkRelatedImageMediaType(context.Background(), "quay.io/example/multi:v1", fetcher, "4.20")
	if !result.Passed {
		t.Error("got Passed=false, want true for manifest list with all Docker V2 inner manifests")
	}
}

func TestCheckRelatedImageMediaType_DockerManifestList_OCIInner_WrongMediaType(t *testing.T) {
	fetcher := &mockFetcher{
		results: map[string]*remote.Descriptor{
			"quay.io/example/mixed:v1": makeManifestListDescriptor(
				types.DockerManifestSchema2,
				types.OCIManifestSchema1,
			),
		},
	}
	result := checkRelatedImageMediaType(context.Background(), "quay.io/example/mixed:v1", fetcher, "4.20")
	if result.Passed {
		t.Error("got Passed=true, want false for manifest list with OCI inner manifest")
	}
	if result.WrongMediaTypeImage != "quay.io/example/mixed:v1" {
		t.Errorf("got WrongMediaTypeImage=%q, want quay.io/example/mixed:v1", result.WrongMediaTypeImage)
	}
}

func TestCheckRelatedImageMediaType_FetchError_Broken(t *testing.T) {
	fetcher := &mockFetcher{
		errors: map[string]error{
			"quay.io/example/broken:v1": fmt.Errorf("connection refused"),
		},
	}
	result := checkRelatedImageMediaType(context.Background(), "quay.io/example/broken:v1", fetcher, "4.20")
	if result.Passed {
		t.Error("got Passed=true, want false when fetch fails")
	}
	if result.BrokenImage != "quay.io/example/broken:v1" {
		t.Errorf("got BrokenImage=%q, want quay.io/example/broken:v1", result.BrokenImage)
	}
	if result.WrongMediaTypeImage != "" {
		t.Errorf("got WrongMediaTypeImage=%q, want empty", result.WrongMediaTypeImage)
	}
}

func TestCheckRelatedImageMediaType_UnexpectedMediaType_WrongMediaType(t *testing.T) {
	fetcher := &mockFetcher{
		results: map[string]*remote.Descriptor{
			"quay.io/example/weird:v1": makeDescriptor(types.MediaType("application/vnd.unknown")),
		},
	}
	result := checkRelatedImageMediaType(context.Background(), "quay.io/example/weird:v1", fetcher, "4.20")
	if result.Passed {
		t.Error("got Passed=true, want false for unexpected media type")
	}
	if result.WrongMediaTypeImage != "quay.io/example/weird:v1" {
		t.Errorf("got WrongMediaTypeImage=%q, want quay.io/example/weird:v1", result.WrongMediaTypeImage)
	}
}

func TestCheckRelatedImageMediaType_ManifestListInvalidBody_Broken(t *testing.T) {
	fetcher := &mockFetcher{
		results: map[string]*remote.Descriptor{
			"quay.io/example/broken-list:v1": {
				Descriptor: v1.Descriptor{
					MediaType: types.DockerManifestList,
				},
				Manifest: []byte("not valid json"),
			},
		},
	}
	result := checkRelatedImageMediaType(context.Background(), "quay.io/example/broken-list:v1", fetcher, "4.20")
	if result.Passed {
		t.Error("got Passed=true, want false for broken manifest list")
	}
	if result.BrokenImage != "quay.io/example/broken-list:v1" {
		t.Errorf("got BrokenImage=%q, want quay.io/example/broken-list:v1", result.BrokenImage)
	}
}

// ── Unit tests: collectResult ──────────────────────────────────────────────────

func TestCollectResult_Passed_NoMutation(t *testing.T) {
	var mu sync.Mutex
	var broken, wrong []string
	collectResult(mediaTypeCheckSingleResult{Passed: true}, &mu, &broken, &wrong)
	if len(broken) != 0 {
		t.Errorf("got broken=%v, want empty", broken)
	}
	if len(wrong) != 0 {
		t.Errorf("got wrong=%v, want empty", wrong)
	}
}

func TestCollectResult_BrokenImage_Appended(t *testing.T) {
	var mu sync.Mutex
	var broken, wrong []string
	collectResult(mediaTypeCheckSingleResult{BrokenImage: "quay.io/broken:v1"}, &mu, &broken, &wrong)
	if len(broken) != 1 || broken[0] != "quay.io/broken:v1" {
		t.Errorf("got broken=%v, want [quay.io/broken:v1]", broken)
	}
	if len(wrong) != 0 {
		t.Errorf("got wrong=%v, want empty", wrong)
	}
}

func TestCollectResult_WrongMediaType_Appended(t *testing.T) {
	var mu sync.Mutex
	var broken, wrong []string
	collectResult(mediaTypeCheckSingleResult{WrongMediaTypeImage: "quay.io/oci:v1"}, &mu, &broken, &wrong)
	if len(broken) != 0 {
		t.Errorf("got broken=%v, want empty", broken)
	}
	if len(wrong) != 1 || wrong[0] != "quay.io/oci:v1" {
		t.Errorf("got wrong=%v, want [quay.io/oci:v1]", wrong)
	}
}

// ── Unit tests: deduplicateImages ─────────────────────────────────────────────

func TestDeduplicateImages_NoDuplicates(t *testing.T) {
	images := []string{"a:v1", "b:v1", "c:v1"}
	result := deduplicateImages(images)
	if len(result) != 3 {
		t.Errorf("got %d images, want 3", len(result))
	}
}

func TestDeduplicateImages_RemovesDuplicates(t *testing.T) {
	images := []string{"a:v1", "b:v1", "a:v1", "c:v1", "b:v1"}
	result := deduplicateImages(images)
	if len(result) != 3 {
		t.Errorf("got %d images, want 3", len(result))
	}
	expected := []string{"a:v1", "b:v1", "c:v1"}
	for i, img := range expected {
		if result[i] != img {
			t.Errorf("result[%d]=%q, want %q (order not preserved)", i, result[i], img)
		}
	}
}

func TestDeduplicateImages_EmptySlice(t *testing.T) {
	result := deduplicateImages([]string{})
	if len(result) != 0 {
		t.Errorf("got %d images, want 0", len(result))
	}
}

func TestDeduplicateImages_AllDuplicates(t *testing.T) {
	images := []string{"a:v1", "a:v1", "a:v1"}
	result := deduplicateImages(images)
	if len(result) != 1 || result[0] != "a:v1" {
		t.Errorf("got %v, want [a:v1]", result)
	}
}

// ── Batch tests: CheckRelatedImagesMediaType ───────────────────────────────────

func TestCheckRelatedImagesMediaType_OCPVersionGTE421_SkipsCheck(t *testing.T) {
	fetcher := &mockFetcher{}
	result, err := CheckRelatedImagesMediaType(context.Background(), "4.21", []string{"quay.io/example/img:v1"}, fetcher)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Passed {
		t.Error("got Passed=false, want true for OCP >= 4.21")
	}
}

func TestCheckRelatedImagesMediaType_OCPVersion500_SkipsCheck(t *testing.T) {
	fetcher := &mockFetcher{}
	result, err := CheckRelatedImagesMediaType(context.Background(), "5.0", []string{"quay.io/example/img:v1"}, fetcher)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Passed {
		t.Error("got Passed=false, want true for OCP >= 4.21")
	}
}

func TestCheckRelatedImagesMediaType_InvalidOCPVersion_ReturnsError(t *testing.T) {
	fetcher := &mockFetcher{}
	_, err := CheckRelatedImagesMediaType(context.Background(), "invalid", []string{"quay.io/example/img:v1"}, fetcher)
	if err == nil {
		t.Fatal("expected error for invalid OCP version, got nil")
	}
}

func TestCheckRelatedImagesMediaType_EmptyImageList_Passes(t *testing.T) {
	fetcher := &mockFetcher{}
	result, err := CheckRelatedImagesMediaType(context.Background(), "4.20", []string{}, fetcher)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Passed {
		t.Error("got Passed=false, want true for empty image list")
	}
}

func TestCheckRelatedImagesMediaType_MixedResults_CollectsAllFailures(t *testing.T) {
	fetcher := &mockFetcher{
		results: map[string]*remote.Descriptor{
			"quay.io/example/good:v1":  makeDescriptor(types.DockerManifestSchema2),
			"quay.io/example/bad:v1":   makeDescriptor(types.OCIManifestSchema1),
			"quay.io/example/multi:v1": makeManifestListDescriptor(types.DockerManifestSchema2, types.OCIManifestSchema1),
		},
		errors: map[string]error{
			"quay.io/example/down:v1": fmt.Errorf("timeout"),
		},
	}
	images := []string{
		"quay.io/example/good:v1",
		"quay.io/example/bad:v1",
		"quay.io/example/down:v1",
		"quay.io/example/multi:v1",
	}
	result, err := CheckRelatedImagesMediaType(context.Background(), "4.20", images, fetcher)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Passed {
		t.Error("got Passed=true, want false")
	}
	if len(result.WrongMediaTypeImages) != 2 {
		t.Errorf("got WrongMediaTypeImages=%v, want 2 (bad + mixed)", result.WrongMediaTypeImages)
	}
	if len(result.BrokenImages) != 1 || result.BrokenImages[0] != "quay.io/example/down:v1" {
		t.Errorf("got BrokenImages=%v, want [quay.io/example/down:v1]", result.BrokenImages)
	}
}

func TestCheckRelatedImagesMediaType_DuplicateImagesCheckedOnce(t *testing.T) {
	fetcher := &mockFetcher{
		results: map[string]*remote.Descriptor{
			"quay.io/example/img:v1": makeDescriptor(types.DockerManifestSchema2),
		},
	}
	countingFetcher := &countingFetcherWrapper{inner: fetcher}

	images := []string{
		"quay.io/example/img:v1",
		"quay.io/example/img:v1",
		"quay.io/example/img:v1",
	}
	result, err := CheckRelatedImagesMediaType(context.Background(), "4.20", images, countingFetcher)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Passed {
		t.Error("got Passed=false, want true")
	}
	if countingFetcher.count.Load() != 1 {
		t.Errorf("got %d fetch calls, want 1 (duplicates should be deduplicated)", countingFetcher.count.Load())
	}
}

type countingFetcherWrapper struct {
	inner ManifestFetcher
	count atomic.Int64
}

func (f *countingFetcherWrapper) FetchManifest(ctx context.Context, imageRef string) (*remote.Descriptor, error) {
	f.count.Add(1)
	return f.inner.FetchManifest(ctx, imageRef)
}

// ── Batch tests: retry behavior ────────────────────────────────────────────────

// flakyFetcher fails the first call for an image and succeeds on subsequent calls.
type flakyFetcher struct {
	mu          sync.Mutex
	calls       map[string]int
	successDesc *remote.Descriptor
}

func (f *flakyFetcher) FetchManifest(_ context.Context, imageRef string) (*remote.Descriptor, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls[imageRef]++
	if f.calls[imageRef] == 1 {
		return nil, fmt.Errorf("transient network error")
	}
	return f.successDesc, nil
}

func TestCheckRelatedImagesMediaType_RetryRecoversBrokenImage(t *testing.T) {
	fetcher := &flakyFetcher{
		calls:       make(map[string]int),
		successDesc: makeDescriptor(types.DockerManifestSchema2),
	}
	result, err := CheckRelatedImagesMediaType(context.Background(), "4.20", []string{"quay.io/example/flaky:v1"}, fetcher)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Passed {
		t.Error("got Passed=false, want true — image should recover on retry")
	}
	if len(result.BrokenImages) != 0 {
		t.Errorf("got BrokenImages=%v, want empty after successful retry", result.BrokenImages)
	}
}

func TestCheckRelatedImagesMediaType_RetryStillBroken(t *testing.T) {
	fetcher := &mockFetcher{
		errors: map[string]error{
			"quay.io/example/down:v1": fmt.Errorf("persistent failure"),
		},
	}
	result, err := CheckRelatedImagesMediaType(context.Background(), "4.20", []string{"quay.io/example/down:v1"}, fetcher)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Passed {
		t.Error("got Passed=true, want false — image fails both passes")
	}
	if len(result.BrokenImages) != 1 || result.BrokenImages[0] != "quay.io/example/down:v1" {
		t.Errorf("got BrokenImages=%v, want [quay.io/example/down:v1]", result.BrokenImages)
	}
}

type brokenThenOCIFetcher struct {
	mu    sync.Mutex
	calls map[string]int
}

func (f *brokenThenOCIFetcher) FetchManifest(_ context.Context, imageRef string) (*remote.Descriptor, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls[imageRef]++
	if f.calls[imageRef] == 1 {
		return nil, fmt.Errorf("transient error")
	}
	return makeDescriptor(types.OCIManifestSchema1), nil
}

func TestCheckRelatedImagesMediaType_RetryBrokenBecomesWrongMediaType(t *testing.T) {
	fetcher := &brokenThenOCIFetcher{
		calls: make(map[string]int),
	}
	result, err := CheckRelatedImagesMediaType(context.Background(), "4.20", []string{"quay.io/example/flaky-oci:v1"}, fetcher)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Passed {
		t.Error("got Passed=true, want false — image has OCI mediatype on retry")
	}
	if len(result.BrokenImages) != 0 {
		t.Errorf("got BrokenImages=%v, want empty — image was reachable on retry", result.BrokenImages)
	}
	if len(result.WrongMediaTypeImages) != 1 || result.WrongMediaTypeImages[0] != "quay.io/example/flaky-oci:v1" {
		t.Errorf("got WrongMediaTypeImages=%v, want [quay.io/example/flaky-oci:v1]", result.WrongMediaTypeImages)
	}
}

// ── Batch tests: concurrency and context ───────────────────────────────────────

func TestCheckRelatedImagesMediaType_ConcurrencyWithManyImages(t *testing.T) {
	results := make(map[string]*remote.Descriptor)
	var images []string
	for i := range 200 {
		ref := fmt.Sprintf("quay.io/example/img-%d:v1", i)
		images = append(images, ref)
		if i%10 == 0 {
			results[ref] = makeDescriptor(types.OCIManifestSchema1)
		} else {
			results[ref] = makeDescriptor(types.DockerManifestSchema2)
		}
	}
	fetcher := &mockFetcher{results: results}
	result, err := CheckRelatedImagesMediaType(context.Background(), "4.20", images, fetcher)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Passed {
		t.Error("got Passed=true, want false — some images have OCI mediatype")
	}
	if len(result.WrongMediaTypeImages) != 20 {
		t.Errorf("got %d WrongMediaTypeImages, want 20", len(result.WrongMediaTypeImages))
	}
	for i := range 200 {
		if i%10 == 0 {
			ref := fmt.Sprintf("quay.io/example/img-%d:v1", i)
			if !slices.Contains(result.WrongMediaTypeImages, ref) {
				t.Errorf("expected %s in WrongMediaTypeImages", ref)
			}
		}
	}
}

func TestCheckRelatedImagesMediaType_CancelledContext_ReturnsError(t *testing.T) {
	fetcher := &mockFetcher{
		errors: map[string]error{
			"quay.io/example/img:v1": fmt.Errorf("connection refused"),
		},
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := CheckRelatedImagesMediaType(ctx, "4.20", []string{"quay.io/example/img:v1"}, fetcher)
	if err == nil {
		t.Fatal("expected error for cancelled context, got nil — should not return false BrokenImages")
	}
}

func TestCheckRelatedImagesMediaType_EmptyManifestList_Passes(t *testing.T) {
	fetcher := &mockFetcher{
		results: map[string]*remote.Descriptor{
			"quay.io/example/empty-list:v1": makeManifestListDescriptor(),
		},
	}
	result, err := CheckRelatedImagesMediaType(context.Background(), "4.20", []string{"quay.io/example/empty-list:v1"}, fetcher)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Passed {
		t.Error("got Passed=false, want true for manifest list with zero inner manifests")
	}
}
