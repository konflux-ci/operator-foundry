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

package ocp

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/keilerkonzept/dockerfile-json/pkg/dockerfile"
)

func mustParseDockerfile(t *testing.T, src string) *dockerfile.Dockerfile {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "Dockerfile")
	if err := os.WriteFile(path, []byte(src), 0644); err != nil {
		t.Fatalf("failed to write temp Dockerfile: %v", err)
	}
	d, err := dockerfile.Parse(path)
	if err != nil {
		t.Fatalf("failed to parse test Dockerfile: %v", err)
	}
	return d
}

// ── GetOCPVersionsFromDockerfile ────────────────────────────────────────────────────────────

func TestGetOCPVersionsFromDockerfile_NilDockerfile(t *testing.T) {
	_, err := GetOCPVersionsFromDockerfile(nil)
	if err == nil {
		t.Fatal("expected error for nil Dockerfile, got nil")
	}
}

func TestGetOCPVersionsFromDockerfile_FromLabel(t *testing.T) {
	d := mustParseDockerfile(t, `
FROM ubuntu
LABEL com.redhat.fbc.openshift.version=["4.20","4.21","5.0"]
`)
	versions, err := GetOCPVersionsFromDockerfile(d)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(versions) != 3 {
		t.Fatalf("got %d versions, want 3", len(versions))
	}
	if versions[0] != "4.20" || versions[1] != "4.21" || versions[2] != "5.0" {
		t.Errorf("got %v, want [4.20 4.21 5.0]", versions)
	}
}

func TestGetOCPVersionsFromDockerfile_FromBaseImage(t *testing.T) {
	d := mustParseDockerfile(t, `
FROM registry.redhat.io/openshift4/ose-operator-registry-rhel9:v4.15
`)
	versions, err := GetOCPVersionsFromDockerfile(d)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(versions) != 1 || versions[0] != "4.15" {
		t.Errorf("got %v, want [4.15]", versions)
	}
}

func TestGetOCPVersionsFromDockerfile_FromBaseImageWithPort(t *testing.T) {
	d := mustParseDockerfile(t, `
FROM registry.example.com:5000/openshift4/ose-operator-registry-rhel9:v4.15
`)
	versions, err := GetOCPVersionsFromDockerfile(d)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(versions) != 1 || versions[0] != "4.15" {
		t.Errorf("got %v, want [4.15]", versions)
	}
}

func TestGetOCPVersionsFromDockerfile_LabelTakesPrecedenceOverBaseImage(t *testing.T) {
	d := mustParseDockerfile(t, `
FROM registry.redhat.io/openshift4/ose-operator-registry-rhel9:v4.15
LABEL com.redhat.fbc.openshift.version=["4.20","4.21"]
`)
	versions, err := GetOCPVersionsFromDockerfile(d)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(versions) != 2 {
		t.Fatalf("got %d versions, want 2", len(versions))
	}
	if versions[0] != "4.20" || versions[1] != "4.21" {
		t.Errorf("got %v, want [4.20 4.21]", versions)
	}
}

func TestGetOCPVersionsFromDockerfile_EmptyLabel_ReturnsError(t *testing.T) {
	d := mustParseDockerfile(t, `
FROM ubuntu
LABEL com.redhat.fbc.openshift.version=[]
`)
	_, err := GetOCPVersionsFromDockerfile(d)
	if err == nil {
		t.Fatal("expected error for empty label array, got nil")
	}
}

func TestGetOCPVersionsFromDockerfile_InvalidLabelFormat_ReturnsError(t *testing.T) {
	d := mustParseDockerfile(t, `
FROM ubuntu
LABEL com.redhat.fbc.openshift.version=not-a-json-array
`)
	_, err := GetOCPVersionsFromDockerfile(d)
	if err == nil {
		t.Fatal("expected error for invalid label format, got nil")
	}
}

func TestGetOCPVersionsFromDockerfile_InvalidVersionInLabel_ReturnsError(t *testing.T) {
	d := mustParseDockerfile(t, `
FROM ubuntu
LABEL com.redhat.fbc.openshift.version=["4.20","invalid"]
`)
	_, err := GetOCPVersionsFromDockerfile(d)
	if err == nil {
		t.Fatal("expected error for invalid OCP version in label, got nil")
	}
}

func TestGetOCPVersionsFromDockerfile_NoLabelNoBaseImageTag_ReturnsError(t *testing.T) {
	d := mustParseDockerfile(t, `
FROM registry.redhat.io/openshift4/ose-operator-registry-rhel9
`)
	_, err := GetOCPVersionsFromDockerfile(d)
	if err == nil {
		t.Fatal("expected error when no label and no versioned base image, got nil")
	}
}

func TestGetOCPVersionsFromDockerfile_LabelInBuilderStageIgnored(t *testing.T) {
	d := mustParseDockerfile(t, `
FROM ubuntu AS builder
LABEL com.redhat.fbc.openshift.version=["4.20"]

FROM ubuntu
LABEL com.redhat.fbc.openshift.version=["5.0"]
COPY --from=builder /catalog /configs
`)
	versions, err := GetOCPVersionsFromDockerfile(d)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(versions) != 1 || versions[0] != "5.0" {
		t.Errorf("got %v, want [5.0] — builder stage label should be ignored", versions)
	}
}

func TestGetOCPVersionsFromDockerfile_DuplicateLabelInStage_LastWins(t *testing.T) {
	d := mustParseDockerfile(t, `
FROM ubuntu
LABEL com.redhat.fbc.openshift.version=["4.20"]
LABEL com.redhat.fbc.openshift.version=["5.0"]
`)
	versions, err := GetOCPVersionsFromDockerfile(d)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(versions) != 1 || versions[0] != "5.0" {
		t.Errorf("got %v, want [5.0] — last label declaration should win", versions)
	}
}

func TestGetOCPVersionsFromDockerfile_LabelOnlyInFinalStage(t *testing.T) {
	d := mustParseDockerfile(t, `
FROM ubuntu AS builder
RUN make catalog

FROM ubuntu
LABEL com.redhat.fbc.openshift.version=["5.0"]
COPY --from=builder /catalog /configs
`)
	versions, err := GetOCPVersionsFromDockerfile(d)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(versions) != 1 || versions[0] != "5.0" {
		t.Errorf("got %v, want [5.0]", versions)
	}
}

func TestGetOCPVersionsFromDockerfile_LabelOnlyInBuilderStage_FallsBackToBaseImage(t *testing.T) {
	// label only in builder stage — must NOT be picked up
	// falls back to base image tag
	d := mustParseDockerfile(t, `
FROM ubuntu AS builder
LABEL com.redhat.fbc.openshift.version=["4.20"]

FROM registry.redhat.io/openshift4/ose-operator-registry-rhel9:v5.0
COPY --from=builder /catalog /configs
`)
	versions, err := GetOCPVersionsFromDockerfile(d)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(versions) != 1 || versions[0] != "5.0" {
		t.Errorf("got %v, want [5.0] — should fall back to base image, not use builder stage label", versions)
	}
}

func TestGetOCPVersionsFromDockerfile_DuplicateKeyInSingleLabel_LastWins(t *testing.T) {
	// LABEL with duplicate key on same line — last value must win (Docker semantics)
	d := mustParseDockerfile(t, `
FROM ubuntu
LABEL com.redhat.fbc.openshift.version=["4.20"] com.redhat.fbc.openshift.version=["5.0"]
`)
	versions, err := GetOCPVersionsFromDockerfile(d)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(versions) != 1 || versions[0] != "5.0" {
		t.Errorf("got %v, want [5.0] — last value in LABEL instruction should win", versions)
	}
}

func TestGetOCPVersionsFromDockerfile_TagAndDigestBaseImage_ExtractsTag(t *testing.T) {
	d := mustParseDockerfile(t, `
FROM registry.redhat.io/openshift4/ose-operator-registry-rhel9:v4.15@sha256:abc123def456abc123def456abc123def456abc123def456abc123def456abcd
`)
	versions, err := GetOCPVersionsFromDockerfile(d)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(versions) != 1 || versions[0] != "4.15" {
		t.Errorf("got %v, want [4.15]", versions)
	}
}

func TestGetOCPVersionsFromDockerfile_BaseImageWithNonOCPTag_ReturnsError(t *testing.T) {
	d := mustParseDockerfile(t, `
FROM registry.redhat.io/openshift4/ose-operator-registry-rhel9:4.17-ubi9
`)
	_, err := GetOCPVersionsFromDockerfile(d)
	if err == nil {
		t.Fatal("expected error for non-OCP base image tag, got nil")
	}
}

// ── ValidateOCPVersion ────────────────────────────────────────────────────────

func TestValidateOCPVersion_ValidFormats(t *testing.T) {
	valid := []string{"4.15", "4.20", "5.0", "v4.15", "v5.0"}
	for _, v := range valid {
		if err := ValidateOCPVersion(v); err != nil {
			t.Errorf("expected valid for %q, got error: %v", v, err)
		}
	}
}

func TestValidateOCPVersion_InvalidFormats(t *testing.T) {
	invalid := []string{"3.11", "4", "4.", ".15", "abc", "", "v", "4.15.1"}
	for _, v := range invalid {
		if err := ValidateOCPVersion(v); err == nil {
			t.Errorf("expected error for %q, got nil", v)
		}
	}
}

// ── AnyOCPVersionGTE ──────────────────────────────────────────────────────────

func TestAnyOCPVersionGTE_ContainsMatchingVersion(t *testing.T) {
	versions := []string{"4.20", "4.21", "5.0"}
	got, err := AnyOCPVersionGTE(versions, "5.0")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !got {
		t.Error("expected true, got false")
	}
}

func TestAnyOCPVersionGTE_NoVersionAboveThreshold(t *testing.T) {
	versions := []string{"4.20", "4.21", "4.23"}
	got, err := AnyOCPVersionGTE(versions, "5.0")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got {
		t.Error("expected false, got true")
	}
}

func TestAnyOCPVersionGTE_EmptyVersions(t *testing.T) {
	got, err := AnyOCPVersionGTE([]string{}, "5.0")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got {
		t.Error("expected false for empty versions, got true")
	}
}

// ── OCPVersionGTE ─────────────────────────────────────────────────────────────

func TestOCPVersionGTE_MajorVersionDiffers(t *testing.T) {
	got, err := OCPVersionGTE("5.0", "4.23")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !got {
		t.Error("expected 5.0 >= 4.23")
	}

	got, err = OCPVersionGTE("4.23", "5.0")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got {
		t.Error("expected 4.23 < 5.0")
	}
}

func TestOCPVersionGTE_MinorVersionDiffers(t *testing.T) {
	got, err := OCPVersionGTE("4.21", "4.20")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !got {
		t.Error("expected 4.21 >= 4.20")
	}

	got, err = OCPVersionGTE("4.19", "4.20")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got {
		t.Error("expected 4.19 < 4.20")
	}
}

func TestOCPVersionGTE_EqualVersions(t *testing.T) {
	got, err := OCPVersionGTE("4.20", "4.20")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !got {
		t.Error("expected 4.20 >= 4.20")
	}
}

func TestOCPVersionGTE_WithLeadingV(t *testing.T) {
	got, err := OCPVersionGTE("v5.0", "5.0")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !got {
		t.Error("expected v5.0 >= 5.0")
	}

	got, err = OCPVersionGTE("5.0", "v5.0")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !got {
		t.Error("expected 5.0 >= v5.0")
	}
}

func TestOCPVersionGTE_NumericalMinorComparison(t *testing.T) {
	got, err := OCPVersionGTE("4.10", "4.2")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !got {
		t.Error("expected 4.10 >= 4.2 (integer comparison, not string)")
	}

	got, err = OCPVersionGTE("4.2", "4.10")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got {
		t.Error("expected 4.2 < 4.10")
	}
}

func TestOCPVersionGTE_InvalidVersion_ReturnsError(t *testing.T) {
	_, err := OCPVersionGTE("invalid", "4.20")
	if err == nil {
		t.Error("expected error when version is invalid, got nil")
	}
}

func TestOCPVersionGTE_InvalidThreshold_ReturnsError(t *testing.T) {
	_, err := OCPVersionGTE("4.20", "invalid")
	if err == nil {
		t.Error("expected error when threshold is invalid, got nil")
	}
}
