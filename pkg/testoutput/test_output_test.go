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

package testoutput

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestMakeResultJSON_ValidResult_Defaults(t *testing.T) {
	out, err := MakeResultJSON(ResultSuccess)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var result TestOutput
	if err := json.Unmarshal([]byte(out), &result); err != nil {
		t.Fatalf("failed to unmarshal output: %v", err)
	}

	if result.Result != ResultSuccess {
		t.Errorf("got result %q, want %q", result.Result, ResultSuccess)
	}
	if result.Note != "For details, check Tekton task log." {
		t.Errorf("got note %q, want default note", result.Note)
	}
	if result.Namespace != "default" {
		t.Errorf("got namespace %q, want default", result.Namespace)
	}
	if result.Successes != 0 || result.Failures != 0 || result.Warnings != 0 {
		t.Errorf("expected zero counts, got successes=%d failures=%d warnings=%d",
			result.Successes, result.Failures, result.Warnings)
	}
}

func TestMakeResultJSON_AllOptions(t *testing.T) {
	out, err := MakeResultJSON(ResultFailure,
		WithSuccesses(5),
		WithFailures(3),
		WithWarnings(1),
		WithNote("custom note"),
		WithNamespace("my-namespace"),
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var result TestOutput
	if err := json.Unmarshal([]byte(out), &result); err != nil {
		t.Fatalf("failed to unmarshal output: %v", err)
	}

	if result.Result != ResultFailure {
		t.Errorf("got result %q, want %q", result.Result, ResultFailure)
	}
	if result.Successes != 5 {
		t.Errorf("got successes %d, want 5", result.Successes)
	}
	if result.Failures != 3 {
		t.Errorf("got failures %d, want 3", result.Failures)
	}
	if result.Warnings != 1 {
		t.Errorf("got warnings %d, want 1", result.Warnings)
	}
	if result.Note != "custom note" {
		t.Errorf("got note %q, want custom note", result.Note)
	}
	if result.Namespace != "my-namespace" {
		t.Errorf("got namespace %q, want my-namespace", result.Namespace)
	}
}

func TestMakeResultJSON_InvalidResult_ReturnsError(t *testing.T) {
	_, err := MakeResultJSON("INVALID")
	if err == nil {
		t.Fatal("expected error for invalid result, got nil")
	}
}

func TestMakeResultJSON_AllValidResults(t *testing.T) {
	for _, result := range []Result{ResultSuccess, ResultFailure, ResultError, ResultWarning, ResultSkipped} {
		_, err := MakeResultJSON(result)
		if err != nil {
			t.Errorf("expected no error for result %q, got: %v", result, err)
		}
	}
}

func TestMakeResultJSON_TimestampIsValidRFC3339(t *testing.T) {
	out, err := MakeResultJSON(ResultSuccess)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var result TestOutput
	if err := json.Unmarshal([]byte(out), &result); err != nil {
		t.Fatalf("failed to unmarshal output: %v", err)
	}

	if _, err := time.Parse(time.RFC3339, result.Timestamp); err != nil {
		t.Errorf("timestamp %q is not valid RFC3339: %v", result.Timestamp, err)
	}
}

func TestMakeResultJSON_OutputIsValidJSON(t *testing.T) {
	out, err := MakeResultJSON(ResultSuccess)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var raw map[string]interface{}
	if err := json.Unmarshal([]byte(out), &raw); err != nil {
		t.Fatalf("output is not valid JSON: %v", err)
	}

	for _, field := range []string{"result", "timestamp", "note", "namespace", "successes", "failures", "warnings"} {
		if _, ok := raw[field]; !ok {
			t.Errorf("missing field %q in output", field)
		}
	}
}

func TestMakeResultJSON_NegativeSuccesses_ReturnsError(t *testing.T) {
	_, err := MakeResultJSON(ResultSuccess, WithSuccesses(-1))
	if err == nil {
		t.Fatal("expected error for negative successes, got nil")
	}
	if !strings.Contains(err.Error(), "successes") {
		t.Errorf("expected error to mention 'successes', got: %v", err)
	}
}

func TestMakeResultJSON_NegativeFailures_ReturnsError(t *testing.T) {
	_, err := MakeResultJSON(ResultSuccess, WithFailures(-1))
	if err == nil {
		t.Fatal("expected error for negative failures, got nil")
	}
	if !strings.Contains(err.Error(), "failures") {
		t.Errorf("expected error to mention 'failures', got: %v", err)
	}
}

func TestMakeResultJSON_NegativeWarnings_ReturnsError(t *testing.T) {
	_, err := MakeResultJSON(ResultSuccess, WithWarnings(-1))
	if err == nil {
		t.Fatal("expected error for negative warnings, got nil")
	}
	if !strings.Contains(err.Error(), "warnings") {
		t.Errorf("expected error to mention 'warnings', got: %v", err)
	}
}

func TestMakeResultJSON_ExceedsMaxTektonResultSize_ReturnsError(t *testing.T) {
	note := strings.Repeat("a", maxTektonResultSize)
	_, err := MakeResultJSON(ResultSuccess, WithNote(note))
	if err == nil {
		t.Fatal("expected error for oversized result, got nil")
	}
	if !strings.Contains(err.Error(), "exceeds maximum") {
		t.Errorf("expected error to mention size limit, got: %v", err)
	}
}
