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
	"testing"

	"github.com/keilerkonzept/dockerfile-json/pkg/dockerfile"
)

func mustParseDockerfile(t *testing.T, src string) *dockerfile.Dockerfile {
	t.Helper()
	f, err := os.CreateTemp("", "Dockerfile")
	if err != nil {
		t.Fatalf("failed to create temp file: %v", err)
	}
	t.Cleanup(func() {
		if err := os.Remove(f.Name()); err != nil {
			t.Logf("failed to remove temp file: %v", err)
		}
	})
	if _, err := f.WriteString(src); err != nil {
		t.Fatalf("failed to write temp Dockerfile: %v", err)
	}
	if err := f.Close(); err != nil {
		t.Fatalf("failed to close temp Dockerfile: %v", err)
	}
	d, err := dockerfile.Parse(f.Name())
	if err != nil {
		t.Fatalf("failed to parse test Dockerfile: %v", err)
	}
	return d
}

// ── ParseCopyInstructionsForConfigs ───────────────────────────────────────────

func TestParseCopyInstructionsForConfigs_NilDockerfile(t *testing.T) {
	_, err := ParseCopyInstructionsForConfigs(nil)
	if err == nil {
		t.Fatal("expected error for nil Dockerfile, got nil")
	}
}

func TestParseCopyInstructionsForConfigs_SingleADD(t *testing.T) {
	d := mustParseDockerfile(t, `
FROM ubuntu
ADD catalog /configs
`)
	entries, err := ParseCopyInstructionsForConfigs(d)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(entries) != 1 || entries[0].Srcs[0] != "catalog" || entries[0].Dest != "/configs" {
		t.Errorf("got %+v, want [{[catalog] /configs }]", entries)
	}
}

func TestParseCopyInstructionsForConfigs_SingleCOPY(t *testing.T) {
	d := mustParseDockerfile(t, `
FROM ubuntu
COPY ./catalog /configs/package-a
`)
	entries, err := ParseCopyInstructionsForConfigs(d)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(entries) != 1 || entries[0].Srcs[0] != "./catalog" || entries[0].Dest != "/configs/package-a" {
		t.Errorf("got %+v", entries)
	}
}

func TestParseCopyInstructionsForConfigs_MultipleSources(t *testing.T) {
	d := mustParseDockerfile(t, `
FROM ubuntu
COPY catalog.yaml channel.yaml /configs/my-operator/
`)
	entries, err := ParseCopyInstructionsForConfigs(d)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("got %d entries, want 1", len(entries))
	}
	if len(entries[0].Srcs) != 2 {
		t.Errorf("got %d srcs, want 2", len(entries[0].Srcs))
	}
	if entries[0].Srcs[0] != "catalog.yaml" || entries[0].Srcs[1] != "channel.yaml" {
		t.Errorf("got srcs %v", entries[0].Srcs)
	}
}

func TestParseCopyInstructionsForConfigs_MultipleCOPY(t *testing.T) {
	d := mustParseDockerfile(t, `
FROM ubuntu
COPY ./catalog-package-a /configs/package-a
COPY ./catalog-package-b /configs/package-b
`)
	entries, err := ParseCopyInstructionsForConfigs(d)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(entries) != 2 {
		t.Errorf("got %d entries, want 2", len(entries))
	}
}

func TestParseCopyInstructionsForConfigs_COPYFromBuilder(t *testing.T) {
	d := mustParseDockerfile(t, `
FROM ubuntu AS builder
RUN make catalog


FROM ubuntu
COPY --from=builder /opt/app-root/src/catalog /configs/netobserv-operator
`)
	entries, err := ParseCopyInstructionsForConfigs(d)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("got %d entries, want 1", len(entries))
	}
	if entries[0].Srcs[0] != "/opt/app-root/src/catalog" {
		t.Errorf("Src = %q", entries[0].Srcs[0])
	}
	if entries[0].From != "builder" {
		t.Errorf("From = %q, want builder", entries[0].From)
	}
	if !entries[0].IsFromBuildStage() {
		t.Error("expected IsFromBuildStage() = true")
	}
}

func TestParseCopyInstructionsForConfigs_COPYFromBuilderWithVariable(t *testing.T) {
	d := mustParseDockerfile(t, `
FROM ubuntu AS builder-v1
RUN make catalog


FROM ubuntu
ARG BUILDER=builder-v1
COPY --from=$BUILDER /opt/catalog /configs/my-operator
`)
	entries, err := ParseCopyInstructionsForConfigs(d)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if entries[0].From != "builder-v1" {
		t.Errorf("From = %q, want builder-v1", entries[0].From)
	}
}

func TestParseCopyInstructionsForConfigs_ENVVariableResolved(t *testing.T) {
	d := mustParseDockerfile(t, `
FROM ubuntu
ENV PACKAGE_NAME=lifecycle-agent
COPY .konflux/catalog/$PACKAGE_NAME /configs/$PACKAGE_NAME
`)
	entries, err := ParseCopyInstructionsForConfigs(d)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if entries[0].Srcs[0] != ".konflux/catalog/lifecycle-agent" {
		t.Errorf("Src = %q, want .konflux/catalog/lifecycle-agent", entries[0].Srcs[0])
	}
	if entries[0].Dest != "/configs/lifecycle-agent" {
		t.Errorf("Dest = %q, want /configs/lifecycle-agent", entries[0].Dest)
	}
}

func TestParseCopyInstructionsForConfigs_ARGVariableWithDefault(t *testing.T) {
	d := mustParseDockerfile(t, `
FROM ubuntu
ARG PACKAGE_NAME=lifecycle-agent
COPY .konflux/catalog/$PACKAGE_NAME /configs/$PACKAGE_NAME
`)
	entries, err := ParseCopyInstructionsForConfigs(d)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if entries[0].Srcs[0] != ".konflux/catalog/lifecycle-agent" {
		t.Errorf("Src = %q, want .konflux/catalog/lifecycle-agent", entries[0].Srcs[0])
	}
	if entries[0].Dest != "/configs/lifecycle-agent" {
		t.Errorf("Dest = %q, want /configs/lifecycle-agent", entries[0].Dest)
	}
}

func TestParseCopyInstructionsForConfigs_GlobalARGResolvedWithReDeclaration(t *testing.T) {
	// global ARG re-declared inside the stage — should resolve correctly
	d := mustParseDockerfile(t, `
ARG PACKAGE_NAME=lifecycle-agent
FROM ubuntu
ARG PACKAGE_NAME
COPY .konflux/catalog/$PACKAGE_NAME /configs/$PACKAGE_NAME
`)
	entries, err := ParseCopyInstructionsForConfigs(d)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if entries[0].Srcs[0] != ".konflux/catalog/lifecycle-agent" {
		t.Errorf("Src = %q, want .konflux/catalog/lifecycle-agent", entries[0].Srcs[0])
	}
	if entries[0].Dest != "/configs/lifecycle-agent" {
		t.Errorf("Dest = %q, want /configs/lifecycle-agent", entries[0].Dest)
	}
}

func TestParseCopyInstructionsForConfigs_VariableShadowingAcrossStages(t *testing.T) {
	d := mustParseDockerfile(t, `
FROM ubuntu AS stage-a
ENV PACKAGE_NAME=operator-a
COPY catalog /configs/$PACKAGE_NAME


FROM ubuntu AS stage-b
ENV PACKAGE_NAME=operator-b
COPY catalog /configs/$PACKAGE_NAME
`)
	entries, err := ParseCopyInstructionsForConfigs(d)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("got %d entries, want 2", len(entries))
	}
	if entries[0].Dest != "/configs/operator-a" {
		t.Errorf("stage 1 Dest = %q, want /configs/operator-a", entries[0].Dest)
	}
	if entries[1].Dest != "/configs/operator-b" {
		t.Errorf("stage 2 Dest = %q, want /configs/operator-b", entries[1].Dest)
	}
}

func TestParseCopyInstructionsForConfigs_UnresolvedVariable_ResolvesToEmpty(t *testing.T) {
	d := mustParseDockerfile(t, `
FROM ubuntu
COPY .konflux/catalog/$UNKNOWN_VAR /configs/$UNKNOWN_VAR
`)
	entries, err := ParseCopyInstructionsForConfigs(d)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if entries[0].Srcs[0] != ".konflux/catalog/" {
		t.Errorf("Src = %q, want .konflux/catalog/", entries[0].Srcs[0])
	}
	if entries[0].Dest != "/configs/" {
		t.Errorf("Dest = %q, want /configs/", entries[0].Dest)
	}
}

func TestParseCopyInstructionsForConfigs_FalsePositivePrevented(t *testing.T) {
	d := mustParseDockerfile(t, `
FROM ubuntu
COPY backup /configs-backup
`)
	_, err := ParseCopyInstructionsForConfigs(d)
	if err == nil {
		t.Fatal("expected error — /configs-backup should not match /configs")
	}
}

func TestParseCopyInstructionsForConfigs_MixedADDAndCOPY(t *testing.T) {
	d := mustParseDockerfile(t, `
FROM ubuntu
ADD catalog /configs
COPY ./catalog-package-b /configs/package-b
`)
	entries, err := ParseCopyInstructionsForConfigs(d)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(entries) != 2 {
		t.Errorf("got %d entries, want 2", len(entries))
	}
}

func TestParseCopyInstructionsForConfigs_NoConfigsTarget(t *testing.T) {
	d := mustParseDockerfile(t, `
FROM ubuntu
ADD catalog /other
`)
	_, err := ParseCopyInstructionsForConfigs(d)
	if err == nil {
		t.Fatal("expected error when no ADD/COPY targets /configs, got nil")
	}
}

func TestParseCopyInstructionsForConfigs_EmptyCommands(t *testing.T) {
	d := mustParseDockerfile(t, `
FROM ubuntu
`)
	_, err := ParseCopyInstructionsForConfigs(d)
	if err == nil {
		t.Fatal("expected error for Dockerfile with no COPY/ADD, got nil")
	}
}

func TestParseCopyInstructionsForConfigs_WildcardSourcePath_ReturnsError(t *testing.T) {
	d := mustParseDockerfile(t, `
FROM ubuntu
COPY catalog/* /configs/my-operator/
`)
	_, err := ParseCopyInstructionsForConfigs(d)
	if err == nil {
		t.Fatal("expected error for wildcard source path, got nil")
	}
}

func TestParseCopyInstructionsForConfigs_VariableDeclaredAfterCOPY_ResolvesToEmpty(t *testing.T) {
	// ENV declared AFTER the COPY — must NOT be resolved
	d := mustParseDockerfile(t, `
FROM ubuntu
COPY .konflux/catalog/$PACKAGE_NAME /configs/$PACKAGE_NAME
ENV PACKAGE_NAME=lifecycle-agent
`)
	entries, err := ParseCopyInstructionsForConfigs(d)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if entries[0].Srcs[0] != ".konflux/catalog/" {
		t.Errorf("Src = %q, want .konflux/catalog/ — variable declared after COPY should not resolve", entries[0].Srcs[0])
	}
	if entries[0].Dest != "/configs/" {
		t.Errorf("Dest = %q, want /configs/ — variable declared after COPY should not resolve", entries[0].Dest)
	}
}

func TestParseCopyInstructionsForConfigs_ARGCannotOverrideENV(t *testing.T) {
	// ENV declared BEFORE ARG with same name — ENV must win
	d := mustParseDockerfile(t, `
FROM ubuntu
ENV PACKAGE_NAME=env-value
ARG PACKAGE_NAME=arg-value
COPY .konflux/catalog/$PACKAGE_NAME /configs/$PACKAGE_NAME
`)
	entries, err := ParseCopyInstructionsForConfigs(d)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if entries[0].Srcs[0] != ".konflux/catalog/env-value" {
		t.Errorf("Src = %q, want .konflux/catalog/env-value — ENV should take precedence over ARG", entries[0].Srcs[0])
	}
	if entries[0].Dest != "/configs/env-value" {
		t.Errorf("Dest = %q, want /configs/env-value — ENV should take precedence over ARG", entries[0].Dest)
	}
}

// ── buildGlobalArgMap ─────────────────────────────────────────────────────────

func TestBuildGlobalArgMap_WithDefault(t *testing.T) {
	d := mustParseDockerfile(t, `
ARG PACKAGE_NAME=lifecycle-agent
FROM ubuntu
`)
	globalArgs := buildGlobalArgMap(d)
	if globalArgs["PACKAGE_NAME"] != "lifecycle-agent" {
		t.Errorf("got %q, want lifecycle-agent", globalArgs["PACKAGE_NAME"])
	}
}

func TestBuildGlobalArgMap_WithoutDefault(t *testing.T) {
	d := mustParseDockerfile(t, `
ARG PACKAGE_NAME
FROM ubuntu
`)
	globalArgs := buildGlobalArgMap(d)
	if _, ok := globalArgs["PACKAGE_NAME"]; ok {
		t.Error("ARG without default should not be in global args map")
	}
}

func TestBuildGlobalArgMap_NoMetaArgs(t *testing.T) {
	d := mustParseDockerfile(t, `
FROM ubuntu
`)
	globalArgs := buildGlobalArgMap(d)
	if len(globalArgs) != 0 {
		t.Errorf("got %d args, want 0", len(globalArgs))
	}
}
