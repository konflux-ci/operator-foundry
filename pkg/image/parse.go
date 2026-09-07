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

// Package image provides helpers for parsing and decomposing OCI image
// references into their registry/repository, tag, and digest components.
package image

import (
	"fmt"
	"strings"

	"github.com/google/go-containerregistry/pkg/name"
)

// parseOpts are the options used for all reference parsing in this package.
// WithDefaultTag("") prevents the library from injecting ":latest" on
// untagged references, preserving the caller's ability to distinguish
// tagged from untagged inputs.
var parseOpts = []name.Option{name.WithDefaultTag("")}

// ParsedImageURL holds the decomposed components of an OCI image reference.
type ParsedImageURL struct {
	RegistryRepository string
	Tag                string
	Digest             string
}

// ParseImageURL decomposes an OCI image reference into its registry/repository,
// tag, and digest components. It returns an error for empty input or references
// that do not conform to the OCI distribution spec.
// Unqualified names (e.g. "ubuntu", "nginx:latest") are rejected — callers
// must supply fully-qualified references containing a registry domain.
func ParseImageURL(imageURL string) (ParsedImageURL, error) {
	if imageURL == "" {
		return ParsedImageURL{}, ErrEmptyImageURL
	}

	if !looksFullyQualified(imageURL) {
		return ParsedImageURL{}, fmt.Errorf("%w: %q: unqualified image name, expected fully-qualified reference with registry", ErrInvalidImageReference, imageURL)
	}

	ref, err := name.ParseReference(imageURL, parseOpts...)
	if err != nil {
		return ParsedImageURL{}, fmt.Errorf("%w: %q: %s", ErrInvalidImageReference, imageURL, err)
	}

	registryRepository := ref.Context().String()
	tag := ""
	digest := ""

	switch r := ref.(type) {
	case name.Tag:
		tag = r.TagStr()
	case name.Digest:
		digest = r.DigestStr()
		tag = extractTagBeforeDigest(imageURL)
	}

	return ParsedImageURL{
		RegistryRepository: registryRepository,
		Tag:                tag,
		Digest:             digest,
	}, nil
}

// looksFullyQualified returns true when the first path segment of the reference
// looks like a registry hostname: it contains a dot or a colon, or equals
// "localhost". This rejects Docker Hub shorthand such as "library/ubuntu" or
// "org/repo:v1" where the first segment is a namespace, not a registry.
func looksFullyQualified(ref string) bool {
	slash := strings.Index(ref, "/")
	if slash <= 0 {
		return false
	}
	host := ref[:slash]
	return strings.ContainsAny(host, ".:") || host == "localhost"
}

// extractTagBeforeDigest extracts the tag from a "repo:tag@digest" reference.
// Returns "" when no tag is present before the digest separator.
func extractTagBeforeDigest(ref string) string {
	atIdx := strings.Index(ref, "@")
	if atIdx <= 0 {
		return ""
	}
	before := ref[:atIdx]
	colonIdx := strings.LastIndex(before, ":")
	if colonIdx <= 0 {
		return ""
	}
	candidate := before[colonIdx+1:]
	if strings.Contains(candidate, "/") {
		return ""
	}
	return candidate
}

// GetImageRegistryAndRepository returns the registry and repository portion of
// the image reference, without tag or digest.
func GetImageRegistryAndRepository(imageURL string) (string, error) {
	parsed, err := ParseImageURL(imageURL)
	if err != nil {
		return "", err
	}
	return parsed.RegistryRepository, nil
}

// GetImageRegistryRepositoryTag returns registry/repository:tag, or just
// registry/repository when no tag is present.
func GetImageRegistryRepositoryTag(imageURL string) (string, error) {
	parsed, err := ParseImageURL(imageURL)
	if err != nil {
		return "", err
	}
	if parsed.Tag != "" {
		return parsed.RegistryRepository + ":" + parsed.Tag, nil
	}
	return parsed.RegistryRepository, nil
}

// GetImageRegistryRepositoryDigest returns registry/repository@digest, or just
// registry/repository when no digest is present.
func GetImageRegistryRepositoryDigest(imageURL string) (string, error) {
	parsed, err := ParseImageURL(imageURL)
	if err != nil {
		return "", err
	}
	if parsed.Digest != "" {
		return parsed.RegistryRepository + "@" + parsed.Digest, nil
	}
	return parsed.RegistryRepository, nil
}
