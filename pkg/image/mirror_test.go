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
	"errors"
	"os"
	"path/filepath"
	"slices"
	"testing"
)

// Unit tests for LoadMirrorConfigFromFile.
// They exercise pure local I/O + YAML parse + validation against temp files;
// no registry, network, or cluster access.
//
// Covered behavior: load ImageDigestMirrorSet YAML into source -> []mirrors,
// requiring YAML string scalars, ignoring well-formed trailing documents,
// rejecting malformed trailing YAML, and validating sources/mirrors via
// go-containerregistry (repository path or registry-only host).

// writeMirrorFile is a test helper that writes YAML into a unique temp path.
func writeMirrorFile(t *testing.T, contents string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "images-mirror-set.yaml")
	if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
		t.Fatalf("write mirror file: %v", err)
	}
	return path
}

// Unit test for the happy path: a multi-entry ImageDigestMirrorSet becomes a
// map with one key per source and the expected mirror lists (including
// multiple mirrors under a single source).
func TestLoadMirrorConfigFromFile_Success(t *testing.T) {
	path := writeMirrorFile(t, `
apiVersion: config.openshift.io/v1
kind: ImageDigestMirrorSet
metadata:
  name: example
spec:
  imageDigestMirrors:
  - source: registry.redhat.io/container-native-virtualization
    mirrors:
    - brew.registry.redhat.io/container-native-virtualization
    - quay.io/openshift-virtualization/konflux-builds/v4-19
  - source: registry.redhat.io/rhacm2
    mirrors:
    - registry.stage.redhat.io/rhacm2
`)

	got, err := LoadMirrorConfigFromFile(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	want := MirrorConfigMap{
		"registry.redhat.io/container-native-virtualization": {
			"brew.registry.redhat.io/container-native-virtualization",
			"quay.io/openshift-virtualization/konflux-builds/v4-19",
		},
		"registry.redhat.io/rhacm2": {
			"registry.stage.redhat.io/rhacm2",
		},
	}
	if len(got) != len(want) {
		t.Fatalf("got %d sources, want %d: %v", len(got), len(want), got)
	}
	for source, wantMirrors := range want {
		if !slices.Equal(got[source], wantMirrors) {
			t.Errorf("source %q mirrors=%v, want %v", source, got[source], wantMirrors)
		}
	}
}

// Unit test for registry-only IDMS sources/mirrors (no repository path).
func TestLoadMirrorConfigFromFile_RegistryOnly(t *testing.T) {
	path := writeMirrorFile(t, `
spec:
  imageDigestMirrors:
  - source: registry.redhat.io
    mirrors:
    - brew.registry.redhat.io
    - mirror.example.com:5000
`)

	got, err := LoadMirrorConfigFromFile(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := []string{"brew.registry.redhat.io", "mirror.example.com:5000"}
	if !slices.Equal(got["registry.redhat.io"], want) {
		t.Errorf("got mirrors=%v, want %v", got["registry.redhat.io"], want)
	}
}

// Unit test for empty mirror lists: omitted mirrors and explicit mirrors: []
// both store a non-nil empty []string.
func TestLoadMirrorConfigFromFile_EmptyMirrors(t *testing.T) {
	tests := []struct {
		name string
		yaml string
	}{
		{
			name: "omitted mirrors",
			yaml: `
spec:
  imageDigestMirrors:
  - source: registry.redhat.io/foo
`,
		},
		{
			name: "explicit empty mirrors list",
			yaml: `
spec:
  imageDigestMirrors:
  - source: registry.redhat.io/foo
    mirrors: []
`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := LoadMirrorConfigFromFile(writeMirrorFile(t, tt.yaml))
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			mirrors, ok := got["registry.redhat.io/foo"]
			if !ok {
				t.Fatal("expected source registry.redhat.io/foo in map")
			}
			if mirrors == nil {
				t.Fatal("expected non-nil empty mirrors slice")
			}
			if len(mirrors) != 0 {
				t.Errorf("got mirrors=%v, want empty", mirrors)
			}
		})
	}
}

// Unit test for empty imageDigestMirrors: returns an empty map, not an error.
func TestLoadMirrorConfigFromFile_EmptyImageDigestMirrors(t *testing.T) {
	path := writeMirrorFile(t, `
spec:
  imageDigestMirrors: []
`)

	got, err := LoadMirrorConfigFromFile(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got == nil {
		t.Fatal("expected non-nil empty map")
	}
	if len(got) != 0 {
		t.Errorf("got %v, want empty map", got)
	}
}

// Unit test: trailing YAML after the IDMS (bash yq '.' parity): bare "---"
// and an extra valid document are ignored; the first document still loads.
func TestLoadMirrorConfigFromFile_TrailingYAMLIgnored(t *testing.T) {
	tests := []struct {
		name string
		yaml string
	}{
		{
			name: "bare document separator",
			yaml: `
spec:
  imageDigestMirrors:
  - source: registry.redhat.io/foo
    mirrors:
    - mirror.example.com/foo
---
`,
		},
		{
			name: "extra valid document",
			yaml: `
spec:
  imageDigestMirrors:
  - source: registry.redhat.io/foo
    mirrors:
    - mirror.example.com/foo
---
apiVersion: v1
kind: ConfigMap
`,
		},
	}

	want := []string{"mirror.example.com/foo"}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := LoadMirrorConfigFromFile(writeMirrorFile(t, tt.yaml))
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !slices.Equal(got["registry.redhat.io/foo"], want) {
				t.Errorf("got mirrors=%v, want %v", got["registry.redhat.io/foo"], want)
			}
		})
	}
}

// Unit test for duplicate source keys: like bash jq reduce, the last entry
// overwrites earlier mirrors for the same source.
func TestLoadMirrorConfigFromFile_DuplicateSourceLastWins(t *testing.T) {
	path := writeMirrorFile(t, `
spec:
  imageDigestMirrors:
  - source: registry.redhat.io/foo
    mirrors:
    - mirror.example.com/first
  - source: registry.redhat.io/foo
    mirrors:
    - mirror.example.com/second
`)

	got, err := LoadMirrorConfigFromFile(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := []string{"mirror.example.com/second"}
	if !slices.Equal(got["registry.redhat.io/foo"], want) {
		t.Errorf("got mirrors=%v, want %v", got["registry.redhat.io/foo"], want)
	}
}

// Unit test table for failure paths: missing file, invalid YAML, non-string
// scalars, malformed trailing YAML, and go-containerregistry rejections for
// empty or illegal source/mirror strings.
func TestLoadMirrorConfigFromFile_Errors(t *testing.T) {
	tests := []struct {
		name    string
		path    func(t *testing.T) string
		wantErr error
	}{
		{
			name: "missing file",
			path: func(t *testing.T) string {
				return filepath.Join(t.TempDir(), "does-not-exist.yaml")
			},
			wantErr: ErrMirrorConfigReadFailed,
		},
		{
			name: "invalid yaml",
			path: func(t *testing.T) string {
				return writeMirrorFile(t, "{{not valid yaml")
			},
			wantErr: ErrMirrorConfigParseFailed,
		},
		{
			name: "numeric source",
			path: func(t *testing.T) string {
				return writeMirrorFile(t, `
spec:
  imageDigestMirrors:
  - source: 123
    mirrors:
    - mirror.example.com/foo
`)
			},
			wantErr: ErrMirrorConfigParseFailed,
		},
		{
			name: "numeric mirror",
			path: func(t *testing.T) string {
				return writeMirrorFile(t, `
spec:
  imageDigestMirrors:
  - source: registry.redhat.io/foo
    mirrors:
    - 456
`)
			},
			wantErr: ErrMirrorConfigParseFailed,
		},
		{
			name: "decimal source",
			path: func(t *testing.T) string {
				return writeMirrorFile(t, `
spec:
  imageDigestMirrors:
  - source: 1.23
    mirrors:
    - mirror.example.com/foo
`)
			},
			wantErr: ErrMirrorConfigParseFailed,
		},
		{
			name: "decimal mirror",
			path: func(t *testing.T) string {
				return writeMirrorFile(t, `
spec:
  imageDigestMirrors:
  - source: registry.redhat.io/foo
    mirrors:
    - 4.56
`)
			},
			wantErr: ErrMirrorConfigParseFailed,
		},
		{
			name: "boolean source",
			path: func(t *testing.T) string {
				return writeMirrorFile(t, `
spec:
  imageDigestMirrors:
  - source: true
    mirrors:
    - mirror.example.com/foo
`)
			},
			wantErr: ErrMirrorConfigParseFailed,
		},
		{
			name: "boolean mirror",
			path: func(t *testing.T) string {
				return writeMirrorFile(t, `
spec:
  imageDigestMirrors:
  - source: registry.redhat.io/foo
    mirrors:
    - false
`)
			},
			wantErr: ErrMirrorConfigParseFailed,
		},
		{
			name: "trailing malformed yaml document",
			path: func(t *testing.T) string {
				return writeMirrorFile(t, `
spec:
  imageDigestMirrors:
  - source: registry.redhat.io/foo
    mirrors:
    - mirror.example.com/foo
---
this: is: broken: {{
`)
			},
			wantErr: ErrMirrorConfigParseFailed,
		},
		{
			name: "empty source",
			path: func(t *testing.T) string {
				return writeMirrorFile(t, `
spec:
  imageDigestMirrors:
  - source: ""
    mirrors:
    - mirror.example.com/foo
`)
			},
			wantErr: ErrInvalidMirrorSource,
		},
		{
			name: "invalid source characters",
			path: func(t *testing.T) string {
				return writeMirrorFile(t, `
spec:
  imageDigestMirrors:
  - source: "registry.redhat.io/foo bar"
    mirrors:
    - mirror.example.com/foo
`)
			},
			wantErr: ErrInvalidMirrorSource,
		},
		{
			name: "unqualified source repository",
			path: func(t *testing.T) string {
				return writeMirrorFile(t, `
spec:
  imageDigestMirrors:
  - source: org/repo
    mirrors:
    - mirror.example.com/foo
`)
			},
			wantErr: ErrInvalidMirrorSource,
		},
		{
			name: "unqualified registry-only source",
			path: func(t *testing.T) string {
				return writeMirrorFile(t, `
spec:
  imageDigestMirrors:
  - source: ubuntu
    mirrors:
    - mirror.example.com
`)
			},
			wantErr: ErrInvalidMirrorSource,
		},
		{
			name: "empty registry host in source",
			path: func(t *testing.T) string {
				return writeMirrorFile(t, `
spec:
  imageDigestMirrors:
  - source: ":5000/foo"
    mirrors:
    - mirror.example.com/foo
`)
			},
			wantErr: ErrInvalidMirrorSource,
		},
		{
			name: "empty path component in source",
			path: func(t *testing.T) string {
				return writeMirrorFile(t, `
spec:
  imageDigestMirrors:
  - source: quay.io/foo//bar
    mirrors:
    - mirror.example.com/foo
`)
			},
			wantErr: ErrInvalidMirrorSource,
		},
		{
			name: "empty registry host in mirror",
			path: func(t *testing.T) string {
				return writeMirrorFile(t, `
spec:
  imageDigestMirrors:
  - source: registry.redhat.io/foo
    mirrors:
    - ":5000/foo"
`)
			},
			wantErr: ErrInvalidMirror,
		},
		{
			name: "empty path component in mirror",
			path: func(t *testing.T) string {
				return writeMirrorFile(t, `
spec:
  imageDigestMirrors:
  - source: registry.redhat.io/foo
    mirrors:
    - quay.io/foo//bar
`)
			},
			wantErr: ErrInvalidMirror,
		},
		{
			name: "empty mirror",
			path: func(t *testing.T) string {
				return writeMirrorFile(t, `
spec:
  imageDigestMirrors:
  - source: registry.redhat.io/foo
    mirrors:
    - ""
`)
			},
			wantErr: ErrInvalidMirror,
		},
		{
			name: "invalid mirror characters",
			path: func(t *testing.T) string {
				return writeMirrorFile(t, `
spec:
  imageDigestMirrors:
  - source: registry.redhat.io/foo
    mirrors:
    - "mirror.example.com/foo bar"
`)
			},
			wantErr: ErrInvalidMirror,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := LoadMirrorConfigFromFile(tt.path(t))
			if err == nil {
				t.Fatal("expected error, got nil")
			}
			if !errors.Is(err, tt.wantErr) {
				t.Errorf("expected error to wrap %v, got: %v", tt.wantErr, err)
			}
		})
	}
}
