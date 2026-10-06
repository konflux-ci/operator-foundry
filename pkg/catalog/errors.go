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
	"errors"

	"github.com/konflux-ci/operator-foundry/pkg/retry"
)

// Argument validation errors
var (
	// ErrEmptyPackageName is returned when a bundle or related image query has no package name.
	ErrEmptyPackageName = errors.New("FBC catalog package name is empty")
	// ErrEmptyTarget is returned when the render target is empty.
	ErrEmptyTarget = errors.New("render target is empty")
	// ErrEmptyOPMPath is returned when the OPM binary path is empty.
	ErrEmptyOPMPath = errors.New("OPM binary path is empty")
	// ErrEmptyCacheDir is returned when the cache directory is empty.
	ErrEmptyCacheDir = errors.New("cache directory is empty")
	// ErrInvalidRetryCount is returned when the retry count is negative.
	ErrInvalidRetryCount = retry.ErrInvalidRetryCount
	// ErrInvalidRetryInterval is returned when the retry interval is negative.
	ErrInvalidRetryInterval = retry.ErrInvalidRetryInterval
)

// Render errors
var (
	// ErrEmptyOPMOutput is returned when OPM succeeds without producing rendered catalog data.
	ErrEmptyOPMOutput = errors.New("OPM output is empty")
)
