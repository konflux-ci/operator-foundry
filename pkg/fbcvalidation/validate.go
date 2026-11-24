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

package fbcvalidation

import (
	"errors"
	"fmt"
	"io"
)

// Validate reports the pending validation checks in extract-and-validate order and returns an error.
func Validate(stderr io.Writer) error {
	reportNotImplemented(stderr, "check-base-image-allowed")
	reportNotImplemented(stderr, "check-catalog-directory")
	reportNotImplemented(stderr, "check-health-probe")
	reportNotImplemented(stderr, "check-package-directories-present")
	reportNotImplemented(stderr, "check-package-directories-non-empty")
	reportNotImplemented(stderr, "validate-catalog")
	reportNotImplemented(stderr, "render-catalog")
	reportNotImplemented(stderr, "check-package-entries")
	reportNotImplemented(stderr, "check-forbidden-bundle-metadata")
	reportNotImplemented(stderr, "check-required-bundle-metadata")
	reportNotImplemented(stderr, "check-bundle-images-pinned")
	reportNotImplemented(stderr, "check-bundle-image-registries")
	reportNotImplemented(stderr, "discover-related-images")

	return errors.New("FBC validation is not implemented yet")
}

func reportNotImplemented(stderr io.Writer, name string) {
	_, _ = fmt.Fprintf(stderr, "%s: not implemented\n", name)
}
