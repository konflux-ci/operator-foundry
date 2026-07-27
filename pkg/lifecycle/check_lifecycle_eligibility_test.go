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

func TestCheckLifecycleEligibility_True_WhenOCPVersionGTE5(t *testing.T) {
	base := t.TempDir()

	dockerfilePath := writeTestDockerfile(t, base, `FROM ubuntu
LABEL com.redhat.fbc.openshift.version=["5.0"]
COPY catalog /configs
`)

	eligible, err := CheckLifecycleEligibility(dockerfilePath, base, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !eligible {
		t.Error("got eligible=false, want true for OCP version 5.0")
	}
}

func TestCheckLifecycleEligibility_False_WhenOCPVersionBelow5(t *testing.T) {
	base := t.TempDir()

	dockerfilePath := writeTestDockerfile(t, base, `FROM ubuntu
LABEL com.redhat.fbc.openshift.version=["4.20"]
COPY catalog /configs
`)

	eligible, err := CheckLifecycleEligibility(dockerfilePath, base, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if eligible {
		t.Error("got eligible=true, want false for OCP version below 5.0")
	}
}

func TestCheckLifecycleEligibility_False_WhenMixedVersions(t *testing.T) {
	base := t.TempDir()

	dockerfilePath := writeTestDockerfile(t, base, `FROM ubuntu
LABEL com.redhat.fbc.openshift.version=["4.20","5.0"]
COPY catalog /configs
`)

	eligible, err := CheckLifecycleEligibility(dockerfilePath, base, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if eligible {
		t.Error("got eligible=true, want false — not all versions >= 5.0")
	}
}

func TestCheckLifecycleEligibility_InvalidDockerfile_ReturnsError(t *testing.T) {
	_, err := CheckLifecycleEligibility("/nonexistent/Dockerfile", t.TempDir(), nil)
	if err == nil {
		t.Fatal("expected error for nonexistent Dockerfile, got nil")
	}
}

func TestCheckLifecycleEligibility_DockerfileInSubdirectory_ResolvesRelativeToBuildContext(t *testing.T) {
	base := t.TempDir()
	if err := os.MkdirAll(filepath.Join(base, "v5.0"), 0755); err != nil {
		t.Fatalf("failed to create subdirectory: %v", err)
	}
	dockerfileContent := []byte(`FROM ubuntu
LABEL com.redhat.fbc.openshift.version=["5.0"]
COPY catalog /configs
`)
	if err := os.WriteFile(filepath.Join(base, "v5.0", "catalog.Dockerfile"), dockerfileContent, 0644); err != nil {
		t.Fatalf("failed to write dockerfile: %v", err)
	}

	// dockerfile path is relative to the build context, not the current directory.
	eligible, err := CheckLifecycleEligibility("catalog.Dockerfile", filepath.Join(base, "v5.0"), nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !eligible {
		t.Error("got eligible=false, want true for OCP version 5.0")
	}
}

func TestCheckLifecycleEligibility_EmptyLabel_ReturnsError(t *testing.T) {
	base := t.TempDir()

	dockerfilePath := writeTestDockerfile(t, base, `FROM ubuntu
LABEL com.redhat.fbc.openshift.version=[]
COPY catalog /configs
`)

	_, err := CheckLifecycleEligibility(dockerfilePath, base, nil)
	if err == nil {
		t.Fatal("expected error for empty label array, got nil")
	}
}

func TestCheckLifecycleEligibility_InvalidVersionInLabel_ReturnsError(t *testing.T) {
	base := t.TempDir()

	dockerfilePath := writeTestDockerfile(t, base, `FROM ubuntu
LABEL com.redhat.fbc.openshift.version=["4.20","invalid"]
COPY catalog /configs
`)

	_, err := CheckLifecycleEligibility(dockerfilePath, base, nil)
	if err == nil {
		t.Fatal("expected error for invalid OCP version in label, got nil")
	}
}
