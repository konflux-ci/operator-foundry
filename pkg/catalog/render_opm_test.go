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
	"context"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/konflux-ci/operator-foundry/pkg/image"
)

const (
	testExecutableMode = 0700
	testImage          = "quay.io/example/index:v1"
	testDigest         = "sha256:1111111111111111111111111111111111111111111111111111111111111111"
	testPayload        = "{\"schema\":\"olm.package\",\"name\":\"example\"}\n"
	// Expected SHA-256 cache prefixes for quay.io/example/index and localhost:5000/example/index.
	testRepositoryCachePath   = "repositories-sha256/c43e683059d2605481b2596467e290717b14752a7ee70163b6f8ea1aff2096fd"
	testRegistryPortCachePath = "repositories-sha256/047cf8f3e5a55f861244b86ca83cb199a41e3e8c36b464848c4f8e4db0f9b6cb"

	testRetryCancelTimeout  = 2 * time.Second
	testRunnerStartTimeout  = 5 * time.Second
	testCancellationTimeout = 3 * time.Second
)

// An uncached image is rendered once and saved at the expected cache path for its reference format.
// 1. Choose an image reference with no cached result.
// 2. Call renderOPM with a fake runner.
// 3. Verify one runner call and the expected contents at the correct cache path, with no extra files.
//
// Cache paths share the prefix <cacheDir>/repositories-sha256/<repository-hash>/_refs/:
//   - No tag or digest: untagged/no-digest/catalog
//   - Tag only: tagged-base32/<encoded-tag>/no-digest/catalog
//   - Digest only: untagged/digest/<digest>/catalog
//   - Tag and digest: tagged-base32/<encoded-tag>/digest/<digest>/catalog
func TestRenderOPM_CacheMiss(t *testing.T) {
	cases := []struct {
		name, target, relativePath string
	}{
		{"untagged", "quay.io/example/index", testRepositoryCachePath + "/_refs/untagged/no-digest/catalog"},
		{"tag", testImage, testRepositoryCachePath + "/_refs/tagged-base32/oyyq/no-digest/catalog"},
		{"digest", "quay.io/example/index@" + testDigest, testRepositoryCachePath + "/_refs/untagged/digest/" + testDigest + "/catalog"},
		{"tag and digest", testImage + "@" + testDigest, testRepositoryCachePath + "/_refs/tagged-base32/oyyq/digest/" + testDigest + "/catalog"},
		{"registry port", "localhost:5000/example/index:v1", testRegistryPortCachePath + "/_refs/tagged-base32/oyyq/no-digest/catalog"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			runner := &fakeRunner{payload: testPayload}
			cacheDir := filepath.Join(t.TempDir(), "new_cache")
			got, err := renderOPM(t.Context(), tc.target, cacheDir, Config{}, runner)
			if err != nil {
				t.Fatalf("renderOPM: %v", err)
			}
			want := filepath.Join(cacheDir, filepath.FromSlash(tc.relativePath))
			if got != want {
				t.Fatalf("result path = %q, want %q", got, want)
			}
			assertContents(t, got, testPayload)
			runner.assertCalls(t, tc.target)
			assertFiles(t, cacheDir, want)
		})
	}
}

// An existing cached result is returned unchanged without invoking the runner or creating extra files.
// 1. Create a cached result and configure the fake runner to fail if invoked.
// 2. Call renderOPM for the same image.
// 3. Verify the existing result is unchanged, the runner was not invoked, and no extra files appeared.
func TestRenderOPM_CacheHit(t *testing.T) {
	runner := &fakeRunner{failOnCall: true}
	cacheDir := t.TempDir()
	want := imageCatalogPath(cacheDir)
	writeTestFile(t, want, "previous successful render", testFileMode)

	got, err := renderOPM(t.Context(), testImage, cacheDir, Config{}, runner)
	if err != nil {
		t.Fatalf("renderOPM: %v", err)
	}
	if got != want {
		t.Fatalf("result path = %q, want %q", got, want)
	}
	assertContents(t, got, "previous successful render")
	runner.assertCalls(t)
	assertFiles(t, cacheDir, want)
}

// Invalid retry settings are rejected even when a rendered catalog already exists in cache.
// 1. Create a cached result and configure an invalid retry count.
// 2. Call renderOPM for the same image.
// 3. Verify the invalid setting is reported, the runner was not invoked, and the cache is unchanged.
func TestRenderOPM_CacheHitRejectsInvalidRetrySettings(t *testing.T) {
	runner := &fakeRunner{failOnCall: true}
	cacheDir := t.TempDir()
	want := imageCatalogPath(cacheDir)
	writeTestFile(t, want, "previous successful render", testFileMode)

	got, err := renderOPM(t.Context(), testImage, cacheDir, Config{RetryCount: -1}, runner)
	if err == nil || got != "" {
		t.Fatalf("cache hit with invalid retry settings = (%q, %v), want empty path and error", got, err)
	}
	if !errors.Is(err, ErrInvalidRetryCount) {
		t.Errorf("configuration error = %v, want %v", err, ErrInvalidRetryCount)
	}
	assertContents(t, want, "previous successful render")
	runner.assertCalls(t)
	assertFiles(t, cacheDir, want)
}

// A long image reference must render and reuse its cache even when its registry exists locally.
// 1. Create the registry directory and choose a reference that would be too long as a local filename.
// 2. Render the image twice.
// 3. Verify the unchanged reference reaches OPM once and the cached output is reused.
func TestRenderOPM_LongImageReference(t *testing.T) {
	runner := &fakeRunner{payload: testPayload}
	t.Chdir(t.TempDir())
	if err := os.Mkdir("quay.io", testDirectoryMode); err != nil {
		t.Fatal(err)
	}
	repository := strings.Repeat("r", 100)
	tag := strings.Repeat("t", 128)

	target := "quay.io/" + repository + ":" + tag + "@" + testDigest
	cacheDir := t.TempDir()
	want := filepath.Join(cacheDir, "repositories-sha256", "17ec59b541607a89300cb3a1530da071837e277425c1bf3a7556042f1bf42ec9", "_refs", "tagged-base32", strings.Repeat("or2hi5du", 25)+"or2hi", "digest", testDigest, "catalog")
	for range 2 {
		got, err := renderOPM(t.Context(), target, cacheDir, Config{}, runner)
		if err != nil || got != want {
			t.Fatalf("long image render = (%q, %v), want %q", got, err, want)
		}
		assertContents(t, got, testPayload)
		runner.failOnCall = true
	}
	runner.assertCalls(t, target)
	assertFiles(t, cacheDir, want)
}

// ── imageCachePath ───────────────────────────────────────────

// Cache paths must hash the repository identity, encode tags, and reject unsafe reference components.
// 1. Supply reference fields directly, without passing through ParseImageURL.
// 2. Call imageCachePath for each valid or unsafe reference.
// 3. Verify the expected path for valid input, or an error and an empty path for unsafe input.
func TestImageCachePath(t *testing.T) {
	const repository = "quay.io/example/index"
	cases := []struct {
		name, repository, tag, digest, relativePath string
	}{
		{"untagged", repository, "", "", testRepositoryCachePath + "/_refs/untagged/no-digest/catalog"},
		{"tag", repository, "v1", "", testRepositoryCachePath + "/_refs/tagged-base32/oyyq/no-digest/catalog"},
		{"digest", repository, "", testDigest, testRepositoryCachePath + "/_refs/untagged/digest/" + testDigest + "/catalog"},
		{"tag and digest", repository, "v1", testDigest, testRepositoryCachePath + "/_refs/tagged-base32/oyyq/digest/" + testDigest + "/catalog"},
		{"registry port", "localhost:5000/example/index", "v1", "", testRegistryPortCachePath + "/_refs/tagged-base32/oyyq/no-digest/catalog"},
		{"empty repository", "", "v1", "", ""},
		{"empty repository component", "quay.io/example//index", "v1", "", ""},
		{"repository dot", "quay.io/example/./index", "v1", "", ""},
		{"repository parent", "quay.io/example/a/../index", "v1", "", ""},
		{"repository escape", "quay.io/../../outside", "v1", "", ""},
		{"repository backslash", `quay.io/example/a\index`, "v1", "", ""},
		{"repository NUL", "quay.io/example/in\x00dex", "v1", "", ""},
		{"tag dot", repository, ".", "", ""},
		{"tag parent", repository, "..", "", ""},
		{"tag parent with digest", repository, "..", testDigest, ""},
		{"tag slash", repository, "v1/extra", "", ""},
		{"tag backslash", repository, `v1\extra`, "", ""},
		{"tag NUL", repository, "v1\x00", "", ""},
		{"digest dot", repository, "v1", ".", ""},
		{"digest parent", repository, "v1", "..", ""},
		{"digest slash", repository, "v1", testDigest + "/extra", ""},
		{"digest backslash", repository, "v1", testDigest + `\extra`, ""},
		{"digest NUL", repository, "v1", testDigest + "\x00", ""},
	}
	cacheDir := t.TempDir()
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ref := image.ParsedImageURL{RegistryRepository: tc.repository, Tag: tc.tag, Digest: tc.digest}
			got, err := imageCachePath(cacheDir, ref)
			if tc.relativePath == "" {
				if err == nil || got != "" {
					t.Fatalf("unsafe reference = (%q, %v), want empty path and error", got, err)
				}
				return
			}
			want := filepath.Join(cacheDir, filepath.FromSlash(tc.relativePath))
			if err != nil || got != want {
				t.Fatalf("cache path = (%q, %v), want %q", got, err, want)
			}
		})
	}
}

// Traversal must not create directories outside the cache or invoke the runner.
// 1. Choose a nonexistent cache directory under an empty parent directory.
// 2. Call renderOPM with a reference that would escape the cache directory.
// 3. Verify an error, an empty result path, no entries in the parent directory, and no runner calls.
func TestRenderOPM_TraversalCannotEscapeCache(t *testing.T) {
	runner := &fakeRunner{failOnCall: true}
	root := t.TempDir()
	cacheDir := filepath.Join(root, "cache")
	got, err := renderOPM(t.Context(), "quay.io/../../outside:v1", cacheDir, Config{}, runner)
	if err == nil || got != "" {
		t.Errorf("traversal reference = (%q, %v), want empty path and error", got, err)
	}
	// Check directories as well as files, including siblings outside cacheDir.
	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Errorf("traversal reference created filesystem entries: %v", entries)
	}
	runner.assertCalls(t)
}

// Traversal must not alias a valid reference's existing cached result.
// 1. Create a cached result for a valid image reference.
// 2. Call renderOPM with a traversal reference that would otherwise resolve to the same cache path.
// 3. Verify an error, an empty result path, an unchanged cached result, no extra files, and no runner calls.
func TestRenderOPM_TraversalCannotReuseCache(t *testing.T) {
	runner := &fakeRunner{failOnCall: true}
	cacheDir := t.TempDir()
	cached := imageCatalogPath(cacheDir)
	writeTestFile(t, cached, "previous successful render", testFileMode)

	got, err := renderOPM(t.Context(), "quay.io/example/a/../index:v1", cacheDir, Config{}, runner)
	if err == nil || got != "" {
		t.Errorf("traversal reference = (%q, %v), want empty path and error", got, err)
	}
	assertContents(t, cached, "previous successful render")
	assertFiles(t, cacheDir, cached)
	runner.assertCalls(t)
}

// Untagged and explicitly tagged images, including case variants and nested repositories, must coexist regardless of render order.
// Verify that:
// 1. All initial renders succeed.
// 2. Repeated requests return the same paths and correct cached contents.
// 3. Repeated requests do not invoke OPM again.
// 4. No extra files remain in the cache.
func TestRenderOPM_CachePathsDoNotCollide(t *testing.T) {
	for _, reverse := range []bool{false, true} {
		name := "untagged first"
		if reverse {
			name = "tagged first"
		}
		t.Run(name, func(t *testing.T) {
			runner := &fakeRunner{}
			cacheDir := t.TempDir()
			targets := []string{
				"quay.io/example/index",
				"quay.io/example/index:catalog",
				"quay.io/example/index:Catalog",
				"quay.io/example/index:untagged",
				"quay.io/example/index:_refs",
				"quay.io/example/index/catalog",
				"quay.io/example/index/_refs/untagged/no-digest/catalog",
				"quay.io/example/index@" + testDigest,
				"quay.io/example/index:catalog@" + testDigest,
				"quay.io/example/index:Catalog@" + testDigest,
			}
			if reverse {
				slices.Reverse(targets)
			}
			paths := make([]string, len(targets))
			for i, target := range targets {
				runner.payload = target
				path, err := renderOPM(t.Context(), target, cacheDir, Config{}, runner)
				if err != nil {
					t.Fatalf("render %q: %v", target, err)
				}
				for _, previous := range paths[:i] {
					if strings.EqualFold(previous, path) {
						t.Fatalf("cache paths collide under case folding: %q and %q", previous, path)
					}
				}
				paths[i] = path
			}
			runner.failOnCall = true
			for i, target := range targets {
				path, err := renderOPM(t.Context(), target, cacheDir, Config{}, runner)
				if err != nil || path != paths[i] {
					t.Fatalf("cached render %q = (%q, %v), want %q", target, path, err, paths[i])
				}
				assertContents(t, path, target)
			}
			runner.assertCalls(t, targets...)
			slices.Sort(paths)
			assertFiles(t, cacheDir, paths...)
		})
	}
}

// Local inputs are not supported by RenderOPM and fail image-reference validation without invoking OPM.
// 1. Create a local file or directory.
// 2. Call renderOPM with that local path.
// 3. Verify a validation error, an empty result path, no runner calls, and no output files.
func TestRenderOPM_LocalInputIsInvalidImageReference(t *testing.T) {
	for _, kind := range []string{"file", "directory"} {
		t.Run(kind, func(t *testing.T) {
			runner := &fakeRunner{failOnCall: true}
			t.Chdir(t.TempDir())
			target := "./local fbc/catalog.json"
			writeTestFile(t, target, testPayload, testFileMode)
			if kind == "directory" {
				target = "./local fbc"
			}
			cacheDir := t.TempDir()
			got, err := renderOPM(t.Context(), target, cacheDir, Config{}, runner)
			if got != "" || err == nil || !strings.Contains(err.Error(), "image reference") {
				t.Fatalf("local %s = (%q, %v), want empty path and image reference error", kind, got, err)
			}
			runner.assertCalls(t)
			assertFiles(t, cacheDir)
		})
	}
}

// A failed image render leaves no output files, and a later call can succeed for the same image.
// 1. Configure the fake runner to fail for an image.
// 2. Call renderOPM and verify an error, an empty result path, and no output files.
// 3. Configure the next fake render to succeed, call renderOPM again, and verify the complete result.
func TestRenderOPM_FailedRenderCanRecover(t *testing.T) {
	runner := &fakeRunner{renders: []fakeRender{
		{payload: "partial output", shouldRetry: true, err: errors.New("mock render failed")},
		{payload: testPayload},
	}}
	cacheDir := t.TempDir()
	got, err := renderOPM(t.Context(), testImage, cacheDir, Config{}, runner)
	if err == nil || got != "" {
		t.Fatalf("failed render = (%q, %v), want empty path and error", got, err)
	}
	runner.assertCalls(t, testImage)
	assertFiles(t, cacheDir)

	got, err = renderOPM(t.Context(), testImage, cacheDir, Config{}, runner)
	if err != nil {
		t.Fatalf("render after previous failure: %v", err)
	}
	assertContents(t, got, testPayload)
	runner.assertCalls(t, testImage, testImage)
	assertFiles(t, cacheDir, got)
}

// A successful retry replaces the failed attempt's partial output and leaves only the complete result.
// 1. Configure the fake runner to emit partial output and fail once, with one retry allowed.
// 2. Call renderOPM.
// 3. Verify two runner calls and only the successful output in the result file.
func TestRenderOPM_RetryDiscardsPartialOutput(t *testing.T) {
	runner := &fakeRunner{renders: []fakeRender{
		{payload: "partial output", shouldRetry: true, err: errors.New("mock render failed")},
		{payload: testPayload},
	}}
	cacheDir := t.TempDir()
	got, err := renderOPM(t.Context(), testImage, cacheDir, Config{RetryCount: 1}, runner)
	if err != nil {
		t.Fatalf("renderOPM: %v", err)
	}
	assertContents(t, got, testPayload)
	runner.assertCalls(t, testImage, testImage)
	assertFiles(t, cacheDir, got)
}

// Persistent failures exhaust the configured retries and leave no output files.
// 1. Configure the fake runner to always fail and select a retry count.
// 2. Call renderOPM.
// 3. Verify the expected number of attempts, an error, and no output files.
func TestRenderOPM_RetryCount(t *testing.T) {
	for _, tc := range []struct {
		name              string
		retries, attempts int
	}{
		{"zero disables retries", 0, 1},
		{"configured retries", 2, 3},
	} {
		t.Run(tc.name, func(t *testing.T) {
			runner := &fakeRunner{payload: "partial output", shouldRetry: true, err: errors.New("mock render failed")}
			cacheDir := t.TempDir()
			got, err := renderOPM(t.Context(), testImage, cacheDir, Config{RetryCount: tc.retries}, runner)
			if err == nil || got != "" {
				t.Fatalf("exhausted retries = (%q, %v), want empty path and error", got, err)
			}
			wantCalls := make([]string, tc.attempts)
			for i := range wantCalls {
				wantCalls[i] = testImage
			}
			runner.assertCalls(t, wantCalls...)
			assertFiles(t, cacheDir)
		})
	}
}

// Invalid retry settings return an error naming the setting without invoking OPM or creating output files.
// 1. Set an invalid retry count or interval.
// 2. Call renderOPM.
// 3. Verify the error names the setting, the runner was not invoked, and no output files were created.
func TestRenderOPM_InvalidRetrySettings(t *testing.T) {
	for _, tc := range []struct {
		key     string
		cfg     Config
		wantErr error
	}{
		{"RetryCount", Config{RetryCount: -1}, ErrInvalidRetryCount},
		{"RetryInterval", Config{RetryInterval: -time.Nanosecond}, ErrInvalidRetryInterval},
	} {
		t.Run(tc.key, func(t *testing.T) {
			runner := &fakeRunner{failOnCall: true}
			cacheDir := t.TempDir()
			got, err := renderOPM(t.Context(), testImage, cacheDir, tc.cfg, runner)
			if err == nil || got != "" {
				t.Fatalf("invalid setting = (%q, %v), want empty path and error", got, err)
			}
			if !strings.Contains(err.Error(), tc.key) {
				t.Errorf("configuration error must identify %s, got: %v", tc.key, err)
			}
			if !errors.Is(err, tc.wantErr) {
				t.Errorf("configuration error = %v, want %v", err, tc.wantErr)
			}
			runner.assertCalls(t)
			assertFiles(t, cacheDir)
		})
	}
}

// Empty required arguments or an invalid image reference return an error without invoking OPM.
// 1. Provide an empty required argument or an invalid image reference.
// 2. Call RenderOPM.
// 3. Verify an error, an empty result path, and no OPM calls.
func TestRenderOPM_InvalidArguments(t *testing.T) {
	for _, name := range []string{"empty target", "empty opmPath", "empty cacheDir", "invalid image"} {
		t.Run(name, func(t *testing.T) {
			target, opmPath, cacheDir := testImage, "/unused/opm", t.TempDir()
			switch name {
			case "empty target":
				target = ""
			case "empty opmPath":
				opmPath = ""
			case "empty cacheDir":
				cacheDir = ""
			case "invalid image":
				target = "quay.io/example/index@not-a-digest"
			}
			got, err := RenderOPM(t.Context(), target, opmPath, cacheDir, Config{})
			if err == nil || got != "" {
				t.Fatalf("invalid input = (%q, %v), want empty path and error", got, err)
			}
		})
	}
}

// A directory at the expected cached result path is preserved and rejected without invoking OPM.
// 1. Create a directory where the cached result file should be.
// 2. Call renderOPM.
// 3. Verify an error, an empty result path, and no output files.
func TestRenderOPM_DirectoryIsNotCachedResult(t *testing.T) {
	runner := &fakeRunner{failOnCall: true}
	cacheDir := t.TempDir()
	path := imageCatalogPath(cacheDir)
	if err := os.MkdirAll(path, testDirectoryMode); err != nil {
		t.Fatal(err)
	}
	got, err := renderOPM(t.Context(), testImage, cacheDir, Config{}, runner)
	if err == nil || got != "" {
		t.Fatalf("directory at result path = (%q, %v), want empty path and error", got, err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("pre-existing directory was not preserved: %v", err)
	}
	if !info.IsDir() {
		t.Fatalf("cached result path %q is no longer a directory", path)
	}
	runner.assertCalls(t)
	assertFiles(t, cacheDir)
}

// Canceling a running render stops it promptly without retrying or leaving partial output files.
// 1. Start renderOPM with a fake runner that writes partial output and waits.
// 2. Wait for the runner to start, verify no cached result is published, and cancel the context.
// 3. Verify prompt cancellation with context.Canceled, no retry, and no output files.
func TestRenderOPM_CancelRunningProcess(t *testing.T) {
	started := make(chan struct{})
	runner := &fakeRunner{renders: []fakeRender{{
		payload:        "partial output",
		started:        started,
		waitForContext: true,
	}}}
	cacheDir := t.TempDir()
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	done := startRenderWithRunner(ctx, testImage, cacheDir, Config{RetryCount: 3}, runner)
	waitForRunnerStart(t, started, done)
	// Partial stdout must not be visible under the final cache filename.
	if _, err := os.Stat(imageCatalogPath(cacheDir)); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("result was published before OPM finished: stat error = %v", err)
	}
	cancel()
	assertCanceledResult(t, done, context.Canceled)
	runner.assertCalls(t, testImage)
	assertFiles(t, cacheDir)
}

// Completed output becomes a cache entry only after the runner exits successfully.
// 1. Start renderOPM with a fake runner that blocks after writing complete output.
// 2. Verify no cached result is published while the runner is blocked.
// 3. Release the runner and verify the complete output is published.
func TestRenderOPM_PublishesAfterSuccess(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	runner := &fakeRunner{renders: []fakeRender{{
		payload: testPayload,
		started: started,
		release: release,
	}}}
	cacheDir := t.TempDir()
	ctx, cancel := context.WithTimeout(t.Context(), testRunnerStartTimeout)
	defer cancel()
	done := startRenderWithRunner(ctx, testImage, cacheDir, Config{}, runner)
	waitForRunnerStart(t, started, done)
	want := imageCatalogPath(cacheDir)
	if _, err := os.Stat(want); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("result published before OPM finished: %v", err)
	}
	close(release)
	select {
	case result := <-done:
		if result.err != nil || result.path != want {
			t.Fatalf("render = (%q, %v), want %q", result.path, result.err, want)
		}
	case <-ctx.Done():
		t.Fatal("render did not finish before the deadline")
	}
	assertContents(t, want, testPayload)
	assertFiles(t, cacheDir, want)
	runner.assertCalls(t, testImage)
}

// A regular file at the cache directory path causes an error without invoking OPM or changing the file.
// 1. Create a regular file where the cache directory should be.
// 2. Call renderOPM for an image using that cache path.
// 3. Verify a directory error, an empty result path, unchanged file contents, and no runner calls.
func TestRenderOPM_OutputDirectoryError(t *testing.T) {
	runner := &fakeRunner{failOnCall: true}
	cacheDir := filepath.Join(t.TempDir(), "cache")
	writeTestFile(t, cacheDir, "existing file", testFileMode)
	got, err := renderOPM(t.Context(), testImage, cacheDir, Config{}, runner)
	if got != "" || err == nil || !strings.Contains(err.Error(), "output directory") {
		t.Fatalf("invalid output directory = (%q, %v), want empty path and directory error", got, err)
	}
	assertContents(t, cacheDir, "existing file")
	runner.assertCalls(t)
}

// A missing OPM executable returns an error without retrying and leaves no output files.
// 1. Choose a nonexistent OPM executable path and an empty cache directory.
// 2. Call RenderOPM with retries configured and a retry interval longer than the context deadline.
// 3. Verify a not-exist error, an empty result path, and no retry wait or output files.
func TestRenderOPM_MissingExecutable(t *testing.T) {
	cacheDir := t.TempDir()
	ctx, cancel := context.WithTimeout(t.Context(), testRetryCancelTimeout)
	defer cancel()
	got, err := RenderOPM(ctx, testImage, filepath.Join(t.TempDir(), "missing-opm"), cacheDir, Config{RetryCount: 3, RetryInterval: time.Hour})
	if got != "" || !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("missing executable = (%q, %v), want empty path and not-exist error", got, err)
	}
	assertFiles(t, cacheDir)
}

// ── execOPMRunner ───────────────────────────────────────────

// The production runner invokes the configured executable as "opm render <image>" and streams stdout.
// Render logic is covered above through renderOPM with fakeRunner.
// 1. Create a test executable that accepts only the expected command shape.
// 2. Call execOPMRunner.Render directly.
// 3. Verify the runner does not request a retry and writes stdout to the provided output.
func TestExecOPMRunner_RenderPassesArgumentsAndStreamsOutput(t *testing.T) {
	script := filepath.Join(t.TempDir(), "opm")
	writeTestFile(t, script, `#!/bin/sh
if [ "$#" -ne 2 ] || [ "$1" != "render" ] || [ "$2" != "quay.io/example/index:v1" ]; then
	echo "bad args: $*" >&2
	exit 64
fi
printf 'exec payload\n'
`, testExecutableMode)
	var output strings.Builder
	shouldRetry, err := execOPMRunner{opmPath: script}.Render(t.Context(), testImage, &output)
	if err != nil || shouldRetry {
		t.Fatalf("exec render = (%v, %v), want no retry and no error", shouldRetry, err)
	}
	if got := output.String(); got != "exec payload\n" {
		t.Fatalf("exec output = %q, want payload", got)
	}
}

// A context deadline interrupts the retry wait before another attempt and leaves no output files.
// 1. Configure the fake runner to fail with a retry interval longer than the context deadline.
// 2. Start renderOPM and let the deadline interrupt the wait after the first attempt.
// 3. Verify context.DeadlineExceeded, no second runner call, and no output files.
func TestRenderOPM_CancelRetryWait(t *testing.T) {
	started := make(chan struct{})
	runner := &fakeRunner{renders: []fakeRender{{
		payload:     "partial output",
		shouldRetry: true,
		err:         errors.New("mock render failed"),
		started:     started,
	}}}
	cacheDir := t.TempDir()
	ctx, cancel := context.WithTimeout(t.Context(), testRetryCancelTimeout)
	defer cancel()
	done := startRenderWithRunner(ctx, testImage, cacheDir, Config{RetryCount: 3, RetryInterval: time.Hour}, runner)
	waitForRunnerStart(t, started, done)
	assertCanceledResult(t, done, context.DeadlineExceeded)
	runner.assertCalls(t, testImage)
	assertFiles(t, cacheDir)
}

// fakeRender describes one fake runner attempt.
type fakeRender struct {
	payload        string
	shouldRetry    bool
	err            error
	started        chan<- struct{}
	release        <-chan struct{}
	waitForContext bool
}

// fakeRunner records render targets and returns configured render results.
type fakeRunner struct {
	mu          sync.Mutex
	payload     string
	shouldRetry bool
	err         error
	calls       []string
	renders     []fakeRender
	failOnCall  bool
}

// Render writes the configured payload and returns the configured retry decision.
func (r *fakeRunner) Render(ctx context.Context, target string, output io.Writer) (bool, error) {
	render := r.nextRender(target)
	if r.failOnCall {
		return false, errors.New("unexpected runner call")
	}
	if render.payload != "" {
		if _, err := io.WriteString(output, render.payload); err != nil {
			return false, err
		}
	}
	if render.started != nil {
		close(render.started)
	}
	if render.waitForContext {
		<-ctx.Done()
		return false, ctx.Err()
	}
	if render.release != nil {
		select {
		case <-render.release:
		case <-ctx.Done():
			return false, ctx.Err()
		}
	}
	return render.shouldRetry, render.err
}

// nextRender records target and returns the per-call or default fake render result.
func (r *fakeRunner) nextRender(target string) fakeRender {
	r.mu.Lock()
	defer r.mu.Unlock()

	callIndex := len(r.calls)
	r.calls = append(r.calls, target)
	render := fakeRender{
		payload:     r.payload,
		shouldRetry: r.shouldRetry,
		err:         r.err,
	}
	if callIndex < len(r.renders) {
		render = r.renders[callIndex]
	}
	return render
}

// assertCalls checks the runner's recorded targets in call order.
func (r *fakeRunner) assertCalls(t *testing.T, want ...string) {
	t.Helper()
	r.mu.Lock()
	defer r.mu.Unlock()
	if !slices.Equal(r.calls, want) {
		t.Fatalf("runner targets = %q, want %q", r.calls, want)
	}
}

// imageCatalogPath returns the expected cached catalog path for testImage.
func imageCatalogPath(cacheDir string) string {
	return filepath.Join(cacheDir, filepath.FromSlash(testRepositoryCachePath), "_refs", "tagged-base32", "oyyq", "no-digest", "catalog")
}

// assertContents checks that a file contains exactly the expected text.
func assertContents(t *testing.T, path, want string) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != want {
		t.Errorf("contents of %s = %q, want %q", path, data, want)
	}
}

// assertFiles ignores empty directories but catches any abandoned partial output.
func assertFiles(t *testing.T, root string, want ...string) {
	t.Helper()
	var got []string
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if errors.Is(err, fs.ErrNotExist) && path == root {
			return nil
		}
		if err != nil {
			return err
		}
		if !entry.IsDir() {
			got = append(got, path)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	slices.Sort(got)
	want = slices.Clone(want)
	slices.Sort(want)
	if !slices.Equal(got, want) {
		t.Errorf("remaining files = %q, want %q", got, want)
	}
}

// renderResult holds the catalog path and error returned by an asynchronous render.
type renderResult struct {
	path string
	err  error
}

// startRenderWithRunner runs renderOPM asynchronously and returns a channel for its result.
func startRenderWithRunner(ctx context.Context, target, cacheDir string, cfg Config, runner opmRunner) <-chan renderResult {
	done := make(chan renderResult, 1)
	go func() {
		path, err := renderOPM(ctx, target, cacheDir, cfg, runner)
		done <- renderResult{path, err}
	}()
	return done
}

// waitForRunnerStart waits for a fake runner attempt to begin.
func waitForRunnerStart(t *testing.T, started <-chan struct{}, done <-chan renderResult) {
	t.Helper()
	select {
	case <-started:
	case result := <-done:
		t.Fatalf("render returned before runner started: (%q, %v)", result.path, result.err)
	case <-time.After(testRunnerStartTimeout):
		t.Fatalf("runner did not start within %s", testRunnerStartTimeout)
	}
}

// assertCanceledResult checks that rendering stops within the timeout with the expected context error and no result path.
func assertCanceledResult(t *testing.T, done <-chan renderResult, want error) {
	t.Helper()
	select {
	case result := <-done:
		if result.path != "" || !errors.Is(result.err, want) {
			t.Fatalf("canceled render = (%q, %v), want empty path and %v", result.path, result.err, want)
		}
	case <-time.After(testCancellationTimeout):
		t.Fatal("render did not stop promptly after context cancellation")
	}
}
