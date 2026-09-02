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

import "testing"

// validDigest is a syntactically valid SHA-256 digest used across test cases.
const validDigest = "sha256:abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789"

// ── ParseImageURL ──────────────────────────────────────────────

func TestParseImageURL_Success(t *testing.T) {
	cases := []struct {
		name     string
		input    string
		registry string
		tag      string
		digest   string
	}{
		{
			name:     "registry and repository only",
			input:    "registry.io/repo",
			registry: "registry.io/repo",
			tag:      "",
			digest:   "",
		},
		{
			name:     "repository with tag",
			input:    "registry.io/repo:v1",
			registry: "registry.io/repo",
			tag:      "v1",
			digest:   "",
		},
		{
			name:     "registry with port, no tag",
			input:    "registry.io:5000/repo",
			registry: "registry.io:5000/repo",
			tag:      "",
			digest:   "",
		},
		{
			name:     "registry with port and tag",
			input:    "registry.io:5000/repo:v1",
			registry: "registry.io:5000/repo",
			tag:      "v1",
			digest:   "",
		},
		{
			name:     "repository with digest",
			input:    "registry.io/repo@" + validDigest,
			registry: "registry.io/repo",
			tag:      "",
			digest:   validDigest,
		},
		{
			name:     "registry with port and digest",
			input:    "registry.io:5000/repo@" + validDigest,
			registry: "registry.io:5000/repo",
			tag:      "",
			digest:   validDigest,
		},
		{
			name:     "registry with port, tag and digest",
			input:    "registry.io:5000/repo:v1@" + validDigest,
			registry: "registry.io:5000/repo",
			tag:      "v1",
			digest:   validDigest,
		},
		{
			name:     "nested repository path with port and tag",
			input:    "registry.io:5000/org/repo:v1",
			registry: "registry.io:5000/org/repo",
			tag:      "v1",
			digest:   "",
		},
		{
			// Unqualified names are normalized by distribution/reference:
			// "ubuntu" becomes "docker.io/library/ubuntu".
			name:     "unqualified name is normalized",
			input:    "ubuntu",
			registry: "docker.io/library/ubuntu",
			tag:      "",
			digest:   "",
		},
		{
			name:     "unqualified name with tag is normalized",
			input:    "nginx:latest",
			registry: "docker.io/library/nginx",
			tag:      "latest",
			digest:   "",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ParseImageURL(tc.input)
			if err != nil {
				t.Fatalf("ParseImageURL(%q) unexpected error: %v", tc.input, err)
			}
			if got.RegistryRepository != tc.registry {
				t.Errorf("RegistryRepository = %q, want %q", got.RegistryRepository, tc.registry)
			}
			if got.Tag != tc.tag {
				t.Errorf("Tag = %q, want %q", got.Tag, tc.tag)
			}
			if got.Digest != tc.digest {
				t.Errorf("Digest = %q, want %q", got.Digest, tc.digest)
			}
		})
	}
}

func TestParseImageURL_EmptyInput_ReturnsError(t *testing.T) {
	if _, err := ParseImageURL(""); err == nil {
		t.Fatal("expected error for empty image url, got nil")
	}
}

func TestParseImageURL_InvalidReference_ReturnsError(t *testing.T) {
	cases := []struct {
		name  string
		input string
	}{
		{name: "invalid digest format", input: "registry.io/repo@notadigest"},
		{name: "uppercase letters in name", input: "Registry.IO/Repo:v1"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := ParseImageURL(tc.input); err == nil {
				t.Fatalf("expected error for %q, got nil", tc.input)
			}
		})
	}
}

// ── GetImageRegistryAndRepository ──────────────────────────────

func TestGetImageRegistryAndRepository_InvalidInput_ReturnsError(t *testing.T) {
	if _, err := GetImageRegistryAndRepository(""); err == nil {
		t.Fatal("expected error for empty image url, got nil")
	}
}

func TestGetImageRegistryAndRepository(t *testing.T) {
	cases := []struct {
		input string
		want  string
	}{
		{"registry.io/repo", "registry.io/repo"},
		{"registry.io/repo:v1", "registry.io/repo"},
		{"registry.io:5000/repo:v1@" + validDigest, "registry.io:5000/repo"},
	}
	for _, tc := range cases {
		got, err := GetImageRegistryAndRepository(tc.input)
		if err != nil {
			t.Fatalf("GetImageRegistryAndRepository(%q) unexpected error: %v", tc.input, err)
		}
		if got != tc.want {
			t.Errorf("GetImageRegistryAndRepository(%q) = %q, want %q", tc.input, got, tc.want)
		}
	}
}

// ── GetImageRegistryRepositoryTag ──────────────────────────────

func TestGetImageRegistryRepositoryTag_InvalidInput_ReturnsError(t *testing.T) {
	if _, err := GetImageRegistryRepositoryTag(""); err == nil {
		t.Fatal("expected error for empty image url, got nil")
	}
}

func TestGetImageRegistryRepositoryTag(t *testing.T) {
	cases := []struct {
		input string
		want  string
	}{
		{"registry.io/repo", "registry.io/repo"},
		{"registry.io/repo:v1", "registry.io/repo:v1"},
		{"registry.io:5000/repo:v1@" + validDigest, "registry.io:5000/repo:v1"},
		{"registry.io:5000/repo@" + validDigest, "registry.io:5000/repo"},
	}
	for _, tc := range cases {
		got, err := GetImageRegistryRepositoryTag(tc.input)
		if err != nil {
			t.Fatalf("GetImageRegistryRepositoryTag(%q) unexpected error: %v", tc.input, err)
		}
		if got != tc.want {
			t.Errorf("GetImageRegistryRepositoryTag(%q) = %q, want %q", tc.input, got, tc.want)
		}
	}
}

// ── GetImageRegistryRepositoryDigest ───────────────────────────

func TestGetImageRegistryRepositoryDigest_InvalidInput_ReturnsError(t *testing.T) {
	if _, err := GetImageRegistryRepositoryDigest(""); err == nil {
		t.Fatal("expected error for empty image url, got nil")
	}
}

func TestGetImageRegistryRepositoryDigest(t *testing.T) {
	cases := []struct {
		input string
		want  string
	}{
		{"registry.io/repo", "registry.io/repo"},
		{"registry.io/repo@" + validDigest, "registry.io/repo@" + validDigest},
		{"registry.io:5000/repo:v1@" + validDigest, "registry.io:5000/repo@" + validDigest},
		{"registry.io/repo:v1", "registry.io/repo"},
	}
	for _, tc := range cases {
		got, err := GetImageRegistryRepositoryDigest(tc.input)
		if err != nil {
			t.Fatalf("GetImageRegistryRepositoryDigest(%q) unexpected error: %v", tc.input, err)
		}
		if got != tc.want {
			t.Errorf("GetImageRegistryRepositoryDigest(%q) = %q, want %q", tc.input, got, tc.want)
		}
	}
}
