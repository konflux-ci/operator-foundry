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
	"encoding/hex"
	"errors"
	"fmt"
	"hash"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/konflux-ci/operator-foundry/pkg/image"
	"github.com/konflux-ci/operator-foundry/pkg/retry"
)

const (
	// cacheDirectoryMode grants the owner full access and others read/traverse access, subject to umask.
	cacheDirectoryMode = 0755
	// opmWaitDelay bounds process/pipe cleanup after cancellation or process exit, not render duration.
	opmWaitDelay = time.Second
	// opmStderrLimit bounds stderr retained from the external OPM process for diagnostics.
	opmStderrLimit = 1 << 20
	// retryAgain allows retry.Do to repeat a failed operation if retries remain.
	retryAgain = true
	// stopRetrying tells retry.Do to return the operation's result without another attempt.
	stopRetrying = false
)

// Config controls retries for RenderOPM.
//
// The zero value makes exactly one attempt with no retries. To try more than
// once, callers must explicitly set RetryCount and, usually, RetryInterval.
type Config struct {
	// RetryCount is the number of retries after the initial attempt; it must be non-negative.
	RetryCount int
	// RetryInterval is the delay between attempts; it must be non-negative.
	RetryInterval time.Duration
}

// RenderOPM runs the specified OPM binary against an image reference and returns
// the path to its rendered output.
//
// The OPM binary must support the "render" subcommand.
// Successful image renders are cached under cacheDir and isolated by the OPM binary hash.
// The cache identity is derived from the executable file at opmPath.
// The opmPath argument must include a path separator; PATH lookup is not supported.
// The executable must be readable on every call, including cache hits.
// Wrappers that delegate to mutable implementations are the caller's responsibility.
// The cache is a trusted pipeline directory used by one render at a time.
// The caller must keep its paths stable throughout rendering and result use.
//
// Cancellation stops the process and retries. Failed renders leave no result.
func RenderOPM(ctx context.Context, target, opmPath, cacheDir string, cfg Config) (string, error) {
	if opmPath == "" {
		return "", ErrEmptyOPMPath
	}
	return renderOPM(ctx, target, cacheDir, cfg, execOPMRunner{opmPath: opmPath})
}

// opmRunner provides OPM operations used by renderOPM.
type opmRunner interface {
	BinaryHash(ctx context.Context) (string, error)
	Render(ctx context.Context, target string, output io.Writer) (shouldRetry bool, err error)
}

// execOPMRunner runs the real OPM executable for production rendering.
type execOPMRunner struct {
	opmPath string
}

// BinaryHash returns the SHA-256 digest of the configured OPM executable.
func (r execOPMRunner) BinaryHash(ctx context.Context) (string, error) {
	if err := validateOPMExecutablePath(r.opmPath); err != nil {
		return "", err
	}
	return hashOPMFile(ctx, r.opmPath, sha256.New())
}

// Render invokes "opm render <target>" and streams rendered catalog data to output.
func (r execOPMRunner) Render(ctx context.Context, target string, output io.Writer) (shouldRetry bool, err error) {
	if err := validateOPMExecutablePath(r.opmPath); err != nil {
		return stopRetrying, err
	}
	var stderr bytes.Buffer
	cmd := exec.CommandContext(ctx, r.opmPath, "render", target)
	cmd.Stdout = output
	cmd.Stderr = &cappedWriter{w: &stderr, remaining: opmStderrLimit}
	cmd.WaitDelay = opmWaitDelay
	err = cmd.Run()
	if ctxErr := ctx.Err(); ctxErr != nil {
		return stopRetrying, ctxErr
	}
	if err != nil {
		err = fmt.Errorf("failed to render %q (%s): %w", target, strings.TrimSpace(stderr.String()), err)
		if errors.Is(err, os.ErrNotExist) || errors.Is(err, exec.ErrNotFound) || errors.Is(err, os.ErrPermission) {
			return stopRetrying, err
		}
		return retryAgain, err
	}
	return stopRetrying, nil
}

// renderOPM renders an image reference through runner and publishes a cached result.
func renderOPM(ctx context.Context, target, cacheDir string, cfg Config, runner opmRunner) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if target == "" {
		return "", ErrEmptyTarget
	}
	if cacheDir == "" {
		return "", ErrEmptyCacheDir
	}
	if err := validateRetryConfig(cfg); err != nil {
		return "", err
	}

	ref, err := image.ParseImageURL(target)
	if err != nil {
		return "", fmt.Errorf("failed to parse image reference: %w", err)
	}
	opmBinaryHash, err := identifyOPMBinary(ctx, cfg, runner)
	if err != nil {
		return "", err
	}
	finalPath, err := buildRenderCachePath(cacheDir, opmBinaryHash, ref)
	if err != nil {
		return "", fmt.Errorf("failed to build cache path for %q: %w", target, err)
	}
	outputDir := filepath.Dir(finalPath)

	if err := os.MkdirAll(outputDir, cacheDirectoryMode); err != nil {
		return "", fmt.Errorf("failed to create output directory: %w", err)
	}

	// Only a completed regular file is a cache hit, not an empty directory.
	if cached, err := os.Lstat(finalPath); err == nil {
		if !cached.Mode().IsRegular() {
			return "", fmt.Errorf("cached result %q is not a regular file", finalPath)
		}
		if cached.Size() > 0 {
			return finalPath, nil
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return "", fmt.Errorf("failed to inspect cached result: %w", err)
	}

	// Keep unfinished output separate from the cache's published result.
	output, err := os.CreateTemp(outputDir, ".catalog-*")
	if err != nil {
		return "", fmt.Errorf("failed to create temporary output: %w", err)
	}
	outputClosed := false
	defer func() {
		if !outputClosed {
			_ = output.Close()
		}
		_ = os.Remove(output.Name())
	}()

	if err := retry.Do(ctx, cfg.RetryCount, cfg.RetryInterval, func(ctx context.Context) (shouldRetry bool, err error) {
		return runOPM(ctx, target, runner, output)
	}); err != nil {
		return "", fmt.Errorf("failed to render catalog: %w", err)
	}
	if err := output.Close(); err != nil {
		return "", fmt.Errorf("failed to close output: %w", err)
	}
	outputClosed = true
	if rendered, err := os.Stat(output.Name()); err != nil {
		return "", fmt.Errorf("failed to inspect rendered output: %w", err)
	} else if rendered.Size() == 0 {
		return "", fmt.Errorf("failed to render catalog: %w", ErrEmptyOPMOutput)
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	// The temporary file is in the same directory, so rename publishes the
	// completed image render atomically on the pipeline's filesystem.
	if err := os.Rename(output.Name(), finalPath); err != nil {
		return "", fmt.Errorf("failed to publish cached result: %w", err)
	}
	return finalPath, nil
}

// validateRetryConfig checks retry settings and identifies the invalid Config field.
func validateRetryConfig(cfg Config) error {
	if cfg.RetryCount < 0 {
		return fmt.Errorf("invalid OPM retry settings for RetryCount: %w", ErrInvalidRetryCount)
	}
	if cfg.RetryInterval < 0 {
		return fmt.Errorf("invalid OPM retry settings for RetryInterval: %w", ErrInvalidRetryInterval)
	}
	return nil
}

// cappedWriter keeps only the first N bytes of process output so error messages stay small.
type cappedWriter struct {
	w io.Writer
	// remaining tracks how many more bytes may be retained.
	remaining int
}

// Write retains bytes up to the cap and reports the original length to avoid short writes.
func (w *cappedWriter) Write(p []byte) (int, error) {
	// Report the original length so callers do not treat discarded bytes as a write failure.
	originalLen := len(p)
	if w.remaining <= 0 {
		return originalLen, nil
	}
	if len(p) > w.remaining {
		p = p[:w.remaining]
	}
	n, err := w.w.Write(p)
	w.remaining -= n
	if err != nil {
		return n, err
	}
	return originalLen, nil
}

var (
	errOPMBinaryNotRegular      = errors.New("OPM binary is not a regular file")
	errOPMBinaryPathNotExplicit = errors.New("OPM binary path must include a path separator")
)

// buildRenderCachePath builds a cache path for an OPM binary hash and image reference.
func buildRenderCachePath(cacheRootDir, opmBinaryHash string, ref image.ParsedImageURL) (string, error) {
	opmBinaryHash = strings.TrimSpace(opmBinaryHash)
	if opmBinaryHash == "" {
		return "", ErrEmptyOPMBinaryHash
	}
	if !isSHA256Hex(opmBinaryHash) {
		return "", fmt.Errorf("invalid OPM binary hash %q", opmBinaryHash)
	}
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
	components = append([]string{"opm-binaries-sha256", opmBinaryHash}, components...)
	result := filepath.Join(append([]string{cacheRootDir}, components...)...)
	relative, err := filepath.Rel(cacheRootDir, result)
	if err != nil {
		return "", fmt.Errorf("failed to check cache containment: %w", err)
	}
	if filepath.IsAbs(relative) || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("cache path %q escapes cache directory %q", result, cacheRootDir)
	}
	return result, nil
}

// identifyOPMBinary applies the configured retry policy to retryable binary hash checks.
func identifyOPMBinary(ctx context.Context, cfg Config, runner opmRunner) (string, error) {
	var binaryHash string
	if err := retry.Do(ctx, cfg.RetryCount, cfg.RetryInterval, func(ctx context.Context) (bool, error) {
		got, err := runner.BinaryHash(ctx)
		if err != nil {
			return isRetryableOPMBinaryHashError(err), err
		}
		got = strings.TrimSpace(got)
		if got == "" {
			return stopRetrying, ErrEmptyOPMBinaryHash
		}
		binaryHash = got
		return stopRetrying, nil
	}); err != nil {
		return "", fmt.Errorf("failed to identify OPM binary: %w", err)
	}
	return binaryHash, nil
}

// isRetryableOPMBinaryHashError reports whether a binary hash error allows another attempt.
func isRetryableOPMBinaryHashError(err error) bool {
	if errors.Is(err, context.Canceled) ||
		errors.Is(err, context.DeadlineExceeded) ||
		errors.Is(err, os.ErrNotExist) ||
		errors.Is(err, exec.ErrNotFound) ||
		errors.Is(err, os.ErrPermission) ||
		errors.Is(err, errOPMBinaryNotRegular) ||
		errors.Is(err, errOPMBinaryPathNotExplicit) ||
		errors.Is(err, ErrEmptyOPMBinaryHash) {
		return stopRetrying
	}
	return retryAgain
}

// validateOPMExecutablePath requires an explicit OPM executable path.
func validateOPMExecutablePath(path string) error {
	if filepath.Base(path) == path {
		return fmt.Errorf("OPM binary path %q: %w", path, errOPMBinaryPathNotExplicit)
	}
	return nil
}

// hashOPMFile returns the lowercase hex digest of the OPM executable contents.
func hashOPMFile(ctx context.Context, path string, h hash.Hash) (digest string, err error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	info, err := os.Stat(path)
	if err != nil {
		return "", fmt.Errorf("failed to inspect OPM binary: %w", err)
	}
	if !info.Mode().IsRegular() {
		return "", fmt.Errorf("OPM binary %q is not a regular file: %w", path, errOPMBinaryNotRegular)
	}
	file, err := os.Open(path)
	if err != nil {
		return "", fmt.Errorf("failed to open OPM binary: %w", err)
	}
	defer func() {
		if closeErr := file.Close(); closeErr != nil && err == nil {
			err = fmt.Errorf("failed to close OPM binary: %w", closeErr)
		}
	}()
	buffer := make([]byte, 32*1024)
	for {
		if err := ctx.Err(); err != nil {
			return "", err
		}
		n, err := file.Read(buffer)
		if n > 0 {
			if _, writeErr := h.Write(buffer[:n]); writeErr != nil {
				return "", fmt.Errorf("failed to hash OPM binary: %w", writeErr)
			}
		}
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return "", fmt.Errorf("failed to read OPM binary: %w", err)
		}
	}
	return fmt.Sprintf("%x", h.Sum(nil)), nil
}

// isSHA256Hex reports whether value is a lowercase or uppercase SHA-256 hex digest.
func isSHA256Hex(value string) bool {
	if len(value) != sha256.Size*2 {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}

// runOPM performs one render, discarding any previous partial output.
// It reports whether an error allows another attempt.
// The caller owns closing, publishing, and removing output.
func runOPM(ctx context.Context, target string, runner opmRunner, output *os.File) (shouldRetry bool, err error) {
	if err := ctx.Err(); err != nil {
		return stopRetrying, err
	}
	if err := output.Truncate(0); err != nil {
		return stopRetrying, fmt.Errorf("failed to truncate output: %w", err)
	}
	if _, err := output.Seek(0, io.SeekStart); err != nil {
		return stopRetrying, fmt.Errorf("failed to rewind output: %w", err)
	}
	return runner.Render(ctx, target, output)
}
