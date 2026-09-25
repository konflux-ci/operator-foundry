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
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/konflux-ci/operator-foundry/pkg/image"
)

const (
	testDirectoryMode  = 0755
	testExecutableMode = 0700
	testFileMode       = 0600
	testImage          = "quay.io/example/index:v1"
	testDigest         = "sha256:1111111111111111111111111111111111111111111111111111111111111111"
	testPayload        = "{\"schema\":\"olm.package\",\"name\":\"example\"}\n"
	// Expected SHA-256 cache prefixes for quay.io/example/index and localhost:5000/example/index.
	testRepositoryCachePath   = "repositories-sha256/c43e683059d2605481b2596467e290717b14752a7ee70163b6f8ea1aff2096fd"
	testRegistryPortCachePath = "repositories-sha256/047cf8f3e5a55f861244b86ca83cb199a41e3e8c36b464848c4f8e4db0f9b6cb"

	testDefaultRetryInterval    = 5 * time.Second
	testFractionalRetryInterval = 20 * time.Millisecond
	testRetryCancelTimeout      = 2 * time.Second
	testMockStartTimeout        = 5 * time.Second
	testMockPollInterval        = 5 * time.Millisecond
	testCancellationTimeout     = 3 * time.Second
)

// An uncached image is rendered once and saved at the expected cache path for its reference format.
// 1. Choose an image reference with no cached result.
// 2. Call RenderOPM.
// 3. Verify one OPM call and the expected contents at the correct cache path, with no extra files.
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
			opm := newMockOPM(t)
			cacheDir := filepath.Join(t.TempDir(), "new_cache")
			got, err := RenderOPM(t.Context(), tc.target, opm.path, cacheDir)
			if err != nil {
				t.Fatalf("RenderOPM: %v", err)
			}
			want := filepath.Join(cacheDir, filepath.FromSlash(tc.relativePath))
			if got != want {
				t.Fatalf("result path = %q, want %q", got, want)
			}
			assertContents(t, got, testPayload)
			opm.assertCalls(t, tc.target)
			assertFiles(t, cacheDir, want)
		})
	}
}

// An existing cached result is returned unchanged without invoking OPM or creating extra files.
// 1. Create a cached result and configure OPM to fail if invoked.
// 2. Call RenderOPM for the same image.
// 3. Verify the existing result is unchanged, OPM was not invoked, and no extra files appeared.
func TestRenderOPM_CacheHit(t *testing.T) {
	opm := newMockOPM(t)
	opm.setMode(t, "fail")
	cacheDir := t.TempDir()
	want := imageCatalogPath(cacheDir)
	writeTestFile(t, want, "previous successful render", testFileMode)

	got, err := RenderOPM(t.Context(), testImage, opm.path, cacheDir)
	if err != nil {
		t.Fatalf("RenderOPM: %v", err)
	}
	if got != want {
		t.Fatalf("result path = %q, want %q", got, want)
	}
	assertContents(t, got, "previous successful render")
	opm.assertCalls(t)
	assertFiles(t, cacheDir, want)
}

// A long image reference must render and reuse its cache even when its registry exists locally.
// 1. Create the registry directory so probing the reference reaches the oversized filename.
// 2. Verify the probe fails with ENAMETOOLONG, then render the image twice.
// 3. Verify the unchanged reference reaches OPM once and the cached output is reused.
func TestRenderOPM_LongImageReference(t *testing.T) {
	opm := newMockOPM(t)
	t.Chdir(t.TempDir())
	if err := os.Mkdir("quay.io", testDirectoryMode); err != nil {
		t.Fatal(err)
	}
	repository := strings.Repeat("r", 100)
	tag := strings.Repeat("t", 128)

	target := "quay.io/" + repository + ":" + tag + "@" + testDigest
	if _, err := os.Stat(target); !errors.Is(err, syscall.ENAMETOOLONG) {
		t.Fatalf("target probe = %v, want ENAMETOOLONG", err)
	}
	cacheDir := t.TempDir()
	want := filepath.Join(cacheDir, "repositories-sha256", "17ec59b541607a89300cb3a1530da071837e277425c1bf3a7556042f1bf42ec9", "_refs", "tagged-base32", strings.Repeat("or2hi5du", 25)+"or2hi", "digest", testDigest, "catalog")
	for range 2 {
		got, err := RenderOPM(t.Context(), target, opm.path, cacheDir)
		if err != nil || got != want {
			t.Fatalf("long image render = (%q, %v), want %q", got, err, want)
		}
		assertContents(t, got, testPayload)
		opm.setMode(t, "fail")
	}
	opm.assertCalls(t, target)
	assertFiles(t, cacheDir, want)
}

// A regular file named after the registry must not prevent image rendering.
func TestRenderOPM_ImageWithNonDirectoryPathComponent(t *testing.T) {
	opm := newMockOPM(t)
	t.Chdir(t.TempDir())
	writeTestFile(t, "quay.io", "local file", testFileMode)
	if _, err := os.Stat(testImage); !errors.Is(err, syscall.ENOTDIR) {
		t.Fatalf("target probe = %v, want ENOTDIR", err)
	}
	cacheDir := t.TempDir()
	want := imageCatalogPath(cacheDir)
	for range 2 {
		got, err := RenderOPM(t.Context(), testImage, opm.path, cacheDir)
		if err != nil || got != want {
			t.Fatalf("image render = (%q, %v), want %q", got, err, want)
		}
		assertContents(t, got, testPayload)
		opm.setMode(t, "fail")
	}
	opm.assertCalls(t, testImage)
	assertContents(t, "quay.io", "local file")
	assertFiles(t, cacheDir, want)
}

// RenderOPM checks whether the target is a local path before parsing it as an image.
// Falling back to image parsing must not hide permission errors or symlink loops.
func TestRenderOPM_TargetProbeErrors(t *testing.T) {
	for _, kind := range []string{"symlink loop", "permission denied"} {
		t.Run(kind, func(t *testing.T) {
			opm := newMockOPM(t)
			t.Chdir(t.TempDir())
			wantErr := error(syscall.ELOOP)
			if kind == "symlink loop" {
				if err := os.Symlink("quay.io", "quay.io"); err != nil {
					t.Fatal(err)
				}
			} else {
				if err := os.Mkdir("quay.io", testDirectoryMode); err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() {
					if err := os.Chmod("quay.io", testDirectoryMode); err != nil {
						t.Error(err)
					}
				})
				if err := os.Chmod("quay.io", 0); err != nil {
					t.Fatal(err)
				}
				wantErr = fs.ErrPermission
				if _, err := os.Stat(testImage); !errors.Is(err, wantErr) {
					t.Skipf("filesystem does not enforce directory permissions: %v", err)
				}
			}
			cacheDir := t.TempDir()
			got, err := RenderOPM(t.Context(), testImage, opm.path, cacheDir)
			if got != "" || !errors.Is(err, wantErr) || !strings.Contains(err.Error(), "inspect target") {
				t.Fatalf("target probe = (%q, %v), want empty path and wrapped %v", got, err, wantErr)
			}
			opm.assertCalls(t)
			assertFiles(t, cacheDir)
		})
	}
}

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

// Traversal must not create directories outside the cache or invoke OPM.
// 1. Choose a nonexistent cache directory under an empty parent directory.
// 2. Call RenderOPM with a reference that would escape the cache directory.
// 3. Verify an error, an empty result path, no entries in the parent directory, and no OPM calls.
func TestRenderOPM_TraversalCannotEscapeCache(t *testing.T) {
	opm := newMockOPM(t)
	root := t.TempDir()
	cacheDir := filepath.Join(root, "cache")
	got, err := RenderOPM(t.Context(), "quay.io/../../outside:v1", opm.path, cacheDir)
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
	opm.assertCalls(t)
}

// Traversal must not alias a valid reference's existing cached result.
// 1. Create a cached result for a valid image reference.
// 2. Call RenderOPM with a traversal reference that would otherwise resolve to the same cache path.
// 3. Verify an error, an empty result path, an unchanged cached result, no extra files, and no OPM calls.
func TestRenderOPM_TraversalCannotReuseCache(t *testing.T) {
	opm := newMockOPM(t)
	cacheDir := t.TempDir()
	cached := imageCatalogPath(cacheDir)
	writeTestFile(t, cached, "previous successful render", testFileMode)

	got, err := RenderOPM(t.Context(), "quay.io/example/a/../index:v1", opm.path, cacheDir)
	if err == nil || got != "" {
		t.Errorf("traversal reference = (%q, %v), want empty path and error", got, err)
	}
	assertContents(t, cached, "previous successful render")
	assertFiles(t, cacheDir, cached)
	opm.assertCalls(t)
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
			opm := newMockOPM(t)
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
				opm.setPayload(t, target)
				path, err := RenderOPM(t.Context(), target, opm.path, cacheDir)
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
			opm.setMode(t, "fail")
			for i, target := range targets {
				path, err := RenderOPM(t.Context(), target, opm.path, cacheDir)
				if err != nil || path != paths[i] {
					t.Fatalf("cached render %q = (%q, %v), want %q", target, path, err, paths[i])
				}
				assertContents(t, path, target)
			}
			opm.assertCalls(t, targets...)
			slices.Sort(paths)
			assertFiles(t, cacheDir, paths...)
		})
	}
}

// An existing cache directory without a result file still triggers rendering.
// 1. Create the cache directory without a result file.
// 2. Call RenderOPM.
// 3. Verify OPM was invoked once and its output was saved at the expected path, with no extra files.
func TestRenderOPM_CacheDirectoryWithoutResult(t *testing.T) {
	opm := newMockOPM(t)
	cacheDir := t.TempDir()
	want := imageCatalogPath(cacheDir)
	if err := os.MkdirAll(filepath.Dir(want), testDirectoryMode); err != nil {
		t.Fatal(err)
	}
	got, err := RenderOPM(t.Context(), testImage, opm.path, cacheDir)
	if err != nil {
		t.Fatalf("RenderOPM: %v", err)
	}
	if got != want {
		t.Fatalf("result path = %q, want %q", got, want)
	}
	assertContents(t, got, testPayload)
	opm.assertCalls(t, testImage)
	assertFiles(t, cacheDir, want)
}

// Rendering the same local input twice invokes OPM twice and saves the new output without overwriting the first result.
// 1. Create a local input.
// 2. Call RenderOPM twice.
// 3. Verify that OPM was invoked twice and both calls succeeded.
func TestRenderOPM_LocalInputAlwaysRenders(t *testing.T) {
	for _, kind := range []string{"file", "directory"} {
		t.Run(kind, func(t *testing.T) {
			opm := newMockOPM(t)
			// Relative paths and spaces must reach OPM as a single, unchanged argument.
			t.Chdir(t.TempDir())
			target := "./local fbc/catalog.json"
			writeTestFile(t, target, testPayload, testFileMode)
			if kind == "directory" {
				target = "./local fbc"
			}
			cacheDir := filepath.Join(t.TempDir(), "local results")
			first, err := RenderOPM(t.Context(), target, opm.path, cacheDir)
			if err != nil {
				t.Fatalf("first render: %v", err)
			}
			// The mock returns this new output on the next call; it does not read the input file.
			updated := "{\"schema\":\"olm.package\",\"name\":\"updated\"}\n"
			opm.setPayload(t, updated)
			second, err := RenderOPM(t.Context(), target, opm.path, cacheDir)
			if err != nil {
				t.Fatalf("second render: %v", err)
			}
			if first == second {
				t.Fatalf("local renders reused the same result path: %s", first)
			}
			assertContents(t, first, testPayload)
			assertContents(t, second, updated)
			opm.assertCalls(t, target, target)
			want := []string{first, second}
			slices.Sort(want)
			assertFiles(t, cacheDir, want...)
		})
	}
}

// A failed render leaves no output files, and a later call can succeed for the same image or local input.
// 1. Configure OPM to fail for an image or local input.
// 2. Call RenderOPM and verify an error, an empty result path, and no output files.
// 3. Configure OPM to succeed, call RenderOPM again, and verify the complete result.
func TestRenderOPM_FailedRenderCanRecover(t *testing.T) {
	localDir := t.TempDir()
	localFile := filepath.Join(localDir, "catalog.json")
	writeTestFile(t, localFile, testPayload, testFileMode)

	for _, tc := range []struct{ name, target string }{
		{"image", testImage},
		{"local file", localFile},
		{"local directory", localDir},
	} {
		t.Run(tc.name, func(t *testing.T) {
			opm := newMockOPM(t)
			opm.setMode(t, "fail")
			cacheDir := t.TempDir()
			got, err := RenderOPM(t.Context(), tc.target, opm.path, cacheDir)
			if err == nil || got != "" {
				t.Fatalf("failed render = (%q, %v), want empty path and error", got, err)
			}
			opm.assertCalls(t, tc.target)
			assertFiles(t, cacheDir)

			opm.setMode(t, "success")
			got, err = RenderOPM(t.Context(), tc.target, opm.path, cacheDir)
			if err != nil {
				t.Fatalf("render after previous failure: %v", err)
			}
			assertContents(t, got, testPayload)
			opm.assertCalls(t, tc.target, tc.target)
			assertFiles(t, cacheDir, got)
		})
	}
}

// A successful retry replaces the failed attempt's partial output and leaves only the complete result.
// 1. Configure OPM to emit partial output and fail once, with one retry allowed.
// 2. Call RenderOPM.
// 3. Verify two OPM calls and only the successful output in the result file.
func TestRenderOPM_RetryDiscardsPartialOutput(t *testing.T) {
	opm := newMockOPM(t)
	opm.setMode(t, "fail-once")
	t.Setenv("RETRY_COUNT", "1")
	cacheDir := t.TempDir()
	got, err := RenderOPM(t.Context(), testImage, opm.path, cacheDir)
	if err != nil {
		t.Fatalf("RenderOPM: %v", err)
	}
	assertContents(t, got, testPayload)
	opm.assertCalls(t, testImage, testImage)
	assertFiles(t, cacheDir, got)
}

// Persistent failures exhaust the default or configured retries and leave no output files.
// 1. Configure OPM to always fail and select a retry count or its default.
// 2. Call RenderOPM.
// 3. Verify the expected number of attempts, an error, and no output files.
func TestRenderOPM_RetryCount(t *testing.T) {
	for _, tc := range []struct {
		name, value string
		unset       bool
		attempts    int
	}{
		{"unset uses default", "", true, 4},
		{"empty uses default", "", false, 4},
		{"zero disables retries", "0", false, 1},
		{"configured retries", "2", false, 3},
	} {
		t.Run(tc.name, func(t *testing.T) {
			opm := newMockOPM(t)
			opm.setMode(t, "fail")
			t.Setenv("RETRY_COUNT", tc.value)
			if tc.unset {
				unsetTestEnv(t, "RETRY_COUNT")
			}
			cacheDir := t.TempDir()
			got, err := RenderOPM(t.Context(), testImage, opm.path, cacheDir)
			if err == nil || got != "" {
				t.Fatalf("exhausted retries = (%q, %v), want empty path and error", got, err)
			}
			wantCalls := make([]string, tc.attempts)
			for i := range wantCalls {
				wantCalls[i] = testImage
			}
			opm.assertCalls(t, wantCalls...)
			assertFiles(t, cacheDir)
		})
	}
}

// An unset or empty interval defaults to five seconds, while zero and fractional seconds are parsed as configured.
// 1. Unset RETRY_INTERVAL or set it to an empty, zero, or fractional value.
// 2. Call readOPMRetrySettings.
// 3. Verify the expected duration is returned without an error.
func TestReadOPMRetrySettings_Interval(t *testing.T) {
	for _, tc := range []struct {
		name, value string
		unset       bool
		want        time.Duration
	}{
		{"unset uses five seconds", "", true, testDefaultRetryInterval},
		{"empty uses five seconds", "", false, testDefaultRetryInterval},
		{"zero disables delay", "0", false, 0},
		{"fractional seconds", "0.02", false, testFractionalRetryInterval},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("RETRY_COUNT", "1")
			t.Setenv("RETRY_INTERVAL", tc.value)
			if tc.unset {
				unsetTestEnv(t, "RETRY_INTERVAL")
			}
			_, got, err := readOPMRetrySettings()
			if err != nil {
				t.Fatalf("readOPMRetrySettings: %v", err)
			}
			if got != tc.want {
				t.Errorf("retry interval = %s, want %s", got, tc.want)
			}
		})
	}
}

// Invalid retry settings return an error naming the setting without invoking OPM or creating output files.
// 1. Set an invalid retry count or interval.
// 2. Call RenderOPM.
// 3. Verify the error names the setting, OPM was not invoked, and no output files were created.
func TestRenderOPM_InvalidRetrySettings(t *testing.T) {
	for _, tc := range []struct{ key, value string }{
		{"RETRY_COUNT", "-1"}, {"RETRY_COUNT", "1.5"}, {"RETRY_COUNT", "invalid"},
		{"RETRY_INTERVAL", "-1"}, {"RETRY_INTERVAL", "invalid"},
		{"RETRY_INTERVAL", "NaN"}, {"RETRY_INTERVAL", "Inf"},
	} {
		t.Run(tc.key+"="+tc.value, func(t *testing.T) {
			opm := newMockOPM(t)
			t.Setenv(tc.key, tc.value)
			cacheDir := t.TempDir()
			got, err := RenderOPM(t.Context(), testImage, opm.path, cacheDir)
			if err == nil || got != "" {
				t.Fatalf("invalid setting = (%q, %v), want empty path and error", got, err)
			}
			if !strings.Contains(err.Error(), tc.key) {
				t.Errorf("configuration error must identify %s, got: %v", tc.key, err)
			}
			opm.assertCalls(t)
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
			opm := newMockOPM(t)
			target, opmPath, cacheDir := testImage, opm.path, t.TempDir()
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
			got, err := RenderOPM(t.Context(), target, opmPath, cacheDir)
			if err == nil || got != "" {
				t.Fatalf("invalid input = (%q, %v), want empty path and error", got, err)
			}
			opm.assertCalls(t)
		})
	}
}

// A directory at the expected cached result path is preserved and rejected without invoking OPM.
// 1. Create a directory where the cached result file should be.
// 2. Call RenderOPM.
// 3. Verify an error, an empty result path, and no output files.
func TestRenderOPM_DirectoryIsNotCachedResult(t *testing.T) {
	opm := newMockOPM(t)
	cacheDir := t.TempDir()
	path := imageCatalogPath(cacheDir)
	if err := os.MkdirAll(path, testDirectoryMode); err != nil {
		t.Fatal(err)
	}
	got, err := RenderOPM(t.Context(), testImage, opm.path, cacheDir)
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
	opm.assertCalls(t)
	assertFiles(t, cacheDir)
}

// Canceling a running render stops it promptly without retrying or leaving partial output files.
// 1. Start RenderOPM with a mock that writes partial output and waits.
// 2. Wait for OPM to start, verify no cached result is published, and cancel the context.
// 3. Verify prompt cancellation with context.Canceled, no retry, and no output files.
func TestRenderOPM_CancelRunningProcess(t *testing.T) {
	opm := newMockOPM(t)
	opm.setMode(t, "block")
	t.Setenv("RETRY_COUNT", "3")
	cacheDir := t.TempDir()
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	done := startRender(ctx, testImage, opm.path, cacheDir)
	waitForMockFile(t, filepath.Join(opm.state, "started"), done)
	// Partial stdout must not be visible under the final cache filename.
	if _, err := os.Stat(imageCatalogPath(cacheDir)); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("result was published before OPM finished: stat error = %v", err)
	}
	cancel()
	assertCanceledResult(t, done, context.Canceled)
	opm.assertCalls(t, testImage)
	assertFiles(t, cacheDir)
}

// Completed output becomes a cache entry only after OPM exits successfully.
func TestRenderOPM_PublishesAfterSuccess(t *testing.T) {
	opm := newMockOPM(t)
	opm.setMode(t, "wait-success")
	cacheDir := t.TempDir()
	ctx, cancel := context.WithTimeout(t.Context(), testMockStartTimeout)
	defer cancel()
	done := startRender(ctx, testImage, opm.path, cacheDir)
	waitForMockFile(t, filepath.Join(opm.state, "started"), done)
	want := imageCatalogPath(cacheDir)
	if _, err := os.Stat(want); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("result published before OPM finished: %v", err)
	}
	writeTestFile(t, filepath.Join(opm.state, "release"), "", testFileMode)
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
	opm.assertCalls(t, testImage)
}

// A regular file at the cache directory path causes an error without invoking OPM or changing the file.
// 1. Create a regular file where the cache directory should be.
// 2. Call RenderOPM for an image using that cache path.
// 3. Verify a directory error, an empty result path, unchanged file contents, and no OPM calls.
func TestRenderOPM_OutputDirectoryError(t *testing.T) {
	opm := newMockOPM(t)
	cacheDir := filepath.Join(t.TempDir(), "cache")
	writeTestFile(t, cacheDir, "existing file", testFileMode)
	got, err := RenderOPM(t.Context(), testImage, opm.path, cacheDir)
	if got != "" || err == nil || !strings.Contains(err.Error(), "output directory") {
		t.Fatalf("invalid output directory = (%q, %v), want empty path and directory error", got, err)
	}
	assertContents(t, cacheDir, "existing file")
	opm.assertCalls(t)
}

// A missing OPM executable returns an error and leaves no output files.
// 1. Choose a nonexistent OPM executable path and an empty cache directory.
// 2. Call RenderOPM for an image using that executable path.
// 3. Verify a not-exist error, an empty result path, no output files, and no mock OPM calls.
func TestRenderOPM_MissingExecutable(t *testing.T) {
	opm := newMockOPM(t)
	cacheDir := t.TempDir()
	got, err := RenderOPM(t.Context(), testImage, filepath.Join(opm.state, "missing-opm"), cacheDir)
	if got != "" || !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("missing executable = (%q, %v), want empty path and not-exist error", got, err)
	}
	assertFiles(t, cacheDir)
	opm.assertCalls(t)
}

// A context deadline interrupts the retry wait before another attempt and leaves no output files.
// 1. Configure OPM to fail with a retry interval longer than the context deadline.
// 2. Start RenderOPM and let the deadline interrupt the wait after the first attempt.
// 3. Verify context.DeadlineExceeded, no second OPM call, and no output files.
func TestRenderOPM_CancelRetryWait(t *testing.T) {
	for _, interval := range []string{"", "3600"} {
		t.Run("interval="+interval, func(t *testing.T) {
			opm := newMockOPM(t)
			opm.setMode(t, "fail")
			t.Setenv("RETRY_COUNT", "3")
			t.Setenv("RETRY_INTERVAL", interval)
			cacheDir := t.TempDir()
			ctx, cancel := context.WithTimeout(t.Context(), testRetryCancelTimeout)
			defer cancel()
			done := startRender(ctx, testImage, opm.path, cacheDir)
			waitForMockFile(t, filepath.Join(opm.state, "calls"), done)
			assertCanceledResult(t, done, context.DeadlineExceeded)
			opm.assertCalls(t, testImage)
			assertFiles(t, cacheDir)
		})
	}
}

type mockOPM struct{ path, state string }

func newMockOPM(t *testing.T) mockOPM {
	t.Helper()
	state := t.TempDir()
	script, err := os.ReadFile("testdata/mock_opm.sh")
	if err != nil {
		t.Fatal(err)
	}
	opm := mockOPM{path: filepath.Join(state, "mock opm.sh"), state: state}
	writeTestFile(t, opm.path, string(script), testExecutableMode)
	opm.setMode(t, "success")
	opm.setPayload(t, testPayload)
	t.Setenv("TEST_OPM_STATE", state)
	t.Setenv("RETRY_COUNT", "0")
	t.Setenv("RETRY_INTERVAL", "0")
	return opm
}

// setMode controls how the mock OPM process behaves:
//   - success: write the configured payload and exit successfully.
//   - fail: write partial output and exit with an error on every call.
//   - fail-once: fail with partial output on the first call, then succeed on later calls.
//   - block: write partial output, then sleep for 30 seconds unless canceled earlier.
//   - wait-success: write the configured payload, then wait for the release file before succeeding.
func (m mockOPM) setMode(t *testing.T, mode string) {
	t.Helper()
	writeTestFile(t, filepath.Join(m.state, "mode"), mode, testFileMode)
}

func (m mockOPM) setPayload(t *testing.T, payload string) {
	t.Helper()
	writeTestFile(t, filepath.Join(m.state, "payload"), payload, testFileMode)
}

func (m mockOPM) assertCalls(t *testing.T, want ...string) {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(m.state, "calls"))
	var got []string
	if err == nil {
		if len(data) > 0 {
			got = strings.Split(strings.TrimSuffix(string(data), "\n"), "\n")
		}
	} else if !errors.Is(err, fs.ErrNotExist) {
		t.Fatal(err)
	}
	if !slices.Equal(got, want) {
		t.Fatalf("OPM targets = %q, want %q", got, want)
	}
}

func TestMockOPM_EmptyCallsFile(t *testing.T) {
	opm := newMockOPM(t)
	writeTestFile(t, filepath.Join(opm.state, "calls"), "", testFileMode)
	opm.assertCalls(t)
}

func imageCatalogPath(cacheDir string) string {
	return filepath.Join(cacheDir, filepath.FromSlash(testRepositoryCachePath), "_refs", "tagged-base32", "oyyq", "no-digest", "catalog")
}

func writeTestFile(t *testing.T, path, contents string, mode fs.FileMode) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), testDirectoryMode); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(contents), mode); err != nil {
		t.Fatal(err)
	}
}

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
// want must be sorted lexically to match filepath.WalkDir traversal order.
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
	if !slices.Equal(got, want) {
		t.Errorf("remaining files = %q, want %q", got, want)
	}
}

func unsetTestEnv(t *testing.T, key string) {
	t.Helper()
	// Setenv registers restoration of the original value, even after Unsetenv.
	t.Setenv(key, "")
	if err := os.Unsetenv(key); err != nil {
		t.Fatal(err)
	}
}

type renderResult struct {
	path string
	err  error
}

func startRender(ctx context.Context, target, opmPath, cacheDir string) <-chan renderResult {
	done := make(chan renderResult, 1)
	go func() {
		path, err := RenderOPM(ctx, target, opmPath, cacheDir)
		done <- renderResult{path, err}
	}()
	return done
}

func waitForMockFile(t *testing.T, path string, done <-chan renderResult) {
	t.Helper()
	timer := time.NewTimer(testMockStartTimeout)
	defer timer.Stop()
	ticker := time.NewTicker(testMockPollInterval)
	defer ticker.Stop()
	for {
		if _, err := os.Stat(path); err == nil {
			return
		} else if !errors.Is(err, fs.ErrNotExist) {
			t.Fatal(err)
		}
		select {
		case result := <-done:
			t.Fatalf("render returned before mock started: (%q, %v)", result.path, result.err)
		case <-timer.C:
			t.Fatalf("mock OPM did not start within %s", testMockStartTimeout)
		case <-ticker.C:
		}
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
