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
	"fmt"
	"os"
	"regexp"

	"gopkg.in/yaml.v3"
)

type mirrorFileConfig struct {
	Spec mirrorSpecConfig `yaml:"spec"`
}

type mirrorSpecConfig struct {
	ImageDigestMirrors []imageDigestMirror `yaml:"imageDigestMirrors"`
}

type imageDigestMirror struct {
	Source  string   `yaml:"source"`
	Mirrors []string `yaml:"mirrors"`
}

// MirrorConfigMap maps an ImageDigestMirrorSet source repository to its mirrors.
type MirrorConfigMap map[string][]string

// registryPathPattern matches bash process_image_digest_mirror_set validation.
var registryPathPattern = regexp.MustCompile(`^[A-Za-z0-9._/:-]+$`)

// LoadMirrorConfigFromFile reads an ImageDigestMirrorSet YAML file and returns
// a MirrorConfigMap (source -> mirrors), matching bash
// process_image_digest_mirror_set. Duplicate sources keep the last entry. A
// source with omitted or empty mirrors maps to a non-nil empty slice.
// Errors wrap ErrMirrorConfigReadFailed, ErrMirrorConfigParseFailed,
// ErrInvalidMirrorSource, or ErrInvalidMirror.
func LoadMirrorConfigFromFile(path string) (MirrorConfigMap, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("%w: reading mirror configuration file %q: %w", ErrMirrorConfigReadFailed, path, err)
	}

	var mirrorCfg mirrorFileConfig
	if err := yaml.Unmarshal(data, &mirrorCfg); err != nil {
		return nil, fmt.Errorf("%w: parsing mirror configuration file %q: %w", ErrMirrorConfigParseFailed, path, err)
	}

	mirrorConfigMap := make(MirrorConfigMap, len(mirrorCfg.Spec.ImageDigestMirrors))
	for _, digestMirror := range mirrorCfg.Spec.ImageDigestMirrors {
		if !registryPathPattern.MatchString(digestMirror.Source) {
			return nil, fmt.Errorf("%w: %q in mirror configuration file %q", ErrInvalidMirrorSource, digestMirror.Source, path)
		}
		for _, mirror := range digestMirror.Mirrors {
			if !registryPathPattern.MatchString(mirror) {
				return nil, fmt.Errorf("%w: %q for source %q in file %q", ErrInvalidMirror, mirror, digestMirror.Source, path)
			}
		}

		// OpenShift allows empty/omitted mirrors; keep a non-nil empty slice.
		mirrors := digestMirror.Mirrors
		if mirrors == nil {
			mirrors = []string{}
		}
		mirrorConfigMap[digestMirror.Source] = mirrors
	}

	return mirrorConfigMap, nil
}
