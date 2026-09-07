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
	ErrEmptyImageURL         = errors.New("image URL is empty")
	ErrInvalidImageReference = errors.New("invalid image reference")
)

// Inspection errors
var (
	ErrRawInspectFailed            = errors.New("raw image inspect command failed")
	ErrImageInspectFailed          = errors.New("image manifest could not be inspected")
	ErrNoUsableManifests           = errors.New("image index contained no usable manifest entries")
	ErrParseInspectOutput          = errors.New("failed to parse inspect output")
	ErrMissingArchDigest           = errors.New("image manifest does not have an architecture and digest")
	ErrInspectImageFailed          = errors.New("failed to inspect the image")
	ErrParseRawManifest            = errors.New("failed to parse raw manifest")
	ErrMissingImageURL             = errors.New("missing image URL")
	ErrManifestDigestNotFound      = errors.New("manifest digest not found")
	ErrBaseImageAnnotationNotFound = errors.New("base image annotation not found")
)
