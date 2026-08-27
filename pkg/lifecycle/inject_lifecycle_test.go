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

	if err := InjectLifecycle(dockerfilePath, base, lifecycleDir, "my-operator", nil); err != nil {
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

	if err := InjectLifecycle(dockerfilePath, base, lifecycleDir, "operator-a,operator-b", nil); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	for _, pkg := range []string{"operator-a", "operator-b"} {
		if _, err := os.Stat(filepath.Join(base, "catalog", pkg, "lifecycle.json")); err != nil {
			t.Errorf("expected lifecycle.json for package %q: %v", pkg, err)
		}
	}
}

func TestInjectLifecycle_DockerfileInSubdirectory_ResolvesRelativeToBuildContext(t *testing.T) {
	base := t.TempDir()
	ctx := filepath.Join(base, "v5.0")
	lifecycleDir := t.TempDir()

	pkgDir := filepath.Join(ctx, "catalog", "my-operator")
	if err := os.MkdirAll(pkgDir, 0755); err != nil {
		t.Fatalf("failed to create package dir: %v", err)
	}

	dockerfileContent := []byte(`FROM ubuntu
LABEL com.redhat.fbc.openshift.version=["5.0"]
COPY catalog /configs
`)
	if err := os.WriteFile(filepath.Join(ctx, "catalog.Dockerfile"), dockerfileContent, 0644); err != nil {
		t.Fatalf("failed to write dockerfile: %v", err)
	}

	lifecycleData := []byte(`{"schema":"io.openshift.operators.lifecycles.v1alpha1"}`)
	pkgLifecycleDir := filepath.Join(lifecycleDir, "my-operator")
	if err := os.MkdirAll(pkgLifecycleDir, 0755); err != nil {
		t.Fatalf("failed to create lifecycle pkg dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(pkgLifecycleDir, "lifecycle.json"), lifecycleData, 0644); err != nil {
		t.Fatalf("failed to write lifecycle file: %v", err)
	}

	// dockerfile path is relative to the build context, not the current directory.
	if err := InjectLifecycle("catalog.Dockerfile", ctx, lifecycleDir, "my-operator", nil); err != nil {
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

	err := InjectLifecycle(dockerfilePath, base, lifecycleDir, "my-operator", nil)
	if err == nil {
		t.Fatal("expected error for missing lifecycle file, got nil")
	}
}

func TestInjectLifecycle_InvalidDockerfile_ReturnsError(t *testing.T) {
	err := InjectLifecycle("/nonexistent/Dockerfile", t.TempDir(), t.TempDir(), "my-operator", nil)
	if err == nil {
		t.Fatal("expected error for nonexistent Dockerfile, got nil")
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
	if err := InjectLifecycle(dockerfilePath, base, lifecycleDir, " my-operator ", nil); err != nil {
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

	if err := InjectLifecycle(dockerfilePath, base, lifecycleDir, "operator-a,operator-b", nil); err != nil {
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
		err := InjectLifecycle(dockerfilePath, base, t.TempDir(), packages, nil)
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

	if err := InjectLifecycle(dockerfilePath, base, lifecycleDir, "my-operator", nil); err != nil {
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

func TestInjectLifecycle_BuildArgResolvesInputDirSourcePath(t *testing.T) {
	// A stage-scoped ARG with no default, used only in the COPY source path.
	base := t.TempDir()
	lifecycleDir := t.TempDir()

	pkgDir := filepath.Join(base, "catalog", "v5.0", "gatekeeper-operator-product")
	if err := os.MkdirAll(pkgDir, 0755); err != nil {
		t.Fatalf("failed to create package dir: %v", err)
	}

	dockerfilePath := writeTestDockerfile(t, base, `ARG OPM_IMAGE=quay.io/operator-framework/opm:latest
FROM ${OPM_IMAGE} as builder
LABEL com.redhat.fbc.openshift.version=["5.0"]
ARG INPUT_DIR
COPY ./${INPUT_DIR}/ /configs/gatekeeper-operator-product
RUN ["/bin/opm", "serve", "/configs", "--cache-dir=/tmp/cache", "--cache-only"]

FROM ${OPM_IMAGE}
COPY --from=builder /configs /configs
COPY --from=builder /tmp/cache /tmp/cache
`)

	lifecycleData := []byte(`{"schema":"io.openshift.operators.lifecycles.v1alpha1"}`)
	pkgLifecycleDir := filepath.Join(lifecycleDir, "gatekeeper-operator-product")
	if err := os.MkdirAll(pkgLifecycleDir, 0755); err != nil {
		t.Fatalf("failed to create lifecycle pkg dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(pkgLifecycleDir, "lifecycle.json"), lifecycleData, 0644); err != nil {
		t.Fatalf("failed to write lifecycle file: %v", err)
	}

	// Without the matching build-arg, the real catalog subdirectory can't be found.
	if err := InjectLifecycle(dockerfilePath, base, lifecycleDir, "gatekeeper-operator-product", nil); err == nil {
		t.Fatal("expected error when INPUT_DIR build-arg is not provided, got nil")
	}

	buildArgs := map[string]string{"INPUT_DIR": "catalog/v5.0"}
	if err := InjectLifecycle(dockerfilePath, base, lifecycleDir, "gatekeeper-operator-product", buildArgs); err != nil {
		t.Fatalf("unexpected error with matching build-arg: %v", err)
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
	if err := InjectLifecycle(dockerfilePath, base, lifecycleDir, "my-operator,my-operator", nil); err != nil {
		t.Fatalf("unexpected error for duplicate package names: %v", err)
	}
}

func TestInjectLifecycle_BuilderStagePattern_TracedToContext(t *testing.T) {
	// Simulates the TALM Dockerfile pattern: the catalog is copied into a builder
	// stage that transforms it with a RUN command, and the final stage copies from
	// the builder. The inject step must trace the --from=builder COPY back to the
	// build-context source and write lifecycle.json there.
	base := t.TempDir()
	lifecycleDir := t.TempDir()
	lifecycleData := []byte(`{"schema":"io.openshift.operators.lifecycles.v1alpha1"}`)

	pkgDir := filepath.Join(base, ".konflux", "catalog", "topology-aware-lifecycle-manager")
	if err := os.MkdirAll(pkgDir, 0755); err != nil {
		t.Fatalf("failed to create package dir: %v", err)
	}

	pkgLifecycleDir := filepath.Join(lifecycleDir, "topology-aware-lifecycle-manager")
	if err := os.MkdirAll(pkgLifecycleDir, 0755); err != nil {
		t.Fatalf("failed to create lifecycle pkg dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(pkgLifecycleDir, "lifecycle.json"), lifecycleData, 0644); err != nil {
		t.Fatalf("failed to write lifecycle file: %v", err)
	}

	dockerfilePath := writeTestDockerfile(t, base, `ARG BUILDER_IMAGE=golang:1.21
FROM ${BUILDER_IMAGE} AS builder
COPY .konflux/catalog/ /app/.konflux/catalog/
RUN make fix-catalog-name

FROM ubuntu
ENV PACKAGE_NAME=topology-aware-lifecycle-manager
COPY --from=builder /app/.konflux/catalog/${PACKAGE_NAME}/ /configs/${PACKAGE_NAME}
`)

	if err := InjectLifecycle(dockerfilePath, base, lifecycleDir, "topology-aware-lifecycle-manager", nil); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	got, err := os.ReadFile(filepath.Join(pkgDir, "lifecycle.json"))
	if err != nil {
		t.Fatalf("lifecycle.json not injected into build context: %v", err)
	}
	if string(got) != string(lifecycleData) {
		t.Errorf("content mismatch\ngot: %s\nwant: %s", got, lifecycleData)
	}
}

func TestInjectLifecycle_BuilderStagePattern_UnresolvableStillFails(t *testing.T) {
	// When the builder stage itself copies from another stage (nested --from=),
	// the trace cannot reach the build context and injection must still fail.
	base := t.TempDir()
	lifecycleDir := t.TempDir()
	lifecycleData := []byte(`{"schema":"io.openshift.operators.lifecycles.v1alpha1"}`)

	pkgLifecycleDir := filepath.Join(lifecycleDir, "my-operator")
	if err := os.MkdirAll(pkgLifecycleDir, 0755); err != nil {
		t.Fatalf("failed to create lifecycle pkg dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(pkgLifecycleDir, "lifecycle.json"), lifecycleData, 0644); err != nil {
		t.Fatalf("failed to write lifecycle file: %v", err)
	}

	dockerfilePath := writeTestDockerfile(t, base, `FROM ubuntu AS base
COPY catalog/ /app/catalog/

FROM base AS builder
COPY --from=base /app/catalog/ /app/.konflux/catalog/

FROM ubuntu
COPY --from=builder /app/.konflux/catalog/my-operator/ /configs/my-operator
`)

	err := InjectLifecycle(dockerfilePath, base, lifecycleDir, "my-operator", nil)
	if err == nil {
		t.Fatal("expected error for unresolvable builder chain, got nil")
	}
}
