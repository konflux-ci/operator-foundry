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
	"strings"
	"testing"
)

// writeGetPackagesDockerfile writes a minimal Dockerfile into dir and returns
// its path. The Dockerfile is just enough for lifecycle.GetPackages to parse.
func writeGetPackagesDockerfile(t *testing.T, dir, content string) string {
	t.Helper()
	path := filepath.Join(dir, "Dockerfile")
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("failed to write test Dockerfile: %v", err)
	}
	return path
}

// ── --all-filtered-marker tests ─────────────────────────────────────────────

func TestGetPackagesCmd_AllFilteredMarker_AllFiltered_MarkerCreated(t *testing.T) {
	// When --skip-packages removes ALL discovered packages, the marker file
	// must be created (0-byte).
	base := t.TempDir()
	if err := os.MkdirAll(filepath.Join(base, "catalog", "my-operator"), 0755); err != nil {
		t.Fatalf("failed to create package dir: %v", err)
	}

	dockerfilePath := writeGetPackagesDockerfile(t, base, `FROM ubuntu
COPY catalog /configs
`)

	markerDir := t.TempDir()
	markerPath := filepath.Join(markerDir, "all-filtered")

	cmd := newGetPackagesCmd()
	cmd.SetArgs([]string{
		"--dockerfile", dockerfilePath,
		"--build-context", base,
		"--skip-packages", "my-operator",
		"--all-filtered-marker", markerPath,
	})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	info, err := os.Stat(markerPath)
	if err != nil {
		t.Fatal("marker file should exist when all packages are filtered")
	}
	if info.Size() != 0 {
		t.Errorf("marker file should be 0-byte, got %d bytes", info.Size())
	}
}

func TestGetPackagesCmd_AllFilteredMarker_ZeroPackages_NoMarker(t *testing.T) {
	// When GetPackages itself returns 0 packages (error), the command should
	// error and the marker file must NOT be created.
	base := t.TempDir()
	// Create a catalog dir with no subdirectories — GetPackages returns an error.
	if err := os.MkdirAll(filepath.Join(base, "catalog"), 0755); err != nil {
		t.Fatalf("failed to create catalog dir: %v", err)
	}

	dockerfilePath := writeGetPackagesDockerfile(t, base, `FROM ubuntu
COPY catalog /configs
`)

	markerDir := t.TempDir()
	markerPath := filepath.Join(markerDir, "all-filtered")

	cmd := newGetPackagesCmd()
	cmd.SetArgs([]string{
		"--dockerfile", dockerfilePath,
		"--build-context", base,
		"--skip-packages", "anything",
		"--all-filtered-marker", markerPath,
	})
	// Silence cobra's error/usage output during tests.
	cmd.SilenceErrors = true
	cmd.SilenceUsage = true

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error when GetPackages finds no packages, got nil")
	}

	if _, statErr := os.Stat(markerPath); statErr == nil {
		t.Fatal("marker file should NOT exist when GetPackages returns 0 packages")
	}
}

func TestGetPackagesCmd_AllFilteredMarker_SomeSurvive_NoMarker(t *testing.T) {
	// When some packages survive filtering, the marker file must NOT be created.
	base := t.TempDir()
	for _, pkg := range []string{"operator-a", "operator-b"} {
		if err := os.MkdirAll(filepath.Join(base, "catalog", pkg), 0755); err != nil {
			t.Fatalf("failed to create package dir: %v", err)
		}
	}

	dockerfilePath := writeGetPackagesDockerfile(t, base, `FROM ubuntu
COPY catalog /configs
`)

	markerDir := t.TempDir()
	markerPath := filepath.Join(markerDir, "all-filtered")

	cmd := newGetPackagesCmd()
	cmd.SetArgs([]string{
		"--dockerfile", dockerfilePath,
		"--build-context", base,
		"--skip-packages", "operator-a",
		"--all-filtered-marker", markerPath,
	})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if _, statErr := os.Stat(markerPath); statErr == nil {
		t.Fatal("marker file should NOT exist when some packages survive filtering")
	}
}

// ── --all-filtered-marker requires --skip-packages ──────────────────────────

func TestGetPackagesCmd_AllFilteredMarker_WithoutSkipPackages_ReturnsError(t *testing.T) {
	// --all-filtered-marker without --skip-packages must return an error.
	base := t.TempDir()
	if err := os.MkdirAll(filepath.Join(base, "catalog", "my-operator"), 0755); err != nil {
		t.Fatalf("failed to create package dir: %v", err)
	}

	dockerfilePath := writeGetPackagesDockerfile(t, base, `FROM ubuntu
COPY catalog /configs
`)

	markerDir := t.TempDir()
	markerPath := filepath.Join(markerDir, "all-filtered")

	cmd := newGetPackagesCmd()
	cmd.SetArgs([]string{
		"--dockerfile", dockerfilePath,
		"--build-context", base,
		"--all-filtered-marker", markerPath,
	})
	cmd.SilenceErrors = true
	cmd.SilenceUsage = true

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error when --all-filtered-marker is set without --skip-packages")
	}
	if !strings.Contains(err.Error(), "--all-filtered-marker requires --skip-packages") {
		t.Errorf("unexpected error message: %v", err)
	}
}
