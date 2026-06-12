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

	packages, err := GetPackages(dockerfilePath, base)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(packages) != 1 || packages[0] != "my-operator" {
		t.Errorf("got %v, want [my-operator]", packages)
	}
}

func TestGetPackages_ReturnsEmpty_WhenNoOCPVersionGTE5(t *testing.T) {
	base := t.TempDir()

	if err := os.MkdirAll(filepath.Join(base, "catalog", "my-operator"), 0755); err != nil {
		t.Fatalf("failed to create package dir: %v", err)
	}

	dockerfilePath := writeTestDockerfile(t, base, `FROM ubuntu
LABEL com.redhat.fbc.openshift.version=["4.20"]
COPY catalog /configs
`)

	packages, err := GetPackages(dockerfilePath, base)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(packages) != 0 {
		t.Errorf("got %v, want empty slice", packages)
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

	packages, err := GetPackages(dockerfilePath, base)
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

func TestGetPackages_MixedVersions_ReturnsEmpty(t *testing.T) {
	base := t.TempDir()

	if err := os.MkdirAll(filepath.Join(base, "catalog", "my-operator"), 0755); err != nil {
		t.Fatalf("failed to create package dir: %v", err)
	}

	dockerfilePath := writeTestDockerfile(t, base, `FROM ubuntu
LABEL com.redhat.fbc.openshift.version=["4.20","5.0"]
COPY catalog /configs
`)

	packages, err := GetPackages(dockerfilePath, base)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(packages) != 0 {
		t.Errorf("got %v, want empty — not all versions >= 5.0", packages)
	}
}

func TestGetPackages_InvalidDockerfile_ReturnsError(t *testing.T) {
	_, err := GetPackages("/nonexistent/Dockerfile", t.TempDir())
	if err == nil {
		t.Fatal("expected error for nonexistent Dockerfile, got nil")
	}
}

func TestGetPackages_PackageNamesFromDest(t *testing.T) {
	base := t.TempDir()

	dockerfilePath := writeTestDockerfile(t, base, `FROM ubuntu
LABEL com.redhat.fbc.openshift.version=["5.0"]
COPY ./catalog /configs/my-operator
`)

	packages, err := GetPackages(dockerfilePath, base)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(packages) != 1 || packages[0] != "my-operator" {
		t.Errorf("got %v, want [my-operator]", packages)
	}
}
