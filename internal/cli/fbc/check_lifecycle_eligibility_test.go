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

import "testing"

func TestParseBuildArgs_Empty(t *testing.T) {
	got, err := parseBuildArgs(nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != nil {
		t.Errorf("got %v, want nil", got)
	}
}

func TestParseBuildArgs_SingleKeyValue(t *testing.T) {
	got, err := parseBuildArgs([]string{"CATALOG_VERSION=v4.15"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got["CATALOG_VERSION"] != "v4.15" {
		t.Errorf("got %v, want CATALOG_VERSION=v4.15", got)
	}
}

func TestParseBuildArgs_EmptyValue(t *testing.T) {
	got, err := parseBuildArgs([]string{"CATALOG_VERSION="})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if v, ok := got["CATALOG_VERSION"]; !ok || v != "" {
		t.Errorf("got %v, want CATALOG_VERSION=\"\"", got)
	}
}

func TestParseBuildArgs_ValueContainingEquals(t *testing.T) {
	got, err := parseBuildArgs([]string{"LABEL=key=value"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got["LABEL"] != "key=value" {
		t.Errorf("got %v, want LABEL=key=value", got)
	}
}

func TestParseBuildArgs_DuplicateKey_LastWins(t *testing.T) {
	got, err := parseBuildArgs([]string{"CATALOG_VERSION=v4.15", "CATALOG_VERSION=v5.0"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got["CATALOG_VERSION"] != "v5.0" {
		t.Errorf("got %v, want CATALOG_VERSION=v5.0 (last wins)", got)
	}
}

func TestParseBuildArgs_MissingEquals_ReturnsError(t *testing.T) {
	_, err := parseBuildArgs([]string{"CATALOG_VERSION"})
	if err == nil {
		t.Fatal("expected error for missing '=', got nil")
	}
}

func TestParseBuildArgs_EmptyKey_ReturnsError(t *testing.T) {
	_, err := parseBuildArgs([]string{"=v4.15"})
	if err == nil {
		t.Fatal("expected error for empty key, got nil")
	}
}

func TestParseBuildArgs_MultipleFlags(t *testing.T) {
	got, err := parseBuildArgs([]string{"A=1", "B=2"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got["A"] != "1" || got["B"] != "2" {
		t.Errorf("got %v, want A=1 B=2", got)
	}
}
