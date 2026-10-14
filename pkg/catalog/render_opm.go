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

// Package catalog provides helpers for rendering and reading FBC catalogs.
package catalog

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base32"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/konflux-ci/operator-foundry/pkg/image"
)

const (
	// defaultRetryCount counts retries after the initial attempt when RETRY_COUNT is unset or empty.
	defaultRetryCount = 3
	// defaultRetryInterval is used when RETRY_INTERVAL is unset or empty.
	defaultRetryInterval = 5 * time.Second
	// retryIntervalBitSize makes strconv.ParseFloat parse seconds with float64 precision.
	retryIntervalBitSize = 64
	// cacheDirectoryMode grants the owner full access and others read/traverse access, subject to umask.
	cacheDirectoryMode = 0755
	// opmWaitDelay bounds process/pipe cleanup after cancellation or process exit, not render duration.
	opmWaitDelay = time.Second
	// anyInfinitySign tells math.IsInf to match both positive and negative infinity.
	anyInfinitySign = 0
)

// RenderOPM runs the specified OPM binary against an image reference or a local
// FBC file or directory and returns the path to its rendered output.
//
// Successful image renders are cached under cacheDir. Local inputs are rendered
// on every call; the caller must remove the returned local result after use.
// The caller should use a separate cacheDir for each OPM version.
// The cache is a trusted pipeline directory used by one render at a time.
// The caller must keep its paths stable throughout rendering and result use.
//
// RETRY_COUNT and RETRY_INTERVAL configure retries after the initial attempt and
// the delay in seconds. Unset or empty values default to 3 and 5 respectively.
// Cancellation stops the process and retries. Failed renders leave no result.
func RenderOPM(ctx context.Context, target, opmPath, cacheDir string) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if target == "" {
		return "", ErrEmptyTarget
	}
	if opmPath == "" {
		return "", ErrEmptyOPMPath
	}
	if cacheDir == "" {
		return "", ErrEmptyCacheDir
	}

	retries, interval, err := readOPMRetrySettings()
	if err != nil {
		return "", fmt.Errorf("invalid OPM retry settings: %w", err)
	}

	outputDir := cacheDir
	finalPath := ""
	// Stat reads filesystem metadata, following symlinks, without reading file contents.
	// When err is nil, info describes the local target's type and other attributes.
	info, err := os.Stat(target)
	switch {
	case err == nil:
		// Local inputs must be regular files or directories, not devices, pipes, or sockets.
		if !info.Mode().IsRegular() && !info.IsDir() {
			return "", fmt.Errorf("local target %q is not a regular file or directory", target)
		}
	case errors.Is(err, os.ErrNotExist), errors.Is(err, syscall.ENOTDIR), errors.Is(err, syscall.ENAMETOOLONG):
		// A valid image reference may not resolve as a local path: a component
		// may be missing, be a regular file, or exceed filename limits.
		ref, err := image.ParseImageURL(target)
		if err != nil {
			return "", fmt.Errorf("failed to parse image reference: %w", err)
		}
		finalPath, err = imageCachePath(cacheDir, ref)
		if err != nil {
			return "", fmt.Errorf("failed to build cache path for %q: %w", target, err)
		}
		outputDir = filepath.Dir(finalPath)
	default:
		// Propagate other filesystem errors, such as permission denied.
		return "", fmt.Errorf("failed to inspect target %q: %w", target, err)
	}

	if err := os.MkdirAll(outputDir, cacheDirectoryMode); err != nil {
		return "", fmt.Errorf("failed to create output directory: %w", err)
	}
	if finalPath != "" {
		// Only a completed regular file is a cache hit, not an empty directory.
		if cached, err := os.Lstat(finalPath); err == nil {
			if !cached.Mode().IsRegular() {
				return "", fmt.Errorf("cached result %q is not a regular file", finalPath)
			}
			return finalPath, nil
		} else if !errors.Is(err, os.ErrNotExist) {
			return "", fmt.Errorf("failed to inspect cached result: %w", err)
		}
	}

	// Keep unfinished output separate from the cache's published result.
	output, err := os.CreateTemp(outputDir, ".catalog-*")
	if err != nil {
		return "", fmt.Errorf("failed to create temporary output: %w", err)
	}
	keepOutput := false
	defer func() {
		_ = output.Close()
		if !keepOutput {
			_ = os.Remove(output.Name())
		}
	}()

	if err := runOPMWithRetries(ctx, target, opmPath, output, retries, interval); err != nil {
		return "", fmt.Errorf("failed to render catalog: %w", err)
	}
	if err := output.Close(); err != nil {
		return "", fmt.Errorf("failed to close output: %w", err)
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if finalPath != "" {
		// The temporary file is in the same directory, so rename publishes the
		// completed image render atomically on the pipeline's filesystem.
		if err := os.Rename(output.Name(), finalPath); err != nil {
			return "", fmt.Errorf("failed to publish cached result: %w", err)
		}
		return finalPath, nil
	}
	keepOutput = true
	return output.Name(), nil
}

// imageCachePath builds a cache path for an image and checks that it stays inside cacheDir.
func imageCachePath(cacheDir string, ref image.ParsedImageURL) (string, error) {
	components := strings.Split(ref.RegistryRepository, "/")
	referenceIndex := len(components)
	// Explicit branches distinguish absent tags and digests from literal values
	// such as "untagged" or "catalog".
	components = append(components, "_refs")
	var tagIndex int
	if ref.Tag == "" {
		components = append(components, "untagged")
	} else {
		components = append(components, "tagged-base32", ref.Tag)
		tagIndex = len(components) - 1
	}
	if ref.Digest == "" {
		components = append(components, "no-digest")
	} else {
		components = append(components, "digest", ref.Digest)
	}
	components = append(components, "catalog")
	for _, component := range components {
		// Reject invalid or unsafe components, including:
		//   - empty components;
		//   - "." (current directory);
		//   - ".." (parent directory);
		//   - "/" or "\" within a component;
		//   - the NUL character (\x00).
		if component == "" || component == "." || component == ".." || strings.ContainsAny(component, "/\\\x00") {
			return "", fmt.Errorf("unsafe cache path component %q", component)
		}
	}

	if ref.Tag != "" {
		// Lowercase Base32 preserves tag case distinctions on case-insensitive filesystems.
		// A 128-byte tag encodes to 205 bytes, below common 255-byte component limits.
		// The namespace separates encoded keys from older raw-tag cache entries.
		components[tagIndex] = strings.ToLower(base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString([]byte(ref.Tag)))
	}

	// Hash the complete validated repository identity so nested names cannot
	// overlap the reference layout. The fixed-size key also fits filename limits.
	repositoryKey := fmt.Sprintf("%x", sha256.Sum256([]byte(ref.RegistryRepository)))
	components = append([]string{"repositories-sha256", repositoryKey}, components[referenceIndex:]...)

	result := filepath.Join(append([]string{cacheDir}, components...)...)
	relative, err := filepath.Rel(cacheDir, result)
	if err != nil {
		return "", fmt.Errorf("failed to check cache containment: %w", err)
	}
	if filepath.IsAbs(relative) || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("cache path %q escapes cache directory %q", result, cacheDir)
	}
	return result, nil
}

// readOPMRetrySettings reads and validates retry configuration from the environment.
// Empty or unset values use the same defaults as Bash retry.
func readOPMRetrySettings() (int, time.Duration, error) {
	retries := defaultRetryCount
	if value := os.Getenv("RETRY_COUNT"); value != "" {
		count, err := strconv.Atoi(value)
		if err != nil || count < 0 {
			return 0, 0, fmt.Errorf("invalid RETRY_COUNT %q: expected a non-negative integer", value)
		}
		retries = count
	}
	interval := defaultRetryInterval
	if value := os.Getenv("RETRY_INTERVAL"); value != "" {
		seconds, err := strconv.ParseFloat(value, retryIntervalBitSize)
		// Reject values that cannot be represented as a time.Duration as well.
		if err != nil || seconds < 0 || math.IsNaN(seconds) || math.IsInf(seconds, anyInfinitySign) || seconds*float64(time.Second) >= float64(math.MaxInt64) {
			return 0, 0, fmt.Errorf("invalid RETRY_INTERVAL %q: expected finite non-negative seconds within duration range", value)
		}
		interval = time.Duration(seconds * float64(time.Second))
	}

	return retries, interval, nil
}

// runOPMWithRetries renders into output, discarding partial output before each attempt.
// The caller owns closing, publishing, and removing output; cancellation stops the
// running command or retry wait and returns the context error.
func runOPMWithRetries(ctx context.Context, target, opmPath string, output *os.File, retries int, interval time.Duration) error {
	for attempt := 0; ; attempt++ {
		if err := ctx.Err(); err != nil {
			return err
		}
		// Each attempt replaces all previous output, including a longer partial render.
		if err := output.Truncate(0); err != nil {
			return fmt.Errorf("failed to truncate output: %w", err)
		}
		if _, err := output.Seek(0, io.SeekStart); err != nil {
			return fmt.Errorf("failed to rewind output: %w", err)
		}
		var stderr bytes.Buffer
		cmd := exec.CommandContext(ctx, opmPath, "render", target)
		cmd.Stdout = output
		cmd.Stderr = &stderr
		cmd.WaitDelay = opmWaitDelay
		err := cmd.Run()
		if ctxErr := ctx.Err(); ctxErr != nil {
			return ctxErr
		}
		if err == nil {
			return nil
		}
		if attempt == retries {
			return fmt.Errorf("failed to render %q (%s): %w", target, strings.TrimSpace(stderr.String()), err)
		}
		timer := time.NewTimer(interval)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}
}
