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
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/konflux-ci/operator-foundry/pkg/image"
	"github.com/konflux-ci/operator-foundry/pkg/retry"
)

const (
	// cacheDirectoryMode grants the owner full access and others read/traverse access, subject to umask.
	cacheDirectoryMode = 0755
	// opmWaitDelay bounds process/pipe cleanup after cancellation or process exit, not render duration.
	opmWaitDelay = time.Second

	// retryAgain allows retry.Do to repeat a failed operation if retries remain.
	retryAgain = true
	// stopRetrying tells retry.Do to return the operation's result without another attempt.
	stopRetrying = false
)

// Config controls retries for RenderOPM. Its zero value makes a single attempt.
// The caller owns defaults and any environment variable parsing.
type Config struct {
	// RetryCount is the number of retries after the initial attempt; it must be non-negative.
	RetryCount int
	// RetryInterval is the delay between attempts; it must be non-negative.
	RetryInterval time.Duration
}

// RenderOPM runs the specified OPM binary against an image reference or a local
// FBC file or directory and returns the path to its rendered output.
//
// Successful image renders are cached under cacheDir. Local inputs are rendered
// on every call; the caller must remove the returned local result after use.
// The caller should use a separate cacheDir for each OPM version.
// The cache is a trusted pipeline directory used by one render at a time.
// The caller must keep its paths stable throughout rendering and result use.
//
// Cancellation stops the process and retries. Failed renders leave no result.
func RenderOPM(ctx context.Context, target, opmPath, cacheDir string, cfg Config) (string, error) {
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

	if cfg.RetryCount < 0 {
		return "", fmt.Errorf("invalid OPM retry settings: RetryCount must be non-negative")
	}
	if cfg.RetryInterval < 0 {
		return "", fmt.Errorf("invalid OPM retry settings: RetryInterval must be non-negative")
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

	if err := retry.Do(ctx, cfg.RetryCount, cfg.RetryInterval, func(ctx context.Context) (shouldRetry bool, err error) {
		return runOPM(ctx, target, opmPath, output)
	}); err != nil {
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

// runOPM performs one render, discarding any previous partial output.
// It reports whether an error allows another attempt.
// The caller owns closing, publishing, and removing output.
func runOPM(ctx context.Context, target, opmPath string, output *os.File) (shouldRetry bool, err error) {
	if err := ctx.Err(); err != nil {
		return stopRetrying, err
	}
	if err := output.Truncate(0); err != nil {
		return stopRetrying, fmt.Errorf("failed to truncate output: %w", err)
	}
	if _, err := output.Seek(0, io.SeekStart); err != nil {
		return stopRetrying, fmt.Errorf("failed to rewind output: %w", err)
	}
	var stderr bytes.Buffer
	cmd := exec.CommandContext(ctx, opmPath, "render", target)
	cmd.Stdout = output
	cmd.Stderr = &stderr
	cmd.WaitDelay = opmWaitDelay
	err = cmd.Run()
	if ctxErr := ctx.Err(); ctxErr != nil {
		return stopRetrying, ctxErr
	}
	if err != nil {
		return retryAgain, fmt.Errorf("failed to render %q (%s): %w", target, strings.TrimSpace(stderr.String()), err)
	}
	return stopRetrying, nil
}
