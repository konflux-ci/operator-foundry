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
	"fmt"
	"strings"
	"time"
)

type Result string

const (
	ResultSuccess Result = "SUCCESS"
	ResultFailure Result = "FAILURE"
	ResultError   Result = "ERROR"
	ResultWarning Result = "WARNING"
	ResultSkipped Result = "SKIPPED"

	maxTektonResultSize = 4096
)

type TestOutput struct {
	Result    Result `json:"result"`
	Timestamp string `json:"timestamp"`
	Note      string `json:"note"`
	Namespace string `json:"namespace"`
	Successes int    `json:"successes"`
	Failures  int    `json:"failures"`
	Warnings  int    `json:"warnings"`
}

type Option func(*TestOutput)

func WithSuccesses(n int) Option {
	return func(o *TestOutput) { o.Successes = n }
}

func WithFailures(n int) Option {
	return func(o *TestOutput) { o.Failures = n }
}

func WithWarnings(n int) Option {
	return func(o *TestOutput) { o.Warnings = n }
}

func WithNote(note string) Option {
	return func(o *TestOutput) { o.Note = note }
}

func WithNamespace(ns string) Option {
	return func(o *TestOutput) { o.Namespace = ns }
}

func MakeResultJSON(result Result, opts ...Option) (string, error) {
	switch result {
	case ResultSuccess, ResultFailure, ResultError, ResultWarning, ResultSkipped:
	default:
		return "", fmt.Errorf("invalid result value %q: must be one of SUCCESS, FAILURE, ERROR, WARNING, SKIPPED", result)
	}

	o := &TestOutput{
		Result:    result,
		Timestamp: time.Now().UTC().Format(time.RFC3339),
		Note:      "For details, check Tekton task log.",
		Namespace: "default",
	}

	for _, opt := range opts {
		opt(o)
	}

	var negativeFields []string
	if o.Successes < 0 {
		negativeFields = append(negativeFields, "successes")
	}
	if o.Failures < 0 {
		negativeFields = append(negativeFields, "failures")
	}
	if o.Warnings < 0 {
		negativeFields = append(negativeFields, "warnings")
	}
	if len(negativeFields) > 0 {
		return "", fmt.Errorf("fields must not be negative: %s", strings.Join(negativeFields, ", "))
	}

	b, err := json.Marshal(o)
	if err != nil {
		return "", fmt.Errorf("failed to marshal TEST_OUTPUT: %w", err)
	}

	if len(b) > maxTektonResultSize {
		return "", fmt.Errorf("TEST_OUTPUT exceeds maximum Tekton result size of %d bytes (got %d)", maxTektonResultSize, len(b))
	}

	return string(b), nil
}
