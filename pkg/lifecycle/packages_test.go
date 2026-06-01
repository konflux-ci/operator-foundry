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

// ── ExtractPackageNames ───────────────────────────────────────────────────────

// Option A tests

func TestExtractPackageNames_OptionA_SinglePackage(t *testing.T) {
	entries := []DockerfileCopyEntry{
		{Srcs: []string{"./catalog"}, Dest: "/configs/my-operator"},
	}
	pkgs, err := ExtractPackageNames(entries, "/workspace/source")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(pkgs) != 1 || pkgs[0] != "my-operator" {
		t.Errorf("got %v, want [my-operator]", pkgs)
	}
}

func TestExtractPackageNames_OptionA_MultiplePackages(t *testing.T) {
	entries := []DockerfileCopyEntry{
		{Srcs: []string{"./catalog-a"}, Dest: "/configs/operator-a"},
		{Srcs: []string{"./catalog-b"}, Dest: "/configs/operator-b"},
	}
	pkgs, err := ExtractPackageNames(entries, "/workspace/source")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(pkgs) != 2 {
		t.Fatalf("got %v, want [operator-a operator-b]", pkgs)
	}
}

func TestExtractPackageNames_OptionA_Deduplicates(t *testing.T) {
	entries := []DockerfileCopyEntry{
		{Srcs: []string{"./catalog.yaml"}, Dest: "/configs/my-operator"},
		{Srcs: []string{"./channel.yaml"}, Dest: "/configs/my-operator"},
	}
	pkgs, err := ExtractPackageNames(entries, "/workspace/source")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(pkgs) != 1 || pkgs[0] != "my-operator" {
		t.Errorf("got %v, want [my-operator]", pkgs)
	}
}

func TestExtractPackageNames_OptionA_RelativeDest(t *testing.T) {
	// Dest without a leading slash — should still be recognized as a configs/ path.
	entries := []DockerfileCopyEntry{
		{Srcs: []string{"./catalog"}, Dest: "configs/my-operator"},
	}
	pkgs, err := ExtractPackageNames(entries, "/workspace/source")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(pkgs) != 1 || pkgs[0] != "my-operator" {
		t.Errorf("got %v, want [my-operator]", pkgs)
	}
}

// Option B tests

func TestExtractPackageNames_OptionB_SinglePackage(t *testing.T) {
	// catalog/
	//     my-operator/     ← subdir name is the package name
	base := t.TempDir()
	if err := os.MkdirAll(filepath.Join(base, "catalog", "my-operator"), 0755); err != nil {
		t.Fatalf("failed to create dirs: %v", err)
	}
	entries := []DockerfileCopyEntry{
		{Srcs: []string{"catalog"}, Dest: "/configs"},
	}
	pkgs, err := ExtractPackageNames(entries, base)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(pkgs) != 1 || pkgs[0] != "my-operator" {
		t.Errorf("got %v, want [my-operator]", pkgs)
	}
}

func TestExtractPackageNames_OptionB_MultiplePackages(t *testing.T) {
	// catalog/
	//     operator-a/
	//     operator-b/
	base := t.TempDir()
	for _, pkg := range []string{"operator-a", "operator-b"} {
		if err := os.MkdirAll(filepath.Join(base, "catalog", pkg), 0755); err != nil {
			t.Fatalf("failed to create dirs: %v", err)
		}
	}
	entries := []DockerfileCopyEntry{
		{Srcs: []string{"catalog"}, Dest: "/configs"},
	}
	pkgs, err := ExtractPackageNames(entries, base)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(pkgs) != 2 {
		t.Errorf("got %v, want [operator-a operator-b]", pkgs)
	}
}

func TestExtractPackageNames_OptionB_FilesInCatalogDirIgnored(t *testing.T) {
	// Loose files at the catalog root are ignored; only subdirs count as package names.
	// catalog/
	//     my-operator/
	//     README.md     ← must be ignored
	base := t.TempDir()
	if err := os.MkdirAll(filepath.Join(base, "catalog", "my-operator"), 0755); err != nil {
		t.Fatalf("failed to create dirs: %v", err)
	}
	if err := os.WriteFile(filepath.Join(base, "catalog", "README.md"), []byte("docs"), 0644); err != nil {
		t.Fatalf("failed to write file: %v", err)
	}
	entries := []DockerfileCopyEntry{
		{Srcs: []string{"catalog"}, Dest: "/configs"},
	}
	pkgs, err := ExtractPackageNames(entries, base)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(pkgs) != 1 || pkgs[0] != "my-operator" {
		t.Errorf("got %v, want [my-operator]", pkgs)
	}
}

func TestExtractPackageNames_OptionB_BuildStageEntriesSkipped(t *testing.T) {
	// COPY --from=builder entries have no local source directories and must be skipped.
	base := t.TempDir()
	if err := os.MkdirAll(filepath.Join(base, "catalog", "my-operator"), 0755); err != nil {
		t.Fatalf("failed to create dirs: %v", err)
	}
	entries := []DockerfileCopyEntry{
		{Srcs: []string{"/opt/catalog"}, Dest: "/configs", From: "builder"},
		{Srcs: []string{"catalog"}, Dest: "/configs"},
	}
	pkgs, err := ExtractPackageNames(entries, base)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(pkgs) != 1 || pkgs[0] != "my-operator" {
		t.Errorf("got %v, want [my-operator]", pkgs)
	}
}

func TestExtractPackageNames_OptionB_FileSourceSkipped(t *testing.T) {
	// A file COPY entry before a directory COPY entry must not cause an error.
	// catalog/
	//     my-operator/
	// my_file.yaml  ← file source, must be skipped
	base := t.TempDir()
	if err := os.MkdirAll(filepath.Join(base, "catalog", "my-operator"), 0755); err != nil {
		t.Fatalf("failed to create dirs: %v", err)
	}
	if err := os.WriteFile(filepath.Join(base, "my_file.yaml"), []byte("data"), 0644); err != nil {
		t.Fatalf("failed to write file: %v", err)
	}
	entries := []DockerfileCopyEntry{
		{Srcs: []string{"my_file.yaml"}, Dest: "/configs"},
		{Srcs: []string{"catalog"}, Dest: "/configs"},
	}
	pkgs, err := ExtractPackageNames(entries, base)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(pkgs) != 1 || pkgs[0] != "my-operator" {
		t.Errorf("got %v, want [my-operator]", pkgs)
	}
}

func TestExtractPackageNames_OptionB_HiddenDirsIgnored(t *testing.T) {
	// Hidden directories (e.g. .git) inside the catalog dir must be ignored.
	// catalog/
	//     my-operator/
	//     .git/         ← must be ignored
	base := t.TempDir()
	if err := os.MkdirAll(filepath.Join(base, "catalog", "my-operator"), 0755); err != nil {
		t.Fatalf("failed to create dirs: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(base, "catalog", ".git"), 0755); err != nil {
		t.Fatalf("failed to create dirs: %v", err)
	}
	entries := []DockerfileCopyEntry{
		{Srcs: []string{"catalog"}, Dest: "/configs"},
	}
	pkgs, err := ExtractPackageNames(entries, base)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(pkgs) != 1 || pkgs[0] != "my-operator" {
		t.Errorf("got %v, want [my-operator]", pkgs)
	}
}

func TestExtractPackageNames_OptionB_AbsoluteSourcePath_ReturnsError(t *testing.T) {
	entries := []DockerfileCopyEntry{
		{Srcs: []string{"/etc"}, Dest: "/configs"},
	}
	_, err := ExtractPackageNames(entries, t.TempDir())
	if err == nil {
		t.Fatal("expected error for absolute source path, got nil")
	}
}

func TestExtractPackageNames_OptionB_TraversalSourcePath_ReturnsError(t *testing.T) {
	entries := []DockerfileCopyEntry{
		{Srcs: []string{"../../etc"}, Dest: "/configs"},
	}
	_, err := ExtractPackageNames(entries, t.TempDir())
	if err == nil {
		t.Fatal("expected error for traversal source path, got nil")
	}
}

func TestExtractPackageNames_AllBuildStageEntries_ReturnsError(t *testing.T) {
	// All entries are COPY --from=builder — no local source directories to scan.
	// Option A finds nothing (Dest is /configs), Option B has nothing to scan.
	// Must return an error.
	entries := []DockerfileCopyEntry{
		{Srcs: []string{"/opt/catalog"}, Dest: "/configs", From: "builder"},
	}
	_, err := ExtractPackageNames(entries, t.TempDir())
	if err == nil {
		t.Fatal("expected error when all entries are from build stages, got nil")
	}
}

func TestExtractPackageNames_NoPackagesFound_ReturnsError(t *testing.T) {
	// Neither Option A nor Option B finds any package names — must return an error.
	// catalog dir exists but has no subdirectories.
	base := t.TempDir()
	if err := os.MkdirAll(filepath.Join(base, "catalog"), 0755); err != nil {
		t.Fatalf("failed to create dirs: %v", err)
	}
	entries := []DockerfileCopyEntry{
		{Srcs: []string{"catalog"}, Dest: "/configs"},
	}
	_, err := ExtractPackageNames(entries, base)
	if err == nil {
		t.Fatal("expected error when no package names found, got nil")
	}
}

func TestExtractPackageNames_UnreadableCatalogDir_ReturnsError(t *testing.T) {
	// Option B must propagate an error if the catalog directory cannot be read.
	base := t.TempDir()
	if err := os.MkdirAll(filepath.Join(base, "catalog"), 0755); err != nil {
		t.Fatalf("failed to create dirs: %v", err)
	}
	entries := []DockerfileCopyEntry{
		{Srcs: []string{"does-not-exist"}, Dest: "/configs"},
	}
	_, err := ExtractPackageNames(entries, base)
	if err == nil {
		t.Fatal("expected error for missing catalog directory, got nil")
	}
}

// ── ResolveAndValidatePath ───────────────────────────────────────────────────────

func TestResolveAndValidatePath_ValidSubPath(t *testing.T) {
	base := t.TempDir()
	resolved, err := resolveAndValidatePath(base, "catalog/my-operator")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resolved != filepath.Join(base, "catalog/my-operator") {
		t.Errorf("got %q, want %q", resolved, filepath.Join(base, "catalog/my-operator"))
	}
}

func TestResolveAndValidatePath_TraversalEscapes_ReturnsError(t *testing.T) {
	base := t.TempDir()
	_, err := resolveAndValidatePath(base, "../../etc/shadow")
	if err == nil {
		t.Fatal("expected error for path traversal, got nil")
	}
}

func TestResolveAndValidatePath_AbsoluteSubPath_ReturnsError(t *testing.T) {
	base := t.TempDir()
	_, err := resolveAndValidatePath(base, "/etc/shadow")
	if err == nil {
		t.Fatal("expected error for absolute sub path, got nil")
	}
}

func TestResolveAndValidatePath_SameAsBaseContext(t *testing.T) {
	base := t.TempDir()
	resolved, err := resolveAndValidatePath(base, ".")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resolved != filepath.Clean(base) {
		t.Errorf("got %q, want %q", resolved, filepath.Clean(base))
	}
}
