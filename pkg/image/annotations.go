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

package image

import (
	"context"
	"encoding/json"
	"fmt"
)

// GetAnnotations extracts OCI annotations from a raw image manifest.
// It normalises imageRef to digest form when a digest is present;
// otherwise it preserves the tag so the registry can resolve it.
func GetAnnotations(ctx context.Context, inspector ImageInspector, imageRef string) (map[string]string, error) {
	if imageRef == "" {
		return nil, ErrEmptyImageURL
	}

	parsed, err := ParseImageURL(imageRef)
	if err != nil {
		return nil, err
	}

	raw, err := inspector.InspectRaw(ctx, normalizeImageRef(parsed))
	if err != nil {
		return nil, err
	}

	var manifest struct {
		Annotations map[string]string `json:"annotations"`
	}
	if err := json.Unmarshal(raw, &manifest); err != nil {
		return nil, fmt.Errorf("%w: %w", ErrParseRawManifest, err)
	}

	if manifest.Annotations == nil {
		return map[string]string{}, nil
	}
	return manifest.Annotations, nil
}
