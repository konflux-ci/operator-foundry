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

package ocp

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"regexp"
	"strconv"
	"strings"

	"github.com/keilerkonzept/dockerfile-json/pkg/dockerfile"
	"github.com/moby/buildkit/frontend/dockerfile/instructions"
	reference "go.podman.io/image/v5/docker/reference"
)

type ocpVersion struct {
	major int
	minor int
}

var ocpVersionRegex = regexp.MustCompile(`^[4-9]\.(0|[1-9][0-9]*)$`)

// GetOCPVersionsFromDockerfile returns the OCP versions targeted by the FBC fragment.
// It first tries to read the com.redhat.fbc.openshift.version label,
// then falls back to extracting the version from the base image tag.
func GetOCPVersionsFromDockerfile(d *dockerfile.Dockerfile) ([]string, error) {
	if d == nil {
		return nil, fmt.Errorf("dockerfile is nil")
	}

	versions, err := getOCPVersionsFromDockerfileLabel(d)
	if err != nil {
		return nil, err
	}
	if len(versions) == 0 {
		slog.Info("com.redhat.fbc.openshift.version label not found, falling back to base image tag")
		version, err := getOCPVersionFromDockerfileBaseImage(d)
		if err != nil {
			return nil, fmt.Errorf("failed to determine OCP version: %w", err)
		}
		slog.Info("determined OCP version from base image tag", "version", version)
		versions = []string{version}
	}

	for _, v := range versions {
		if err := ValidateOCPVersion(v); err != nil {
			return nil, err
		}
	}

	for i, v := range versions {
		versions[i] = strings.TrimPrefix(v, "v")
	}

	slog.Info("fetched OCP versions", "versions", versions)
	return versions, nil
}

// getOCPVersionsFromDockerfileLabel reads the com.redhat.fbc.openshift.version label
// from the Dockerfile and returns the list of targeted OCP versions.
// Returns nil if the label is not present.
func getOCPVersionsFromDockerfileLabel(d *dockerfile.Dockerfile) ([]string, error) {
	const labelKey = "com.redhat.fbc.openshift.version"

	raw := getFBCLabel(d, labelKey)
	if raw == "" {
		return nil, nil
	}

	var versions []string
	if err := json.Unmarshal([]byte(raw), &versions); err != nil {
		return nil, fmt.Errorf("label %q must contain a valid JSON array: %w", labelKey, err)
	}
	if len(versions) == 0 {
		return nil, fmt.Errorf("label %q must contain a non-empty JSON array", labelKey)
	}

	slog.Info("determined OCP version(s) from Dockerfile label", "label", labelKey, "versions", versions)
	return versions, nil
}

// getOCPVersionFromDockerfileBaseImage extracts the OCP version from the base image tag
// in the final Dockerfile stage, as a fallback when the label is absent.
// e.g. FROM registry.redhat.io/openshift4/ose-operator-registry-rhel9:v4.15 → "v4.15"
func getOCPVersionFromDockerfileBaseImage(d *dockerfile.Dockerfile) (string, error) {
	if len(d.Stages) == 0 {
		return "", fmt.Errorf("no stages found in Dockerfile")
	}

	lastStage := d.Stages[len(d.Stages)-1]
	baseImage := lastStage.BaseName
	if baseImage == "" {
		return "", fmt.Errorf("base image is empty in final stage")
	}

	slog.Info("extracting OCP version from base image", "image", baseImage)

	ref, err := reference.ParseNormalizedNamed(baseImage)
	if err != nil {
		return "", fmt.Errorf("could not parse base image reference %q: %w", baseImage, err)
	}

	if _, ok := ref.(reference.Tagged); !ok {
		return "", fmt.Errorf("base image %q has no version tag", baseImage)
	}

	tagged := ref.(reference.Tagged)
	return tagged.Tag(), nil
}

// getFBCLabel searches the final Dockerfile stage for a LABEL instruction
// matching the given key, returning the value that would survive into the
// built image. Commands are iterated in reverse so the last declaration
// wins, mirroring Docker's label precedence rules.
// Only the final stage is searched since earlier builder stages do not
// contribute labels to the built image.
func getFBCLabel(d *dockerfile.Dockerfile, key string) string {
	if len(d.Stages) == 0 {
		return ""
	}
	lastStage := d.Stages[len(d.Stages)-1]
	for j := len(lastStage.Commands) - 1; j >= 0; j-- {
		cmd := lastStage.Commands[j]
		labelCmd, ok := cmd.Command.(*instructions.LabelCommand)
		if !ok {
			continue
		}
		for k := len(labelCmd.Labels) - 1; k >= 0; k-- {
			if labelCmd.Labels[k].Key == key {
				return labelCmd.Labels[k].Value
			}
		}
	}
	return ""
}

// ValidateOCPVersion returns an error if the version is not in the expected
// major.minor format, with an optional leading "v" (e.g., 4.21 or v5.0).
func ValidateOCPVersion(version string) error {
	v := strings.TrimPrefix(version, "v")
	if v == "" {
		return fmt.Errorf("OCP version cannot be empty")
	}
	if !ocpVersionRegex.MatchString(v) {
		return fmt.Errorf("invalid OCP version %q (expected an optional 'v' followed by major.minor format, e.g., 4.21 or v5.0)", version)
	}
	return nil
}

// AnyOCPVersionGTE returns true if any version in the list is >= the threshold.
// Returns an error if any version or the threshold cannot be parsed.
func AnyOCPVersionGTE(versions []string, threshold string) (bool, error) {
	for _, v := range versions {
		gte, err := OCPVersionGTE(v, threshold)
		if err != nil {
			return false, err
		}
		if gte {
			return true, nil
		}
	}
	return false, nil
}

// OCPVersionGTE returns true if version >= threshold.
// Both must be in major.minor format, with or without a leading "v" (e.g., 4.21 or v5.0).
// Returns an error if either version string cannot be parsed.
func OCPVersionGTE(version, threshold string) (bool, error) {
	if err := ValidateOCPVersion(version); err != nil {
		return false, fmt.Errorf("invalid version provided for comparison: %w", err)
	}
	if err := ValidateOCPVersion(threshold); err != nil {
		return false, fmt.Errorf("invalid threshold provided for comparison: %w", err)
	}

	v := parseOCPVersion(strings.TrimPrefix(version, "v"))
	t := parseOCPVersion(strings.TrimPrefix(threshold, "v"))

	if v == nil {
		return false, fmt.Errorf("failed to parse version %q", version)
	}
	if t == nil {
		return false, fmt.Errorf("failed to parse threshold %q", threshold)
	}

	if v.major != t.major {
		return v.major > t.major, nil
	}
	return v.minor >= t.minor, nil
}

// parseOCPVersion parses "4.15" into [4, 15]. Returns nil on failure.
func parseOCPVersion(v string) *ocpVersion {
	parts := strings.SplitN(v, ".", 2)
	if len(parts) != 2 {
		return nil
	}

	major, err := strconv.Atoi(parts[0])
	if err != nil {
		return nil
	}

	minor, err := strconv.Atoi(parts[1])
	if err != nil {
		return nil
	}

	return &ocpVersion{major: major, minor: minor}
}
