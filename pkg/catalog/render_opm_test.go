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
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"
)

const (
	testDirectoryMode = 0755
	testFileMode      = 0600
	testImage         = "quay.io/example/index:v1"
	testDigest        = "sha256:1111111111111111111111111111111111111111111111111111111111111111"
	testPayload       = "{\"schema\":\"olm.package\",\"name\":\"example\"}\n"

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
func TestRenderOPM_CacheMiss(t *testing.T) {
	cases := []struct {
		name, target, relativePath string
	}{
		{"untagged", "quay.io/example/index", "quay.io/example/index/catalog"},
		{"tag", testImage, "quay.io/example/index/v1/catalog"},
		{"digest", "quay.io/example/index@" + testDigest, "quay.io/example/index/" + testDigest + "/catalog"},
		{"tag and digest", testImage + "@" + testDigest, "quay.io/example/index/v1/" + testDigest + "/catalog"},
		{"registry port", "localhost:5000/example/index:v1", "localhost:5000/example/index/v1/catalog"},
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

// An existing cache directory without a result file still triggers rendering.
// 1. Create the cache directory without a result file.
// 2. Call RenderOPM.
// 3. Verify OPM was invoked once and its output was saved at the expected path.
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

// A missing OPM executable returns an error and leaves no output files.
// 1. Choose a path where no OPM executable exists.
// 2. Call RenderOPM with that executable path.
// 3. Verify an error, an empty result path, and no output files.
func TestRenderOPM_MissingBinary(t *testing.T) {
	newMockOPM(t) // Isolate retry settings from the developer's environment.
	cacheDir := t.TempDir()
	got, err := RenderOPM(t.Context(), testImage, filepath.Join(t.TempDir(), "missing-opm"), cacheDir)
	if err == nil || got != "" {
		t.Fatalf("missing binary = (%q, %v), want empty path and error", got, err)
	}
	assertFiles(t, cacheDir)
}

// A regular file used as the cache directory is preserved and rejected without invoking OPM.
// 1. Create a regular file at the cache directory path.
// 2. Call RenderOPM using that path as cacheDir.
// 3. Verify an error, no OPM calls, and unchanged file contents.
func TestRenderOPM_CachePathIsFile(t *testing.T) {
	opm := newMockOPM(t)
	cacheDir := filepath.Join(t.TempDir(), "not-a-directory")
	writeTestFile(t, cacheDir, "keep this file", testFileMode)
	got, err := RenderOPM(t.Context(), testImage, opm.path, cacheDir)
	if err == nil || got != "" {
		t.Fatalf("invalid cache path = (%q, %v), want empty path and error", got, err)
	}
	opm.assertCalls(t)
	assertContents(t, cacheDir, "keep this file")
}

// A directory at the expected cached result path is rejected and no output files are created.
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
	assertFiles(t, cacheDir)
}

// An already canceled context returns context.Canceled without invoking OPM or creating output files.
// 1. Create a context and cancel it before rendering.
// 2. Call RenderOPM with the canceled context.
// 3. Verify context.Canceled, an empty result path, no OPM calls, and no output files.
func TestRenderOPM_ContextAlreadyCanceled(t *testing.T) {
	opm := newMockOPM(t)
	cacheDir := t.TempDir()
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	got, err := RenderOPM(ctx, testImage, opm.path, cacheDir)
	if !errors.Is(err, context.Canceled) || got != "" {
		t.Fatalf("canceled render = (%q, %v), want empty path and context.Canceled", got, err)
	}
	opm.assertCalls(t)
	assertFiles(t, cacheDir)
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
	writeTestFile(t, opm.path, string(script), 0700)
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
		got = strings.Split(strings.TrimSuffix(string(data), "\n"), "\n")
	} else if !errors.Is(err, fs.ErrNotExist) {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("OPM targets = %q, want %q", got, want)
	}
}

func imageCatalogPath(cacheDir string) string {
	return filepath.Join(cacheDir, "quay.io", "example", "index", "v1", "catalog")
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
	if !reflect.DeepEqual(got, want) {
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
