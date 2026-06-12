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

func writeTestDockerfile(t *testing.T, dir, content string) string {
	t.Helper()
	path := filepath.Join(dir, "Dockerfile")
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("failed to write test Dockerfile: %v", err)
	}
	return path
}

func TestInjectLifecycle_InjectsLifecycleJSON(t *testing.T) {
	base := t.TempDir()
	lifecycleDir := t.TempDir()

	pkgDir := filepath.Join(base, "catalog", "my-operator")
	if err := os.MkdirAll(pkgDir, 0755); err != nil {
		t.Fatalf("failed to create package dir: %v", err)
	}

	dockerfilePath := writeTestDockerfile(t, base, `FROM ubuntu
LABEL com.redhat.fbc.openshift.version=["5.0"]
COPY catalog /configs
`)

	lifecycleData := []byte(`{"schema":"io.openshift.operators.lifecycles.v1alpha1"}`)
	pkgLifecycleDir := filepath.Join(lifecycleDir, "my-operator")
	if err := os.MkdirAll(pkgLifecycleDir, 0755); err != nil {
		t.Fatalf("failed to create lifecycle pkg dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(pkgLifecycleDir, "lifecycle.json"), lifecycleData, 0644); err != nil {
		t.Fatalf("failed to write lifecycle file: %v", err)
	}

	if err := InjectLifecycle(dockerfilePath, base, lifecycleDir, "my-operator"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	got, err := os.ReadFile(filepath.Join(pkgDir, "lifecycle.json"))
	if err != nil {
		t.Fatalf("lifecycle.json not injected: %v", err)
	}
	if string(got) != string(lifecycleData) {
		t.Errorf("content mismatch\ngot: %s\nwant: %s", got, lifecycleData)
	}
}

func TestInjectLifecycle_SkipsInjection_WhenNoOCPVersionGTE5(t *testing.T) {
	base := t.TempDir()
	lifecycleDir := t.TempDir()

	pkgDir := filepath.Join(base, "catalog", "my-operator")
	if err := os.MkdirAll(pkgDir, 0755); err != nil {
		t.Fatalf("failed to create package dir: %v", err)
	}

	// set up lifecycle file so test doesn't accidentally pass due to missing file
	pkgLifecycleDir := filepath.Join(lifecycleDir, "my-operator")
	if err := os.MkdirAll(pkgLifecycleDir, 0755); err != nil {
		t.Fatalf("failed to create lifecycle pkg dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(pkgLifecycleDir, "lifecycle.json"), []byte(`{}`), 0644); err != nil {
		t.Fatalf("failed to write lifecycle file: %v", err)
	}

	dockerfilePath := writeTestDockerfile(t, base, `FROM ubuntu
LABEL com.redhat.fbc.openshift.version=["4.20"]
COPY catalog /configs
`)

	if err := InjectLifecycle(dockerfilePath, base, lifecycleDir, "my-operator"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if _, err := os.Stat(filepath.Join(pkgDir, "lifecycle.json")); err == nil {
		t.Fatal("expected lifecycle.json to NOT be injected for OCP < 5.0")
	}
}

func TestInjectLifecycle_MultiplePackages(t *testing.T) {
	base := t.TempDir()
	lifecycleDir := t.TempDir()

	for _, pkg := range []string{"operator-a", "operator-b"} {
		if err := os.MkdirAll(filepath.Join(base, "catalog", pkg), 0755); err != nil {
			t.Fatalf("failed to create package dir: %v", err)
		}
		lifecycleData := []byte(`{"schema":"io.openshift.operators.lifecycles.v1alpha1"}`)
		pkgLifecycleDir := filepath.Join(lifecycleDir, pkg)
		if err := os.MkdirAll(pkgLifecycleDir, 0755); err != nil {
			t.Fatalf("failed to create lifecycle pkg dir: %v", err)
		}
		if err := os.WriteFile(filepath.Join(pkgLifecycleDir, "lifecycle.json"), lifecycleData, 0644); err != nil {
			t.Fatalf("failed to write lifecycle file: %v", err)
		}
	}

	dockerfilePath := writeTestDockerfile(t, base, `FROM ubuntu
LABEL com.redhat.fbc.openshift.version=["5.0"]
COPY catalog /configs
`)

	if err := InjectLifecycle(dockerfilePath, base, lifecycleDir, "operator-a,operator-b"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	for _, pkg := range []string{"operator-a", "operator-b"} {
		if _, err := os.Stat(filepath.Join(base, "catalog", pkg, "lifecycle.json")); err != nil {
			t.Errorf("expected lifecycle.json for package %q: %v", pkg, err)
		}
	}
}

func TestInjectLifecycle_MissingLifecycleFile_ReturnsError(t *testing.T) {
	base := t.TempDir()
	lifecycleDir := t.TempDir()

	if err := os.MkdirAll(filepath.Join(base, "catalog", "my-operator"), 0755); err != nil {
		t.Fatalf("failed to create package dir: %v", err)
	}

	dockerfilePath := writeTestDockerfile(t, base, `FROM ubuntu
LABEL com.redhat.fbc.openshift.version=["5.0"]
COPY catalog /configs
`)

	err := InjectLifecycle(dockerfilePath, base, lifecycleDir, "my-operator")
	if err == nil {
		t.Fatal("expected error for missing lifecycle file, got nil")
	}
}

func TestInjectLifecycle_InvalidDockerfile_ReturnsError(t *testing.T) {
	err := InjectLifecycle("/nonexistent/Dockerfile", t.TempDir(), t.TempDir(), "my-operator")
	if err == nil {
		t.Fatal("expected error for nonexistent Dockerfile, got nil")
	}
}

func TestInjectLifecycle_MixedOCPVersions_SkipsWhenNotAllGTE5(t *testing.T) {
	base := t.TempDir()
	lifecycleDir := t.TempDir()

	pkgDir := filepath.Join(base, "catalog", "my-operator")
	if err := os.MkdirAll(pkgDir, 0755); err != nil {
		t.Fatalf("failed to create package dir: %v", err)
	}

	dockerfilePath := writeTestDockerfile(t, base, `FROM ubuntu
LABEL com.redhat.fbc.openshift.version=["4.20","5.0"]
COPY catalog /configs
`)

	lifecycleData := []byte(`{"schema":"io.openshift.operators.lifecycles.v1alpha1"}`)
	pkgLifecycleDir := filepath.Join(lifecycleDir, "my-operator")
	if err := os.MkdirAll(pkgLifecycleDir, 0755); err != nil {
		t.Fatalf("failed to create lifecycle pkg dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(pkgLifecycleDir, "lifecycle.json"), lifecycleData, 0644); err != nil {
		t.Fatalf("failed to write lifecycle file: %v", err)
	}

	if err := InjectLifecycle(dockerfilePath, base, lifecycleDir, "my-operator"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if _, err := os.Stat(filepath.Join(pkgDir, "lifecycle.json")); err == nil {
		t.Fatal("expected lifecycle.json to NOT be injected when not all versions >= 5.0")
	}
}

func TestInjectLifecycle_PackagesWithWhitespace_Trimmed(t *testing.T) {
	base := t.TempDir()
	lifecycleDir := t.TempDir()

	pkgDir := filepath.Join(base, "catalog", "my-operator")
	if err := os.MkdirAll(pkgDir, 0755); err != nil {
		t.Fatalf("failed to create package dir: %v", err)
	}

	dockerfilePath := writeTestDockerfile(t, base, `FROM ubuntu
LABEL com.redhat.fbc.openshift.version=["5.0"]
COPY catalog /configs
`)

	lifecycleData := []byte(`{"schema":"io.openshift.operators.lifecycles.v1alpha1"}`)
	pkgLifecycleDir := filepath.Join(lifecycleDir, "my-operator")
	if err := os.MkdirAll(pkgLifecycleDir, 0755); err != nil {
		t.Fatalf("failed to create lifecycle pkg dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(pkgLifecycleDir, "lifecycle.json"), lifecycleData, 0644); err != nil {
		t.Fatalf("failed to write lifecycle file: %v", err)
	}

	// packages string with extra whitespace
	if err := InjectLifecycle(dockerfilePath, base, lifecycleDir, " my-operator "); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if _, err := os.Stat(filepath.Join(pkgDir, "lifecycle.json")); err != nil {
		t.Errorf("expected lifecycle.json to be injected: %v", err)
	}
}

func TestInjectLifecycle_MultiplePackages_SeparateCOPYEntries(t *testing.T) {
	base := t.TempDir()
	lifecycleDir := t.TempDir()
	lifecycleData := []byte(`{"schema":"io.openshift.operators.lifecycles.v1alpha1"}`)

	for _, pkg := range []string{"operator-a", "operator-b"} {
		if err := os.MkdirAll(filepath.Join(base, "catalog", pkg), 0755); err != nil {
			t.Fatalf("failed to create package dir: %v", err)
		}
		pkgLifecycleDir := filepath.Join(lifecycleDir, pkg)
		if err := os.MkdirAll(pkgLifecycleDir, 0755); err != nil {
			t.Fatalf("failed to create lifecycle pkg dir: %v", err)
		}
		if err := os.WriteFile(filepath.Join(pkgLifecycleDir, "lifecycle.json"), lifecycleData, 0644); err != nil {
			t.Fatalf("failed to write lifecycle file: %v", err)
		}
	}

	dockerfilePath := writeTestDockerfile(t, base, `FROM ubuntu
LABEL com.redhat.fbc.openshift.version=["5.0"]
COPY catalog/operator-a /configs/operator-a
COPY catalog/operator-b /configs/operator-b
`)

	if err := InjectLifecycle(dockerfilePath, base, lifecycleDir, "operator-a,operator-b"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	for _, pkg := range []string{"operator-a", "operator-b"} {
		if _, err := os.Stat(filepath.Join(base, "catalog", pkg, "lifecycle.json")); err != nil {
			t.Errorf("expected lifecycle.json for package %q: %v", pkg, err)
		}
	}
}

func TestInjectLifecycle_DegeneratePackagesString_ReturnsError(t *testing.T) {
	base := t.TempDir()
	dockerfilePath := writeTestDockerfile(t, base, `FROM ubuntu
LABEL com.redhat.fbc.openshift.version=["5.0"]
COPY catalog /configs
`)

	for _, packages := range []string{",", ",,", "  ,  ", " "} {
		err := InjectLifecycle(dockerfilePath, base, t.TempDir(), packages)
		if err == nil {
			t.Errorf("expected error for degenerate packages string %q, got nil", packages)
		}
	}
}

func TestInjectLifecycle_DuplicateCOPYEntries_InjectsOnce(t *testing.T) {
	base := t.TempDir()
	lifecycleDir := t.TempDir()

	pkgDir := filepath.Join(base, "catalog", "my-operator")
	if err := os.MkdirAll(pkgDir, 0755); err != nil {
		t.Fatalf("failed to create package dir: %v", err)
	}

	pkgLifecycleDir := filepath.Join(lifecycleDir, "my-operator")
	if err := os.MkdirAll(pkgLifecycleDir, 0755); err != nil {
		t.Fatalf("failed to create lifecycle pkg dir: %v", err)
	}
	lifecycleData := []byte(`{"schema":"io.openshift.operators.lifecycles.v1alpha1"}`)
	if err := os.WriteFile(filepath.Join(pkgLifecycleDir, "lifecycle.json"), lifecycleData, 0644); err != nil {
		t.Fatalf("failed to write lifecycle file: %v", err)
	}

	// two COPY entries targeting the same destination
	dockerfilePath := writeTestDockerfile(t, base, `FROM ubuntu
LABEL com.redhat.fbc.openshift.version=["5.0"]
COPY catalog /configs
COPY catalog /configs
`)

	if err := InjectLifecycle(dockerfilePath, base, lifecycleDir, "my-operator"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	got, err := os.ReadFile(filepath.Join(pkgDir, "lifecycle.json"))
	if err != nil {
		t.Fatalf("lifecycle.json not injected: %v", err)
	}
	if string(got) != string(lifecycleData) {
		t.Errorf("content mismatch\ngot: %s\nwant: %s", got, lifecycleData)
	}
}

func TestInjectLifecycle_DuplicatePackageNames_DeduplicatedCorrectly(t *testing.T) {
	base := t.TempDir()
	lifecycleDir := t.TempDir()
	lifecycleData := []byte(`{"schema":"io.openshift.operators.lifecycles.v1alpha1"}`)

	pkgDir := filepath.Join(base, "catalog", "my-operator")
	if err := os.MkdirAll(pkgDir, 0755); err != nil {
		t.Fatalf("failed to create package dir: %v", err)
	}

	pkgLifecycleDir := filepath.Join(lifecycleDir, "my-operator")
	if err := os.MkdirAll(pkgLifecycleDir, 0755); err != nil {
		t.Fatalf("failed to create lifecycle pkg dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(pkgLifecycleDir, "lifecycle.json"), lifecycleData, 0644); err != nil {
		t.Fatalf("failed to write lifecycle file: %v", err)
	}

	dockerfilePath := writeTestDockerfile(t, base, `FROM ubuntu
LABEL com.redhat.fbc.openshift.version=["5.0"]
COPY catalog /configs
`)

	// duplicate package name — should be deduplicated and not cause O_EXCL failure
	if err := InjectLifecycle(dockerfilePath, base, lifecycleDir, "my-operator,my-operator"); err != nil {
		t.Fatalf("unexpected error for duplicate package names: %v", err)
	}
}
