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

package fbc

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/konflux-ci/operator-foundry/pkg/mediatype"
)

func TestLoadRelatedImages_ValidList(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "related-images.json")
	content := `["quay.io/example/a:v1","quay.io/example/b:v2","quay.io/example/c@sha256:abc123"]`
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("failed to write test file: %v", err)
	}

	images, err := loadRelatedImages(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(images) != 3 {
		t.Fatalf("got %d images, want 3", len(images))
	}
	expected := []string{
		"quay.io/example/a:v1",
		"quay.io/example/b:v2",
		"quay.io/example/c@sha256:abc123",
	}
	for i, img := range images {
		if img != expected[i] {
			t.Errorf("image[%d]: got %q, want %q", i, img, expected[i])
		}
	}
}

func TestLoadRelatedImages_EmptyList(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "related-images.json")
	if err := os.WriteFile(path, []byte("[]"), 0644); err != nil {
		t.Fatalf("failed to write test file: %v", err)
	}

	images, err := loadRelatedImages(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(images) != 0 {
		t.Errorf("got %d images, want 0", len(images))
	}
}

func TestLoadRelatedImages_InvalidJSON_ReturnsError(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "related-images.json")
	if err := os.WriteFile(path, []byte("not json"), 0644); err != nil {
		t.Fatalf("failed to write test file: %v", err)
	}

	_, err := loadRelatedImages(path)
	if err == nil {
		t.Fatal("expected error for invalid JSON, got nil")
	}
}

func TestLoadRelatedImages_NullJSON_ReturnsError(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "related-images.json")
	if err := os.WriteFile(path, []byte("null"), 0644); err != nil {
		t.Fatalf("failed to write test file: %v", err)
	}

	_, err := loadRelatedImages(path)
	if err == nil {
		t.Fatal("expected error for null JSON, got nil")
	}
}

func TestLoadRelatedImages_NonexistentFile_ReturnsError(t *testing.T) {
	_, err := loadRelatedImages("/nonexistent/related-images.json")
	if err == nil {
		t.Fatal("expected error for nonexistent file, got nil")
	}
}

func TestNewMediaTypeFetcher_EmptyPath_ReturnsRegistryFetcher(t *testing.T) {
	fetcher, err := newMediaTypeFetcher("")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, ok := fetcher.(*mediatype.RegistryFetcher); !ok {
		t.Fatalf("got %T, want *mediatype.RegistryFetcher", fetcher)
	}
}

func TestNewMediaTypeFetcher_MissingFile_ReturnsRegistryFetcher(t *testing.T) {
	fetcher, err := newMediaTypeFetcher(filepath.Join(t.TempDir(), "missing-idms.yaml"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, ok := fetcher.(*mediatype.RegistryFetcher); !ok {
		t.Fatalf("got %T, want *mediatype.RegistryFetcher", fetcher)
	}
}

func TestNewMediaTypeFetcher_ValidFile_ReturnsMirroringFetcher(t *testing.T) {
	path := filepath.Join(t.TempDir(), "images-mirror-set.yaml")
	content := `
spec:
  imageDigestMirrors:
  - source: registry.redhat.io/foo
    mirrors:
    - quay.io/mirror/foo
`
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("write mirror set: %v", err)
	}

	fetcher, err := newMediaTypeFetcher(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, ok := fetcher.(*mediatype.MirroringFetcher); !ok {
		t.Fatalf("got %T, want *mediatype.MirroringFetcher", fetcher)
	}
}

func TestNewMediaTypeFetcher_InvalidFile_ReturnsError(t *testing.T) {
	path := filepath.Join(t.TempDir(), "bad.yaml")
	if err := os.WriteFile(path, []byte("{{not valid"), 0644); err != nil {
		t.Fatalf("write mirror set: %v", err)
	}

	_, err := newMediaTypeFetcher(path)
	if err == nil {
		t.Fatal("expected error for invalid mirror set, got nil")
	}
}
