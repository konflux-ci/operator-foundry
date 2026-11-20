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
	"fmt"
	"strings"
	"testing"

	"github.com/google/go-containerregistry/pkg/v1/remote"
	"github.com/google/go-containerregistry/pkg/v1/types"
	"github.com/konflux-ci/operator-foundry/pkg/image"
)

type stubFetcher struct {
	calls    []string
	failRefs map[string]bool
}

func (s *stubFetcher) FetchManifest(_ context.Context, imageRef string) (*remote.Descriptor, error) {
	s.calls = append(s.calls, imageRef)
	if s.failRefs[imageRef] {
		return nil, fmt.Errorf("fetch failed for %s", imageRef)
	}
	return makeDescriptor(types.DockerManifestSchema2), nil
}

func TestMirroringFetcher_OriginalSucceeds_NoMirrorAttempt(t *testing.T) {
	inner := &stubFetcher{failRefs: map[string]bool{}}
	fetcher := &MirroringFetcher{
		Inner: inner,
		Mirrors: image.MirrorConfigMap{
			"registry.redhat.io/foo/bar": {"quay.io/mirror/bar"},
		},
	}

	desc, err := fetcher.FetchManifest(context.Background(), "registry.redhat.io/foo/bar:v1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if desc == nil {
		t.Fatal("expected descriptor")
	}
	if len(inner.calls) != 1 || inner.calls[0] != "registry.redhat.io/foo/bar:v1" {
		t.Fatalf("calls = %v, want only original", inner.calls)
	}
}

func TestMirroringFetcher_OriginalFails_TriesMirrorsInOrder(t *testing.T) {
	original := "registry.redhat.io/foo/bar:v1"
	mirror1 := "quay.io/mirror/bar:v1"
	mirror2 := "brew.registry.redhat.io/foo/bar:v1"

	inner := &stubFetcher{
		failRefs: map[string]bool{
			original: true,
			mirror1:  true,
		},
	}
	fetcher := &MirroringFetcher{
		Inner: inner,
		Mirrors: image.MirrorConfigMap{
			"registry.redhat.io/foo/bar": {
				"quay.io/mirror/bar",
				"brew.registry.redhat.io/foo/bar",
			},
		},
	}

	desc, err := fetcher.FetchManifest(context.Background(), original)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if desc == nil {
		t.Fatal("expected descriptor from second mirror")
	}
	wantCalls := []string{original, mirror1, mirror2}
	if len(inner.calls) != len(wantCalls) {
		t.Fatalf("calls = %v, want %v", inner.calls, wantCalls)
	}
	for i, c := range wantCalls {
		if inner.calls[i] != c {
			t.Errorf("call[%d] = %q, want %q", i, inner.calls[i], c)
		}
	}
}

func TestMirroringFetcher_OriginalFails_NoMirrors_ReturnsOriginalError(t *testing.T) {
	original := "registry.redhat.io/other/image:v1"
	inner := &stubFetcher{failRefs: map[string]bool{original: true}}
	fetcher := &MirroringFetcher{
		Inner: inner,
		Mirrors: image.MirrorConfigMap{
			"registry.redhat.io/foo/bar": {"quay.io/mirror/bar"},
		},
	}

	_, err := fetcher.FetchManifest(context.Background(), original)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if len(inner.calls) != 1 {
		t.Fatalf("calls = %v, want only original", inner.calls)
	}
}

func TestMirroringFetcher_AllMirrorsFail_ReturnsLastError(t *testing.T) {
	original := "registry.redhat.io/foo/bar:v1"
	mirror1 := "quay.io/mirror/bar:v1"
	inner := &stubFetcher{
		failRefs: map[string]bool{
			original: true,
			mirror1:  true,
		},
	}
	fetcher := &MirroringFetcher{
		Inner: inner,
		Mirrors: image.MirrorConfigMap{
			"registry.redhat.io/foo/bar": {"quay.io/mirror/bar"},
		},
	}

	_, err := fetcher.FetchManifest(context.Background(), original)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), mirror1) {
		t.Errorf("error %q should mention last mirror attempt", err)
	}
	if len(inner.calls) != 2 {
		t.Fatalf("calls = %v, want original + one mirror", inner.calls)
	}
}

func TestMirroringFetcher_NilMirrorMap_Passthrough(t *testing.T) {
	original := "registry.redhat.io/foo/bar:v1"
	inner := &stubFetcher{failRefs: map[string]bool{original: true}}
	fetcher := &MirroringFetcher{Inner: inner, Mirrors: nil}

	_, err := fetcher.FetchManifest(context.Background(), original)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if len(inner.calls) != 1 {
		t.Fatalf("calls = %v, want only original", inner.calls)
	}
}

func TestMirroringFetcher_NilInner_ReturnsError(t *testing.T) {
	fetcher := &MirroringFetcher{Inner: nil}
	_, err := fetcher.FetchManifest(context.Background(), "registry.redhat.io/foo/bar:v1")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}
