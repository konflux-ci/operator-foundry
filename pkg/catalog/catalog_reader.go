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
	"fmt"
	"io"
)

// catalogRecord defers field decoding until an extractor actually needs a field.
type catalogRecord map[string]json.RawMessage

// readCatalog delivers each JSON object before decoding the next one.
// The caller owns r; no complete catalog is accumulated in memory.
// Input is decoded as-is; invalid JSON returns an error with the record number.
func readCatalog(r io.Reader, consume func(catalogRecord) error) error {
	decoder := json.NewDecoder(r)
	for index := 1; ; index++ {
		var record catalogRecord
		if err := decoder.Decode(&record); err != nil {
			if err == io.EOF {
				return nil
			}
			return fmt.Errorf("read catalog record %d: %w", index, err)
		}
		if record == nil {
			return fmt.Errorf("catalog record %d: expected a JSON object", index)
		}
		if err := consume(record); err != nil {
			return fmt.Errorf("catalog record %d: %w", index, err)
		}
	}
}

// matches reports whether field decodes without error to the given string value.
func (r catalogRecord) matches(field, value string) bool {
	var got string
	return json.Unmarshal(r[field], &got) == nil && got == value
}

// nonEmptyString decodes field as a string, returning an error if decoding fails
// or the value is empty.
func (r catalogRecord) nonEmptyString(field string) (string, error) {
	var value string
	if err := json.Unmarshal(r[field], &value); err != nil {
		return "", fmt.Errorf("decode %s: %w", field, err)
	}
	if value == "" {
		return "", fmt.Errorf("%s must be a non-empty string", field)
	}
	return value, nil
}
