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

package mediatype

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

func makeDescriptor(mediaType types.MediaType) *remote.Descriptor {
	return &remote.Descriptor{
		Descriptor: v1.Descriptor{
			MediaType: mediaType,
		},
	}
}

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

type countingFetcherWrapper struct {
	inner ManifestFetcher
	count atomic.Int64
}

func (f *countingFetcherWrapper) FetchManifest(ctx context.Context, imageRef string) (*remote.Descriptor, error) {
	f.count.Add(1)
	return f.inner.FetchManifest(ctx, imageRef)
}

// sequenceFetcher returns pre-configured responses in order per image reference.
// On the Nth call for an image, it returns responses[imageRef][N-1].
// If N exceeds the slice length, the last response is repeated.
type fetchResponse struct {
	desc *remote.Descriptor
	err  error
	fn   func()
}

type sequenceFetcher struct {
	mu        sync.Mutex
	calls     map[string]int
	responses map[string][]fetchResponse
}

func (f *sequenceFetcher) FetchManifest(_ context.Context, imageRef string) (*remote.Descriptor, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls[imageRef]++
	seq := f.responses[imageRef]
	idx := f.calls[imageRef] - 1
	if idx >= len(seq) {
		idx = len(seq) - 1
	}
	r := seq[idx]
	if r.fn != nil {
		r.fn()
	}
	return r.desc, r.err
}

// ── Unit tests: checkRelatedImageMediaType ─────────────────────────────────────

func TestCheckRelatedImageMediaType(t *testing.T) {
	tests := []struct {
		name               string
		imageRef           string
		desc               *remote.Descriptor
		fetchErr           error
		wantBrokenImage    string
		wantWrongMediaType string
	}{
		{
			name:     "Docker V2 passes",
			imageRef: "quay.io/example/img:v1",
			desc:     makeDescriptor(types.DockerManifestSchema2),
		},
		{
			name:               "OCI manifest is wrong mediaType",
			imageRef:           "quay.io/example/oci:v1",
			desc:               makeDescriptor(types.OCIManifestSchema1),
			wantWrongMediaType: "quay.io/example/oci:v1",
		},
		{
			name:               "OCI image index is wrong mediaType",
			imageRef:           "quay.io/example/idx:v1",
			desc:               makeDescriptor(types.OCIImageIndex),
			wantWrongMediaType: "quay.io/example/idx:v1",
		},
		{
			name:     "manifest list with all Docker V2 inner manifests passes",
			imageRef: "quay.io/example/multi:v1",
			desc:     makeManifestListDescriptor(types.DockerManifestSchema2, types.DockerManifestSchema2),
		},
		{
			name:               "manifest list with OCI inner manifest is wrong mediaType",
			imageRef:           "quay.io/example/mixed:v1",
			desc:               makeManifestListDescriptor(types.DockerManifestSchema2, types.OCIManifestSchema1),
			wantWrongMediaType: "quay.io/example/mixed:v1",
		},
		{
			name:            "fetch error marks image as broken",
			imageRef:        "quay.io/example/broken:v1",
			fetchErr:        fmt.Errorf("connection refused"),
			wantBrokenImage: "quay.io/example/broken:v1",
		},
		{
			name:               "unexpected mediaType is wrong mediaType",
			imageRef:           "quay.io/example/weird:v1",
			desc:               makeDescriptor(types.MediaType("application/vnd.unknown")),
			wantWrongMediaType: "quay.io/example/weird:v1",
		},
		{
			name:     "manifest list with invalid JSON body is broken",
			imageRef: "quay.io/example/broken-list:v1",
			desc: &remote.Descriptor{
				Descriptor: v1.Descriptor{
					MediaType: types.DockerManifestList,
				},
				Manifest: []byte("not valid json"),
			},
			wantBrokenImage: "quay.io/example/broken-list:v1",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fetcher := &mockFetcher{
				results: map[string]*remote.Descriptor{},
				errors:  map[string]error{},
			}
			if tt.desc != nil {
				fetcher.results[tt.imageRef] = tt.desc
			}
			if tt.fetchErr != nil {
				fetcher.errors[tt.imageRef] = tt.fetchErr
			}

			result := checkRelatedImageMediaType(context.Background(), tt.imageRef, fetcher, "4.20")

			if result.BrokenImage != tt.wantBrokenImage {
				t.Errorf("got BrokenImage=%q, want %q", result.BrokenImage, tt.wantBrokenImage)
			}
			if result.WrongMediaTypeImage != tt.wantWrongMediaType {
				t.Errorf("got WrongMediaTypeImage=%q, want %q", result.WrongMediaTypeImage, tt.wantWrongMediaType)
			}
		})
	}
}

// ── Unit tests: collectResult ────────────────────────────────────────────────

func TestCollectResult(t *testing.T) {
	tests := []struct {
		name       string
		result     mediaTypeCheckSingleResult
		wantBroken []string
		wantWrong  []string
	}{
		{
			name:   "passed result does not mutate slices",
			result: mediaTypeCheckSingleResult{},
		},
		{
			name:       "broken image appended",
			result:     mediaTypeCheckSingleResult{BrokenImage: "quay.io/broken:v1"},
			wantBroken: []string{"quay.io/broken:v1"},
		},
		{
			name:      "wrong mediaType appended",
			result:    mediaTypeCheckSingleResult{WrongMediaTypeImage: "quay.io/oci:v1"},
			wantWrong: []string{"quay.io/oci:v1"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var mu sync.Mutex
			var broken, wrong []string
			collectResult(tt.result, &mu, &wrong, &broken)
			if len(broken) != len(tt.wantBroken) {
				t.Errorf("got broken=%v, want %v", broken, tt.wantBroken)
			}
			if len(wrong) != len(tt.wantWrong) {
				t.Errorf("got wrong=%v, want %v", wrong, tt.wantWrong)
			}
		})
	}
}

// ── Unit tests: deduplicateImages ────────────────────────────────────────────

func TestDeduplicateImages(t *testing.T) {
	tests := []struct {
		name  string
		input []string
		want  []string
	}{
		{
			name:  "no duplicates",
			input: []string{"a:v1", "b:v1", "c:v1"},
			want:  []string{"a:v1", "b:v1", "c:v1"},
		},
		{
			name:  "removes duplicates preserving order",
			input: []string{"a:v1", "b:v1", "a:v1", "c:v1", "b:v1"},
			want:  []string{"a:v1", "b:v1", "c:v1"},
		},
		{
			name:  "empty slice",
			input: []string{},
			want:  []string{},
		},
		{
			name:  "all duplicates",
			input: []string{"a:v1", "a:v1", "a:v1"},
			want:  []string{"a:v1"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := deduplicateImages(tt.input)
			if len(result) != len(tt.want) {
				t.Fatalf("got %d images, want %d", len(result), len(tt.want))
			}
			for i, img := range tt.want {
				if result[i] != img {
					t.Errorf("result[%d]=%q, want %q", i, result[i], img)
				}
			}
		})
	}
}

// ── Batch tests: CheckRelatedImagesMediaType ───────────────────────────────────

func TestCheckRelatedImagesMediaType_OCPVersionGTE421_SkipsCheck(t *testing.T) {
	fetcher := &mockFetcher{}
	result, err := CheckRelatedImagesMediaType(context.Background(), "4.21", []string{"quay.io/example/img:v1"}, fetcher)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Skipped {
		t.Error("expected Skipped=true for OCP >= 4.21")
	}
	if len(result.WrongMediaTypeImages) != 0 || len(result.BrokenImages) != 0 {
		t.Errorf("expected no failures for OCP >= 4.21, got WrongMediaTypeImages=%v, BrokenImages=%v", result.WrongMediaTypeImages, result.BrokenImages)
	}
}

func TestCheckRelatedImagesMediaType_OCPVersion500_SkipsCheck(t *testing.T) {
	fetcher := &mockFetcher{}
	result, err := CheckRelatedImagesMediaType(context.Background(), "5.0", []string{"quay.io/example/img:v1"}, fetcher)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Skipped {
		t.Error("expected Skipped=true for OCP >= 4.21")
	}
	if len(result.WrongMediaTypeImages) != 0 || len(result.BrokenImages) != 0 {
		t.Errorf("expected no failures for OCP >= 4.21, got WrongMediaTypeImages=%v, BrokenImages=%v", result.WrongMediaTypeImages, result.BrokenImages)
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
	if result.Skipped {
		t.Error("expected Skipped=false for OCP < 4.21")
	}
	if len(result.WrongMediaTypeImages) != 0 || len(result.BrokenImages) != 0 {
		t.Errorf("expected no failures for empty image list, got WrongMediaTypeImages=%v, BrokenImages=%v", result.WrongMediaTypeImages, result.BrokenImages)
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
	if len(result.WrongMediaTypeImages) == 0 && len(result.BrokenImages) == 0 {
		t.Error("expected failures, got none")
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
	if len(result.WrongMediaTypeImages) != 0 || len(result.BrokenImages) != 0 {
		t.Errorf("expected no failures, got WrongMediaTypeImages=%v, BrokenImages=%v", result.WrongMediaTypeImages, result.BrokenImages)
	}
	if countingFetcher.count.Load() != 1 {
		t.Errorf("got %d fetch calls, want 1 (duplicates should be deduplicated)", countingFetcher.count.Load())
	}
}

// ── Batch tests: retry behavior ────────────────────────────────────────────────

func TestCheckRelatedImagesMediaType_RetryRecoversBrokenImage(t *testing.T) {
	fetcher := &sequenceFetcher{
		calls: make(map[string]int),
		responses: map[string][]fetchResponse{
			"quay.io/example/flaky:v1": {
				{err: fmt.Errorf("transient network error")},
				{desc: makeDescriptor(types.DockerManifestSchema2)},
			},
		},
	}
	result, err := CheckRelatedImagesMediaType(context.Background(), "4.20", []string{"quay.io/example/flaky:v1"}, fetcher)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.WrongMediaTypeImages) != 0 || len(result.BrokenImages) != 0 {
		t.Errorf("expected no failures after retry recovery, got WrongMediaTypeImages=%v, BrokenImages=%v", result.WrongMediaTypeImages, result.BrokenImages)
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
	if len(result.BrokenImages) == 0 {
		t.Error("expected BrokenImages after persistent failure, got none")
	}
	if len(result.BrokenImages) != 1 || result.BrokenImages[0] != "quay.io/example/down:v1" {
		t.Errorf("got BrokenImages=%v, want [quay.io/example/down:v1]", result.BrokenImages)
	}
}

func TestCheckRelatedImagesMediaType_RetryBrokenBecomesWrongMediaType(t *testing.T) {
	fetcher := &sequenceFetcher{
		calls: make(map[string]int),
		responses: map[string][]fetchResponse{
			"quay.io/example/flaky-oci:v1": {
				{err: fmt.Errorf("transient error")},
				{desc: makeDescriptor(types.OCIManifestSchema1)},
			},
		},
	}
	result, err := CheckRelatedImagesMediaType(context.Background(), "4.20", []string{"quay.io/example/flaky-oci:v1"}, fetcher)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.WrongMediaTypeImages) == 0 {
		t.Error("expected WrongMediaTypeImages after retry reveals OCI mediaType, got none")
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
	if len(result.WrongMediaTypeImages) == 0 {
		t.Error("expected WrongMediaTypeImages for OCI images, got none")
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

func TestCheckRelatedImagesMediaType_CancelledDuringRetry_ReturnsError(t *testing.T) {
	ctx, cancelCtx := context.WithCancel(context.Background())
	fetcher := &sequenceFetcher{
		calls: make(map[string]int),
		responses: map[string][]fetchResponse{
			"quay.io/example/img:v1": {
				{err: fmt.Errorf("transient network error")},
				{err: fmt.Errorf("context cancelled during retry"), fn: cancelCtx},
			},
		},
	}
	_, err := CheckRelatedImagesMediaType(ctx, "4.20", []string{"quay.io/example/img:v1"}, fetcher)
	if err == nil {
		t.Fatal("expected error for context cancelled during retry pass, got nil — should not return false BrokenImages")
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
	if len(result.WrongMediaTypeImages) != 0 || len(result.BrokenImages) != 0 {
		t.Errorf("expected no failures for empty manifest list, got WrongMediaTypeImages=%v, BrokenImages=%v", result.WrongMediaTypeImages, result.BrokenImages)
	}
}
