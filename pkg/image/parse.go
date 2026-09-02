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
	"github.com/distribution/reference"
)

// ParsedImageURL holds the decomposed components of an OCI image reference.
type ParsedImageURL struct {
	RegistryRepository string
	Tag                string
	Digest             string
}

// ParseImageURL decomposes an OCI image reference into its registry/repository,
// tag, and digest components. It returns an error for empty input or references
// that do not conform to the OCI distribution spec.
func ParseImageURL(imageURL string) (ParsedImageURL, error) {
	if imageURL == "" {
		return ParsedImageURL{}, fmt.Errorf("ParseImageURL: missing image URL")
	}

	ref, err := reference.ParseNormalizedNamed(imageURL)
	if err != nil {
		return ParsedImageURL{}, fmt.Errorf("ParseImageURL: %w", err)
	}

	registryRepository := ref.Name()
	tag := ""
	digest := ""

	if tagged, ok := ref.(reference.Tagged); ok {
		tag = tagged.Tag()
	}
	if digested, ok := ref.(reference.Digested); ok {
		digest = digested.Digest().String()
	}

	return ParsedImageURL{
		RegistryRepository: registryRepository,
		Tag:                tag,
		Digest:             digest,
	}, nil
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
