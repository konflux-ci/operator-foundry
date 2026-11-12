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
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/google/go-containerregistry/pkg/name"
	"gopkg.in/yaml.v3"
)

type mirrorFileConfig struct {
	Spec mirrorSpecConfig `yaml:"spec"`
}

type mirrorSpecConfig struct {
	ImageDigestMirrors []imageDigestMirror `yaml:"imageDigestMirrors"`
}

type imageDigestMirror struct {
	Source  yamlString   `yaml:"source"`
	Mirrors []yamlString `yaml:"mirrors"`
}

// yamlString unmarshals only YAML string scalars (!!str), matching bash
// process_image_digest_mirror_set where jq string-concat rejects numbers/bools.
type yamlString string

func (s *yamlString) UnmarshalYAML(value *yaml.Node) error {
	if value.Kind != yaml.ScalarNode || value.ShortTag() != "!!str" {
		return fmt.Errorf("expected YAML string, got %s", value.ShortTag())
	}
	*s = yamlString(value.Value)
	return nil
}

// MirrorConfigMap maps an ImageDigestMirrorSet source repository to its mirrors.
type MirrorConfigMap map[string][]string

// LoadMirrorConfigFromFile reads an ImageDigestMirrorSet YAML file and returns
// a MirrorConfigMap (source -> mirrors), matching bash
// process_image_digest_mirror_set. Duplicate sources keep the last entry. A
// source with omitted or empty mirrors maps to a non-nil empty slice.
// Sources and mirrors must be YAML strings (not numbers or booleans).
// Trailing YAML is handled like bash yq '.': empty separators and extra
// valid documents are ignored; malformed trailing content fails parse.
// Errors wrap ErrMirrorConfigReadFailed, ErrMirrorConfigParseFailed,
// ErrInvalidMirrorSource, or ErrInvalidMirror.
func LoadMirrorConfigFromFile(path string) (MirrorConfigMap, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("%w: %q: %w", ErrMirrorConfigReadFailed, path, err)
	}

	// Parse the mirror configuration file
	mirrorCfg, err := decodeMirrorConfig(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("%w: %q: %w", ErrMirrorConfigParseFailed, path, err)
	}

	// Build source -> mirrors map. Duplicate sources overwrite (last wins), like bash.
	mirrorConfigMap := make(MirrorConfigMap, len(mirrorCfg.Spec.ImageDigestMirrors))
	for _, digestMirror := range mirrorCfg.Spec.ImageDigestMirrors {
		// yamlString -> string for the public map and validators.
		source := string(digestMirror.Source)
		if err := validateIDMSRepositoryOrRegistry(source); err != nil {
			return nil, fmt.Errorf("%w: %q in mirror configuration file %q: %w", ErrInvalidMirrorSource, source, path, err)
		}
		// Omitted mirrors is a nil slice; make() yields a non-nil empty []string.
		mirrors := make([]string, 0, len(digestMirror.Mirrors))
		for _, mirror := range digestMirror.Mirrors {
			mirrorStr := string(mirror)
			if err := validateIDMSRepositoryOrRegistry(mirrorStr); err != nil {
				return nil, fmt.Errorf("%w: %q for source %q in file %q: %w", ErrInvalidMirror, mirrorStr, source, path, err)
			}
			mirrors = append(mirrors, mirrorStr)
		}
		mirrorConfigMap[source] = mirrors
	}

	return mirrorConfigMap, nil
}

// decodeMirrorConfig unmarshals the first YAML document as an ImageDigestMirrorSet,
// then walks the rest of the stream like bash yq '.': extra documents (empty or
// valid) are ignored; malformed trailing YAML fails.
func decodeMirrorConfig(r io.Reader) (mirrorFileConfig, error) {
	dec := yaml.NewDecoder(r)

	// First document is the IDMS. Decode into mirrorFileConfig so yamlString
	// rejects non-string source/mirror scalars during unmarshal.
	var mirrorCfg mirrorFileConfig
	if err := dec.Decode(&mirrorCfg); err != nil {
		return mirrorFileConfig{}, err
	}

	// Drain trailing documents: ignore well-formed extras (bare "---", another
	// object); fail only on malformed content, matching bash yq '.'.
	for {
		var trailing any
		err := dec.Decode(&trailing)
		if errors.Is(err, io.EOF) {
			return mirrorCfg, nil
		}
		if err != nil {
			return mirrorFileConfig{}, err
		}
	}
}

// validateIDMSRepositoryOrRegistry checks an ImageDigestMirrorSet source or
// mirror value. IDMS entries are repository paths (registry.example/ns/name)
// or registry-only hosts (registry.example[:port]), not tagged/digested image
// refs. Parsing uses go-containerregistry rather than a character-class regex.
func validateIDMSRepositoryOrRegistry(ref string) error {
	// e.g. ""
	if ref == "" {
		return errors.New("empty repository or registry")
	}

	if strings.Contains(ref, "/") {
		// Repository path: require an explicit registry host, same as ParseImageURL.
		// e.g. "org/repo" (no registry host)
		if !looksFullyQualified(ref) {
			return errors.New("unqualified repository path, expected registry host")
		}
		// e.g. "registry.redhat.io/foo bar", "registry.redhat.io/foo:tag"
		repo, err := name.NewRepository(ref, parseOpts...)
		if err != nil {
			return err
		}
		// check if the host is empty or beginning with ':'
		host := repo.RegistryStr()
		if host == "" || strings.HasPrefix(host, ":") {
			return errors.New("empty registry host")
		}
		// check if path is empty or if it contains "//"
		path := repo.RepositoryStr()
		if path == "" || strings.Contains(path, "//") {
			return errors.New("empty path component")
		}
		// e.g. "registry.redhat.io/container-native-virtualization"
		return nil
	}

	// Registry-only: require an explicit host-looking value so NewRegistry does
	// not silently default short names (e.g. "ubuntu") to Docker Hub.
	// e.g. "ubuntu", "123"
	if !looksLikeRegistryHost(ref) {
		return errors.New("unqualified registry name, expected host with '.' or ':' or localhost")
	}
	// e.g. "foo bar.com" (has '.', so host-looking, but invalid authority)
	if _, err := name.NewRegistry(ref, parseOpts...); err != nil {
		return err
	}
	// e.g. "registry.redhat.io", "mirror.example.com:5000", "localhost"
	return nil
}
