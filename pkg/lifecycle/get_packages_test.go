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

package lifecycle

import (
	"os"
	"path/filepath"
	"testing"
)

func TestGetPackages_ReturnsPackages_WhenOCPVersionGTE5(t *testing.T) {
	base := t.TempDir()

	if err := os.MkdirAll(filepath.Join(base, "catalog", "my-operator"), 0755); err != nil {
		t.Fatalf("failed to create package dir: %v", err)
	}

	dockerfilePath := writeTestDockerfile(t, base, `FROM ubuntu
LABEL com.redhat.fbc.openshift.version=["5.0"]
COPY catalog /configs
`)

	packages, err := GetPackages(dockerfilePath, base, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(packages) != 1 || packages[0] != "my-operator" {
		t.Errorf("got %v, want [my-operator]", packages)
	}
}

func TestGetPackages_MultiplePackages(t *testing.T) {
	base := t.TempDir()

	for _, pkg := range []string{"operator-a", "operator-b"} {
		if err := os.MkdirAll(filepath.Join(base, "catalog", pkg), 0755); err != nil {
			t.Fatalf("failed to create package dir: %v", err)
		}
	}

	dockerfilePath := writeTestDockerfile(t, base, `FROM ubuntu
LABEL com.redhat.fbc.openshift.version=["5.0"]
COPY catalog /configs
`)

	packages, err := GetPackages(dockerfilePath, base, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(packages) != 2 {
		t.Fatalf("got %d packages, want 2: %v", len(packages), packages)
	}

	pkgSet := map[string]bool{"operator-a": false, "operator-b": false}
	for _, p := range packages {
		if _, ok := pkgSet[p]; ok {
			pkgSet[p] = true
		}
	}
	for name, found := range pkgSet {
		if !found {
			t.Errorf("expected package %q in result, got %v", name, packages)
		}
	}
}

func TestGetPackages_DockerfileInSubdirectory_ResolvesRelativeToBuildContext(t *testing.T) {
	base := t.TempDir()
	ctx := filepath.Join(base, "v5.0")

	if err := os.MkdirAll(filepath.Join(ctx, "catalog", "my-operator"), 0755); err != nil {
		t.Fatalf("failed to create package dir: %v", err)
	}

	dockerfileContent := []byte(`FROM ubuntu
LABEL com.redhat.fbc.openshift.version=["5.0"]
COPY catalog /configs
`)
	if err := os.WriteFile(filepath.Join(ctx, "catalog.Dockerfile"), dockerfileContent, 0644); err != nil {
		t.Fatalf("failed to write dockerfile: %v", err)
	}

	// dockerfile path is relative to the build context, not the current directory.
	packages, err := GetPackages("catalog.Dockerfile", ctx, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(packages) != 1 || packages[0] != "my-operator" {
		t.Errorf("got %v, want [my-operator]", packages)
	}
}

func TestGetPackages_InvalidDockerfile_ReturnsError(t *testing.T) {
	_, err := GetPackages("/nonexistent/Dockerfile", t.TempDir(), nil)
	if err == nil {
		t.Fatal("expected error for nonexistent Dockerfile, got nil")
	}
}

func TestGetPackages_BuildArgResolvesSourcePath(t *testing.T) {
	base := t.TempDir()

	if err := os.MkdirAll(filepath.Join(base, "catalog", "v5.0", "my-operator"), 0755); err != nil {
		t.Fatalf("failed to create package dir: %v", err)
	}

	// Dest is exactly /configs (Option A doesn't apply), so the package name
	// must come from scanning the resolved (build-arg-dependent) source dir.
	dockerfilePath := writeTestDockerfile(t, base, `FROM ubuntu
LABEL com.redhat.fbc.openshift.version=["5.0"]
ARG INPUT_DIR
COPY ./${INPUT_DIR}/ /configs
`)

	buildArgs := map[string]string{"INPUT_DIR": "catalog/v5.0"}
	packages, err := GetPackages(dockerfilePath, base, buildArgs)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(packages) != 1 || packages[0] != "my-operator" {
		t.Errorf("got %v, want [my-operator]", packages)
	}

	// Without the build-arg, INPUT_DIR resolves to empty and the scan falls
	// back to the build context root, picking up the wrong directory name.
	wrongPackages, err := GetPackages(dockerfilePath, base, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(wrongPackages) == 1 && wrongPackages[0] == "my-operator" {
		t.Error("expected wrong/stale package name when INPUT_DIR build-arg is not provided, got the correct one")
	}
}

func TestGetPackages_PackageNamesFromDest(t *testing.T) {
	base := t.TempDir()

	dockerfilePath := writeTestDockerfile(t, base, `FROM ubuntu
LABEL com.redhat.fbc.openshift.version=["5.0"]
COPY ./catalog /configs/my-operator
`)

	packages, err := GetPackages(dockerfilePath, base, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(packages) != 1 || packages[0] != "my-operator" {
		t.Errorf("got %v, want [my-operator]", packages)
	}
}
