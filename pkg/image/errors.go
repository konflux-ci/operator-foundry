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

import "errors"

// Parsing errors
var (
	// ErrEmptyImageURL is returned when the provided image reference is empty.
	ErrEmptyImageURL = errors.New("image URL is empty")
	// ErrInvalidImageReference is returned when the image reference cannot be parsed.
	ErrInvalidImageReference = errors.New("invalid image reference")
)

// Inspection errors
var (
	// ErrRawInspectFailed is returned when the raw manifest fetch fails.
	ErrRawInspectFailed = errors.New("raw manifest fetch failed")
	// ErrManifestInspectFailed is returned when the image manifest cannot be inspected.
	ErrManifestInspectFailed = errors.New("image manifest could not be inspected")
	// ErrNoUsableManifests is returned when an image index contains no usable manifest entries.
	ErrNoUsableManifests = errors.New("image index contained no usable manifest entries")
	// ErrMissingArchDigest is returned when an image manifest lacks architecture and digest information.
	ErrMissingArchDigest = errors.New("image manifest does not have an architecture and digest")
	// ErrImageFetchFailed is returned when image metadata cannot be fetched.
	ErrImageFetchFailed = errors.New("failed to fetch image metadata")
	// ErrRawManifestParseFailed is returned when the raw manifest cannot be parsed.
	ErrRawManifestParseFailed = errors.New("failed to parse raw manifest")
	// ErrManifestDigestNotFound is returned when the expected manifest digest is not found.
	ErrManifestDigestNotFound = errors.New("manifest digest not found")
	// ErrBaseImageAnnotationNotFound is returned when the base image annotation is missing from the manifest.
	ErrBaseImageAnnotationNotFound = errors.New("base image annotation not found")
)
