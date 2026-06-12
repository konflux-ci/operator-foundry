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
	"strings"
	"testing"
)

func TestInjectLifecycleJSON_DestWithoutPackageName(t *testing.T) {
	// COPY catalog /configs
	// src points to catalog root — package is a subdirectory
	base := t.TempDir()

	pkgDir := filepath.Join(base, "catalog", "my-operator")
	if err := os.MkdirAll(pkgDir, 0755); err != nil {
		t.Fatalf("failed to create package dir: %v", err)
	}

	lifecyclePath := filepath.Join(base, "lifecycle.json")
	lifecycleData := []byte(`{"schema": "io.openshift.operators.lifecycles.v1alpha1"}`)
	if err := os.WriteFile(lifecyclePath, lifecycleData, 0644); err != nil {
		t.Fatalf("failed to write lifecycle.json: %v", err)
	}

	entry := DockerfileCopyEntry{
		Srcs: []string{"catalog"},
		Dest: "/configs",
	}

	if _, err := InjectLifecycleJSON(lifecyclePath, base, "my-operator", entry); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	destPath := filepath.Join(pkgDir, "lifecycle.json")
	got, err := os.ReadFile(destPath)
	if err != nil {
		t.Fatalf("failed to read injected lifecycle.json: %v", err)
	}
	if string(got) != string(lifecycleData) {
		t.Errorf("content mismatch\ngot:  %s\nwant: %s", got, lifecycleData)
	}
}

func TestInjectLifecycleJSON_DestWithPackageName(t *testing.T) {
	// COPY catalog/my-operator /configs/my-operator
	// src points directly to the package directory
	base := t.TempDir()

	pkgDir := filepath.Join(base, "catalog", "my-operator")
	if err := os.MkdirAll(pkgDir, 0755); err != nil {
		t.Fatalf("failed to create package dir: %v", err)
	}

	lifecyclePath := filepath.Join(base, "lifecycle.json")
	lifecycleData := []byte(`{"schema": "io.openshift.operators.lifecycles.v1alpha1"}`)
	if err := os.WriteFile(lifecyclePath, lifecycleData, 0644); err != nil {
		t.Fatalf("failed to write lifecycle.json: %v", err)
	}

	entry := DockerfileCopyEntry{
		Srcs: []string{"catalog/my-operator"},
		Dest: "/configs/my-operator",
	}

	if _, err := InjectLifecycleJSON(lifecyclePath, base, "my-operator", entry); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	destPath := filepath.Join(pkgDir, "lifecycle.json")
	got, err := os.ReadFile(destPath)
	if err != nil {
		t.Fatalf("failed to read injected lifecycle.json: %v", err)
	}
	if string(got) != string(lifecycleData) {
		t.Errorf("content mismatch\ngot:  %s\nwant: %s", got, lifecycleData)
	}
}

func TestInjectLifecycleJSON_MultipleSrcs_CorrectOneUsed(t *testing.T) {
	// COPY catalog-a catalog-b /configs
	// only catalog-b contains my-operator
	base := t.TempDir()

	if err := os.MkdirAll(filepath.Join(base, "catalog-a"), 0755); err != nil {
		t.Fatalf("failed to create catalog-a: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(base, "catalog-b", "my-operator"), 0755); err != nil {
		t.Fatalf("failed to create catalog-b/my-operator: %v", err)
	}

	lifecyclePath := filepath.Join(base, "lifecycle.json")
	lifecycleData := []byte(`{"schema":"io.openshift.operators.lifecycles.v1alpha1"}`)
	if err := os.WriteFile(lifecyclePath, lifecycleData, 0644); err != nil {
		t.Fatalf("failed to write lifecycle.json: %v", err)
	}

	entry := DockerfileCopyEntry{
		Srcs: []string{"catalog-a", "catalog-b"},
		Dest: "/configs",
	}

	if _, err := InjectLifecycleJSON(lifecyclePath, base, "my-operator", entry); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	destPath := filepath.Join(base, "catalog-b", "my-operator", "lifecycle.json")
	if _, err := os.Stat(destPath); err != nil {
		t.Errorf("expected lifecycle.json in catalog-b/my-operator, got error: %v", err)
	}
}

func TestInjectLifecycleJSON_PackageNotFound_ReturnsFalse(t *testing.T) {
	base := t.TempDir()

	if err := os.MkdirAll(filepath.Join(base, "catalog"), 0755); err != nil {
		t.Fatalf("failed to create catalog dir: %v", err)
	}

	lifecyclePath := filepath.Join(base, "lifecycle.json")
	lifecycleData := []byte(`{"schema":"io.openshift.operators.lifecycles.v1alpha1"}`)
	if err := os.WriteFile(lifecyclePath, lifecycleData, 0644); err != nil {
		t.Fatalf("failed to write lifecycle.json: %v", err)
	}

	entry := DockerfileCopyEntry{
		Srcs: []string{"catalog"},
		Dest: "/configs",
	}

	wasInjected, err := InjectLifecycleJSON(lifecyclePath, base, "non-existent-operator", entry)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if wasInjected {
		t.Fatal("expected wasInjected=false when package directory not found, got true")
	}
}

func TestInjectLifecycleJSON_MissingLifecycleFile_ReturnsError(t *testing.T) {
	base := t.TempDir()

	if err := os.MkdirAll(filepath.Join(base, "catalog", "my-operator"), 0755); err != nil {
		t.Fatalf("failed to create package dir: %v", err)
	}

	entry := DockerfileCopyEntry{
		Srcs: []string{"catalog"},
		Dest: "/configs",
	}

	_, err := InjectLifecycleJSON("/nonexistent/lifecycle.json", base, "my-operator", entry)
	if err == nil {
		t.Fatal("expected error for missing lifecycle.json source, got nil")
	}
}

func TestInjectLifecycleJSON_BuildStageEntry_Rejects(t *testing.T) {
	base := t.TempDir()

	lifecyclePath := filepath.Join(base, "lifecycle.json")
	if err := os.WriteFile(lifecyclePath, []byte(`{}`), 0644); err != nil {
		t.Fatalf("failed to write lifecycle.json: %v", err)
	}

	entry := DockerfileCopyEntry{
		Srcs: []string{"/opt/catalog"},
		Dest: "/configs",
		From: "builder",
	}

	_, err := InjectLifecycleJSON(lifecyclePath, base, "my-operator", entry)
	if err == nil {
		t.Fatal("expected error for build stage entry, got nil")
	}

	// ADD THIS: Ensure it fails for the right reason!
	if !strings.Contains(err.Error(), "cannot inject") && !strings.Contains(err.Error(), "build stage") {
		t.Errorf("expected explicit build stage rejection error, got: %v", err)
	}
}

func TestInjectLifecycleJSON_RejectsPathTraversal(t *testing.T) {
	base := t.TempDir()

	lifecyclePath := filepath.Join(base, "lifecycle.json")
	lifecycleData := []byte(`{"schema":"io.openshift.operators.lifecycles.v1alpha1"}`)
	if err := os.WriteFile(lifecyclePath, lifecycleData, 0644); err != nil {
		t.Fatalf("failed to write lifecycle.json: %v", err)
	}

	// Malicious source path attempting to escape the build context
	entry := DockerfileCopyEntry{
		Srcs: []string{"../../../etc/shadow"},
		Dest: "/configs",
	}

	_, err := InjectLifecycleJSON(lifecyclePath, base, "my-operator", entry)
	if err == nil {
		t.Fatal("expected error due to path traversal attempt, got nil")
	}
	if !strings.Contains(err.Error(), "escapes build context") && !strings.Contains(err.Error(), "invalid source path") {
		t.Errorf("expected path traversal error message, got: %v", err)
	}
}

func TestInjectLifecycleJSON_DestWithPkgButSrcIsCatalogRoot(t *testing.T) {
	// COPY catalog /configs/my-operator
	// src is catalog root, dest has pkg name — must write to catalog/my-operator/lifecycle.json
	base := t.TempDir()

	if err := os.MkdirAll(filepath.Join(base, "catalog", "my-operator"), 0755); err != nil {
		t.Fatalf("failed to create package dir: %v", err)
	}

	lifecyclePath := filepath.Join(base, "lifecycle.json")
	lifecycleData := []byte(`{"schema": "io.openshift.operators.lifecycles.v1alpha1"}`)
	if err := os.WriteFile(lifecyclePath, lifecycleData, 0644); err != nil {
		t.Fatalf("failed to write lifecycle.json: %v", err)
	}

	entry := DockerfileCopyEntry{
		Srcs: []string{"catalog"},
		Dest: "/configs/my-operator",
	}

	if _, err := InjectLifecycleJSON(lifecyclePath, base, "my-operator", entry); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	destPath := filepath.Join(base, "catalog", "my-operator", "lifecycle.json")
	got, err := os.ReadFile(destPath)
	if err != nil {
		t.Fatalf("failed to read injected lifecycle.json: %v", err)
	}
	if string(got) != string(lifecycleData) {
		t.Errorf("content mismatch\ngot: %s\nwant: %s", got, lifecycleData)
	}
}

func TestInjectLifecycleJSON_SymlinkPackageDir_ReturnsError(t *testing.T) {
	base := t.TempDir()
	outside := t.TempDir()

	// create a symlink inside the build context pointing outside
	symlinkPath := filepath.Join(base, "catalog", "evil-operator")
	if err := os.MkdirAll(filepath.Join(base, "catalog"), 0755); err != nil {
		t.Fatalf("failed to create catalog dir: %v", err)
	}
	if err := os.Symlink(outside, symlinkPath); err != nil {
		t.Fatalf("failed to create symlink: %v", err)
	}

	lifecyclePath := filepath.Join(base, "lifecycle.json")
	lifecycleData := []byte(`{"schema":"io.openshift.operators.lifecycles.v1alpha1"}`)
	if err := os.WriteFile(lifecyclePath, lifecycleData, 0644); err != nil {
		t.Fatalf("failed to write lifecycle.json: %v", err)
	}

	entry := DockerfileCopyEntry{
		Srcs: []string{"catalog"},
		Dest: "/configs",
	}

	_, err := InjectLifecycleJSON(lifecyclePath, base, "evil-operator", entry)
	if err == nil {
		t.Fatal("expected error for symlinked package directory, got nil")
	}
	if !strings.Contains(err.Error(), "symlink") {
		t.Errorf("expected symlink error, got: %v", err)
	}
}

func TestInjectLifecycleJSON_DestTargetsDifferentPackage_ReturnsError(t *testing.T) {
	base := t.TempDir()

	lifecyclePath := filepath.Join(base, "lifecycle.json")
	lifecycleData := []byte(`{"schema":"io.openshift.operators.lifecycles.v1alpha1"}`)
	if err := os.WriteFile(lifecyclePath, lifecycleData, 0644); err != nil {
		t.Fatalf("failed to write lifecycle.json: %v", err)
	}

	entry := DockerfileCopyEntry{
		Srcs: []string{"catalog/other-operator"},
		Dest: "/configs/other-operator",
	}

	_, err := InjectLifecycleJSON(lifecyclePath, base, "my-operator", entry)
	if err == nil {
		t.Fatal("expected error when dest targets a different package, got nil")
	}
	if !strings.Contains(err.Error(), "other-operator") || !strings.Contains(err.Error(), "my-operator") {
		t.Errorf("expected error to mention both package names, got: %v", err)
	}
}

func TestInjectLifecycleJSON_MultipleSources(t *testing.T) {
	buildContext := t.TempDir()

	lifecycleSourcePath := filepath.Join(buildContext, "source-lifecycle.json")
	lifecycleData := []byte(`{"schema":"io.openshift.operators.lifecycles.v1alpha1"}`)
	err := os.WriteFile(lifecycleSourcePath, lifecycleData, 0644)
	if err != nil {
		t.Fatalf("failed to create source lifecycle.json: %v", err)
	}

	srcA := filepath.Join(buildContext, "generated", "my-operator")
	srcB := filepath.Join(buildContext, "manual", "my-operator")

	if err := os.MkdirAll(srcA, 0755); err != nil {
		t.Fatalf("failed to create srcA: %v", err)
	}
	if err := os.MkdirAll(srcB, 0755); err != nil {
		t.Fatalf("failed to create srcB: %v", err)
	}

	entry := DockerfileCopyEntry{
		Srcs: []string{"generated/my-operator", "manual/my-operator"},
		Dest: "/configs/my-operator",
	}

	_, err = InjectLifecycleJSON(lifecycleSourcePath, buildContext, "my-operator", entry)
	if err != nil {
		t.Fatalf("InjectLifecycleJSON returned unexpected error: %v", err)
	}

	if _, err := os.Stat(filepath.Join(srcA, "lifecycle.json")); os.IsNotExist(err) {
		t.Errorf("expected lifecycle.json in first source directory %q, but it was not found", srcA)
	}

	if _, err := os.Stat(filepath.Join(srcB, "lifecycle.json")); os.IsNotExist(err) {
		t.Errorf("expected lifecycle.json in second source directory %q, but it was not found", srcB)
	}
}

func TestInjectLifecycleJSON_DestWithDeepSubPath_ReturnsError(t *testing.T) {
	base := t.TempDir()

	pkgDir := filepath.Join(base, "catalog", "my-operator")
	if err := os.MkdirAll(pkgDir, 0755); err != nil {
		t.Fatalf("failed to create package dir: %v", err)
	}

	lifecyclePath := filepath.Join(base, "lifecycle.json")
	lifecycleData := []byte(`{"schema":"io.openshift.operators.lifecycles.v1alpha1"}`)
	if err := os.WriteFile(lifecyclePath, lifecycleData, 0644); err != nil {
		t.Fatalf("failed to write lifecycle.json: %v", err)
	}

	entry := DockerfileCopyEntry{
		Srcs: []string{"catalog/my-operator"},
		Dest: "/configs/my-operator/subdir",
	}

	_, err := InjectLifecycleJSON(lifecyclePath, base, "my-operator", entry)
	if err == nil {
		t.Fatal("expected error for deep subpath dest, got nil")
	}
	if !strings.Contains(err.Error(), "not a valid FBC path") {
		t.Errorf("expected deep subpath error, got: %v", err)
	}
}

func TestInjectLifecycleJSON_BasenameCoincidence(t *testing.T) {
	buildContext := t.TempDir()

	lifecycleSourcePath := filepath.Join(buildContext, "source-lifecycle.json")
	lifecycleData := []byte(`{"schema":"io.openshift.operators.lifecycles.v1alpha1"}`)
	if err := os.WriteFile(lifecycleSourcePath, lifecycleData, 0644); err != nil {
		t.Fatalf("failed to create source lifecycle.json: %v", err)
	}

	outerCatalogRoot := filepath.Join(buildContext, "catalog", "my-operator")
	innerPackageDir := filepath.Join(outerCatalogRoot, "my-operator")
	if err := os.MkdirAll(innerPackageDir, 0755); err != nil {
		t.Fatalf("failed to create nested package directory: %v", err)
	}

	entry := DockerfileCopyEntry{
		Srcs: []string{"catalog/my-operator"},
		Dest: "/configs",
	}

	_, err := InjectLifecycleJSON(lifecycleSourcePath, buildContext, "my-operator", entry)
	if err != nil {
		t.Fatalf("InjectLifecycleJSON returned unexpected error: %v", err)
	}

	expectedPath := filepath.Join(innerPackageDir, "lifecycle.json")
	if _, err := os.Stat(expectedPath); os.IsNotExist(err) {
		t.Errorf("expected lifecycle.json in package subdirectory %q, but it was not found", expectedPath)
	}

	wrongPath := filepath.Join(outerCatalogRoot, "lifecycle.json")
	if _, err := os.Stat(wrongPath); err == nil {
		t.Errorf("lifecycle.json was incorrectly written to the catalog root due to basename coincidence")
	}
}

func TestInjectLifecycleJSON_LifecycleAlreadyExists_ReturnsError(t *testing.T) {
	base := t.TempDir()

	pkgDir := filepath.Join(base, "catalog", "my-operator")
	if err := os.MkdirAll(pkgDir, 0755); err != nil {
		t.Fatalf("failed to create package dir: %v", err)
	}

	lifecycleData := []byte(`{"schema":"io.openshift.operators.lifecycles.v1alpha1"}`)
	// Pre-create lifecycle.json to simulate duplicate injection
	if err := os.WriteFile(filepath.Join(pkgDir, "lifecycle.json"), lifecycleData, 0644); err != nil {
		t.Fatalf("failed to pre-create lifecycle.json: %v", err)
	}

	lifecyclePath := filepath.Join(base, "source-lifecycle.json")
	if err := os.WriteFile(lifecyclePath, lifecycleData, 0644); err != nil {
		t.Fatalf("failed to write source lifecycle.json: %v", err)
	}

	entry := DockerfileCopyEntry{
		Srcs: []string{"catalog"},
		Dest: "/configs",
	}

	_, err := InjectLifecycleJSON(lifecyclePath, base, "my-operator", entry)
	if err == nil {
		t.Fatal("expected error when lifecycle.json already exists, got nil")
	}
	if !strings.Contains(err.Error(), "already exists") {
		t.Errorf("expected 'already exists' error, got: %v", err)
	}
}

func TestInjectLifecycleJSON_LifecycleSchemaAlreadyExists_JSON_ReturnsError(t *testing.T) {
	base := t.TempDir()

	pkgDir := filepath.Join(base, "catalog", "my-operator")
	if err := os.MkdirAll(pkgDir, 0755); err != nil {
		t.Fatalf("failed to create package dir: %v", err)
	}

	// existing file with lifecycle schema but different name
	existingData := []byte(`{"schema":"io.openshift.operators.lifecycles.v1alpha1"}`)
	if err := os.WriteFile(filepath.Join(pkgDir, "lifecycle-custom.json"), existingData, 0644); err != nil {
		t.Fatalf("failed to write existing lifecycle file: %v", err)
	}

	lifecyclePath := filepath.Join(base, "lifecycle.json")
	if err := os.WriteFile(lifecyclePath, existingData, 0644); err != nil {
		t.Fatalf("failed to write lifecycle.json: %v", err)
	}

	entry := DockerfileCopyEntry{
		Srcs: []string{"catalog"},
		Dest: "/configs",
	}

	_, err := InjectLifecycleJSON(lifecyclePath, base, "my-operator", entry)
	if err == nil {
		t.Fatal("expected error when lifecycle schema already exists, got nil")
	}
}

func TestInjectLifecycleJSON_LifecycleSchemaAlreadyExists_YAML_ReturnsError(t *testing.T) {
	base := t.TempDir()

	pkgDir := filepath.Join(base, "catalog", "my-operator")
	if err := os.MkdirAll(pkgDir, 0755); err != nil {
		t.Fatalf("failed to create package dir: %v", err)
	}

	// existing YAML file with lifecycle schema
	existingData := []byte("schema: io.openshift.operators.lifecycles.v1alpha1\n")
	if err := os.WriteFile(filepath.Join(pkgDir, "lifecycle-custom.yaml"), existingData, 0644); err != nil {
		t.Fatalf("failed to write existing lifecycle yaml file: %v", err)
	}

	lifecycleData := []byte(`{"schema":"io.openshift.operators.lifecycles.v1alpha1"}`)
	lifecyclePath := filepath.Join(base, "lifecycle.json")
	if err := os.WriteFile(lifecyclePath, lifecycleData, 0644); err != nil {
		t.Fatalf("failed to write lifecycle.json: %v", err)
	}

	entry := DockerfileCopyEntry{
		Srcs: []string{"catalog"},
		Dest: "/configs",
	}

	_, err := InjectLifecycleJSON(lifecyclePath, base, "my-operator", entry)
	if err == nil {
		t.Fatal("expected error when lifecycle schema already exists in YAML, got nil")
	}
}

func TestInjectLifecycleJSON_NonLifecycleJSONInDir_DoesNotBlock(t *testing.T) {
	base := t.TempDir()

	pkgDir := filepath.Join(base, "catalog", "my-operator")
	if err := os.MkdirAll(pkgDir, 0755); err != nil {
		t.Fatalf("failed to create package dir: %v", err)
	}

	// existing catalog.json with different schema — should not block injection
	catalogData := []byte(`{"schema":"olm.package","name":"my-operator"}`)
	if err := os.WriteFile(filepath.Join(pkgDir, "catalog.json"), catalogData, 0644); err != nil {
		t.Fatalf("failed to write catalog.json: %v", err)
	}

	lifecycleData := []byte(`{"schema":"io.openshift.operators.lifecycles.v1alpha1"}`)
	lifecyclePath := filepath.Join(base, "lifecycle.json")
	if err := os.WriteFile(lifecyclePath, lifecycleData, 0644); err != nil {
		t.Fatalf("failed to write lifecycle.json: %v", err)
	}

	entry := DockerfileCopyEntry{
		Srcs: []string{"catalog"},
		Dest: "/configs",
	}

	wasInjected, err := InjectLifecycleJSON(lifecyclePath, base, "my-operator", entry)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !wasInjected {
		t.Fatal("expected lifecycle.json to be injected, got wasInjected=false")
	}
}

func TestInjectLifecycleJSON_LifecycleSchemaInCatalogFile_ReturnsError(t *testing.T) {
	base := t.TempDir()

	pkgDir := filepath.Join(base, "catalog", "my-operator")
	if err := os.MkdirAll(pkgDir, 0755); err != nil {
		t.Fatalf("failed to create package dir: %v", err)
	}

	// lifecycle schema embedded in catalog.json
	catalogData := []byte(`{"schema":"io.openshift.operators.lifecycles.v1alpha1","package":"my-operator"}`)
	if err := os.WriteFile(filepath.Join(pkgDir, "catalog.json"), catalogData, 0644); err != nil {
		t.Fatalf("failed to write catalog.json: %v", err)
	}

	lifecycleData := []byte(`{"schema":"io.openshift.operators.lifecycles.v1alpha1"}`)
	lifecyclePath := filepath.Join(base, "lifecycle.json")
	if err := os.WriteFile(lifecyclePath, lifecycleData, 0644); err != nil {
		t.Fatalf("failed to write lifecycle.json: %v", err)
	}

	entry := DockerfileCopyEntry{
		Srcs: []string{"catalog"},
		Dest: "/configs",
	}

	_, err := InjectLifecycleJSON(lifecyclePath, base, "my-operator", entry)
	if err == nil {
		t.Fatal("expected error when lifecycle schema exists in catalog.json, got nil")
	}
}

func TestHasLifecycleSchema_ValidJSON(t *testing.T) {
	data := []byte(`{"schema":"io.openshift.operators.lifecycles.v1alpha1"}`)
	if !hasLifecycleSchema(data) {
		t.Error("expected true for valid lifecycle schema JSON, got false")
	}
}

func TestHasLifecycleSchema_ValidYAML(t *testing.T) {
	data := []byte("schema: io.openshift.operators.lifecycles.v1alpha1\n")
	if !hasLifecycleSchema(data) {
		t.Error("expected true for valid lifecycle schema YAML, got false")
	}
}

func TestHasLifecycleSchema_WrongSchema(t *testing.T) {
	data := []byte(`{"schema":"olm.package"}`)
	if hasLifecycleSchema(data) {
		t.Error("expected false for wrong schema, got true")
	}
}

func TestHasLifecycleSchema_InvalidData(t *testing.T) {
	data := []byte(`not valid json or yaml }{`)
	if hasLifecycleSchema(data) {
		t.Error("expected false for invalid data, got true")
	}
}
