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

package catalog

import (
	"encoding/json"
	"os"
	"reflect"
	"strings"
	"testing"
)

// Every object reaches the consumer individually with its fields and input order intact.
// 1. Write indented A/B/C records, including nested extra fields and a custom schema, to a file.
// 2. Read the file through the shared parser and inspect each callback separately.
// 3. Verify three deliveries and normal completion; repeat A/B/A to preserve duplicate records.
func TestReadCatalog_Records(t *testing.T) {
	a := `{
    "schema": "olm.package",
    "name": "zeta",
    "defaultChannel": "stable",
    "extra": {"labels": ["first", "second"], "enabled": true}
}`
	b := `{
    "schema": "example.com.metadata",
    "name": "metadata",
    "details": {"description": "kept"}
}`
	c := `{
    "schema": "olm.package",
    "name": "alpha",
    "defaultChannel": "stable"
}`
	for _, tc := range []struct {
		name    string
		records []string
	}{
		// Different schemas and nested fields must arrive without filtering or sorting.
		{"A B C", []string{a, b, c}},
		// The parser must deliver a repeated object again instead of deduplicating it.
		{"A B A", []string{a, b, a}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := catalogTestFile(t, strings.Join(tc.records, "\n")+"\n")
			assertCatalogRecords(t, r, tc.records)
		})
	}
}

// Empty files and files containing only whitespace produce no callbacks and no error.
func TestReadCatalog_Empty(t *testing.T) {
	for _, tc := range []struct{ name, input string }{
		// A zero-byte file ends normally without a record.
		{"empty", ""},
		// Spaces and line breaks do not constitute a record.
		{"whitespace", "  \n   \n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			assertCatalogRecords(t, catalogTestFile(t, tc.input), nil)
		})
	}
}

// A record with an extra string larger than 64 KiB is delivered intact, followed by the next object.
// 1. Write a large indented package record and a second package to a temporary file.
// 2. Inspect both parser deliveries, including the complete large field.
// 3. Verify normal completion after exactly two records.
func TestReadCatalog_LargeRecord(t *testing.T) {
	first := `{
    "schema": "olm.package",
    "name": "alpha",
    "defaultChannel": "stable",
    "extra": "` + strings.Repeat("x", 70*1024) + `"
}`
	second := `{
    "schema": "olm.package",
    "name": "beta",
    "defaultChannel": "stable"
}`
	assertCatalogRecords(t, catalogTestFile(t, first+"\n"+second+"\n"), []string{first, second})
}

// assertCatalogRecords verifies each record's decoded contents and order, the total count, and error-free completion.
func assertCatalogRecords(t *testing.T, r *os.File, want []string) {
	t.Helper()
	count := 0
	err := readCatalog(r, func(record catalogRecord) error {
		if count >= len(want) {
			t.Fatalf("unexpected delivery %d: %s", count+1, record)
		}
		var expected catalogRecord
		if err := json.Unmarshal([]byte(want[count]), &expected); err != nil {
			t.Fatal(err)
		}
		// Compare decoded values, not whitespace in the original JSON.
		for key, raw := range record {
			var gotValue, wantValue any
			if err := json.Unmarshal(raw, &gotValue); err != nil {
				t.Fatal(err)
			}
			if err := json.Unmarshal(expected[key], &wantValue); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(gotValue, wantValue) {
				t.Fatalf("delivery %d field %q = %#v, want %#v", count+1, key, gotValue, wantValue)
			}
		}
		if len(record) != len(expected) {
			t.Fatalf("delivery %d: got %d fields, want %d", count+1, len(record), len(expected))
		}
		count++
		return nil
	})
	if err != nil {
		t.Fatalf("readCatalog: %v", err)
	}
	if count != len(want) {
		t.Fatalf("deliveries = %d, want %d", count, len(want))
	}
}
