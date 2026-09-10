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
	"context"
	"crypto/sha256"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/go-containerregistry/pkg/v1/remote/transport"
)

// ── Existing tests ─────────────────────────────────────────────────────────────

func TestRegistryFetcher_InvalidImageReference_ReturnsError(t *testing.T) {
	fetcher := &RegistryFetcher{Timeout: 5 * time.Second}
	_, err := fetcher.FetchManifest(context.Background(), ":::invalid")
	if err == nil {
		t.Fatal("expected error for invalid image reference, got nil")
	}
	if !strings.Contains(err.Error(), "failed to parse image reference") {
		t.Errorf("expected parse error, got: %v", err)
	}
}

func TestRegistryFetcher_CancelledContext_ReturnsError(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	fetcher := &RegistryFetcher{Timeout: 5 * time.Second}
	_, err := fetcher.FetchManifest(ctx, "quay.io/example/img:v1")
	if err == nil {
		t.Fatal("expected error for cancelled context, got nil")
	}
}

func TestRegistryFetcher_ZeroTimeout_UsesDefault(t *testing.T) {
	fetcher := &RegistryFetcher{}
	_, err := fetcher.FetchManifest(context.Background(), ":::invalid")
	if err == nil {
		t.Fatal("expected error for invalid image reference, got nil")
	}
	if !strings.Contains(err.Error(), "failed to parse image reference") {
		t.Errorf("expected parse error (not timeout), got: %v", err)
	}
}

// ── isTransientError tests ─────────────────────────────────────────────────────

func TestIsTransientError(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		expected bool
	}{
		{
			name:     "429 Too Many Requests",
			err:      &transport.Error{StatusCode: http.StatusTooManyRequests},
			expected: true,
		},
		{
			name:     "503 Service Unavailable",
			err:      &transport.Error{StatusCode: http.StatusServiceUnavailable},
			expected: true,
		},
		{
			name:     "502 Bad Gateway",
			err:      &transport.Error{StatusCode: http.StatusBadGateway},
			expected: true,
		},
		{
			name:     "504 Gateway Timeout",
			err:      &transport.Error{StatusCode: http.StatusGatewayTimeout},
			expected: true,
		},
		{
			name:     "404 Not Found",
			err:      &transport.Error{StatusCode: http.StatusNotFound},
			expected: false,
		},
		{
			name:     "401 Unauthorized",
			err:      &transport.Error{StatusCode: http.StatusUnauthorized},
			expected: false,
		},
		{
			name:     "500 Internal Server Error",
			err:      &transport.Error{StatusCode: http.StatusInternalServerError},
			expected: false,
		},
		{
			name:     "non-transport error",
			err:      fmt.Errorf("connection refused"),
			expected: false,
		},
		{
			name:     "wrapped 429 transport error",
			err:      fmt.Errorf("fetch failed: %w", &transport.Error{StatusCode: http.StatusTooManyRequests}),
			expected: true,
		},
		{
			name:     "wrapped non-transient transport error",
			err:      fmt.Errorf("fetch failed: %w", &transport.Error{StatusCode: http.StatusForbidden}),
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isTransientError(tt.err)
			if got != tt.expected {
				t.Errorf("isTransientError() = %v, want %v", got, tt.expected)
			}
		})
	}
}

// ── FetchManifest retry tests ──────────────────────────────────────────────────

// fakeRegistryServer creates an httptest server that simulates an OCI registry.
// failCount controls how many manifest requests return the given failStatus before succeeding.
func fakeRegistryServer(t *testing.T, failCount int, failStatus int) (*httptest.Server, *atomic.Int32) {
	t.Helper()
	var manifestCalls atomic.Int32

	manifest := `{"schemaVersion": 2, "mediaType": "application/vnd.docker.distribution.manifest.v2+json", "config": {"mediaType": "application/vnd.docker.container.image.v1+json", "size": 2, "digest": "sha256:44136fa355b311bfa3a0d57ea7b4cbb4a5a0ae0e29a269c30a6c4e9b1e57c3c8"}, "layers": []}`
	digest := fmt.Sprintf("sha256:%x", sha256.Sum256([]byte(manifest)))

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/v2/":
			w.WriteHeader(http.StatusOK)
		case strings.Contains(r.URL.Path, "/manifests/"):
			count := int(manifestCalls.Add(1))
			if count <= failCount {
				w.WriteHeader(failStatus)
				return
			}
			w.Header().Set("Content-Type", "application/vnd.docker.distribution.manifest.v2+json")
			w.Header().Set("Docker-Content-Digest", digest)
			_, _ = w.Write([]byte(manifest))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))

	return server, &manifestCalls
}

func TestFetchManifest_RetriesOnTransientError_ThenSucceeds(t *testing.T) {
	server, calls := fakeRegistryServer(t, 1, http.StatusTooManyRequests)
	defer server.Close()

	addr := strings.TrimPrefix(server.URL, "http://")
	fetcher := &RegistryFetcher{Timeout: 30 * time.Second}
	desc, err := fetcher.FetchManifest(context.Background(), fmt.Sprintf("%s/test/repo:v1", addr))
	if err != nil {
		t.Fatalf("expected success after retry, got: %v", err)
	}
	if desc == nil {
		t.Fatal("expected descriptor, got nil")
	}
	if calls.Load() != 2 {
		t.Errorf("expected 2 manifest calls (1 fail + 1 success), got %d", calls.Load())
	}
}

func TestFetchManifest_TransientErrorExhaustsRetries(t *testing.T) {
	// failCount=1000 ensures the server never succeeds.
	// remote.Get has its own internal retries for 5xx, so the actual call count
	// will be higher than transientErrorRetries+1.
	server, _ := fakeRegistryServer(t, 1000, http.StatusServiceUnavailable)
	defer server.Close()

	addr := strings.TrimPrefix(server.URL, "http://")
	fetcher := &RegistryFetcher{Timeout: 20 * time.Second}
	_, err := fetcher.FetchManifest(context.Background(), fmt.Sprintf("%s/test/repo:v1", addr))
	if err == nil {
		t.Fatal("expected error after exhausting retries, got nil")
	}
}

func TestFetchManifest_NonTransientError_NoRetry(t *testing.T) {
	server, calls := fakeRegistryServer(t, 10, http.StatusNotFound)
	defer server.Close()

	addr := strings.TrimPrefix(server.URL, "http://")
	fetcher := &RegistryFetcher{Timeout: 30 * time.Second}
	_, err := fetcher.FetchManifest(context.Background(), fmt.Sprintf("%s/test/repo:v1", addr))
	if err == nil {
		t.Fatal("expected error for 404, got nil")
	}
	if calls.Load() != 1 {
		t.Errorf("expected 1 manifest call (no retry for 404), got %d", calls.Load())
	}
}

func TestFetchManifest_ContextCancelledDuringBackoff(t *testing.T) {
	server, _ := fakeRegistryServer(t, 10, http.StatusTooManyRequests)
	defer server.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()

	addr := strings.TrimPrefix(server.URL, "http://")
	fetcher := &RegistryFetcher{Timeout: 30 * time.Second}
	_, err := fetcher.FetchManifest(ctx, fmt.Sprintf("%s/test/repo:v1", addr))
	if err == nil {
		t.Fatal("expected error when context cancelled during backoff, got nil")
	}
}

func TestFetchManifest_UnsupportedMediaType_ReturnsDescriptor(t *testing.T) {
	manifest := `{"schemaVersion": 2, "mediaType": "application/vnd.unknown"}`
	digest := fmt.Sprintf("sha256:%x", sha256.Sum256([]byte(manifest)))

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/v2/":
			w.WriteHeader(http.StatusOK)
		case strings.Contains(r.URL.Path, "/manifests/"):
			w.Header().Set("Content-Type", "application/vnd.unknown")
			w.Header().Set("Docker-Content-Digest", digest)
			_, _ = w.Write([]byte(manifest))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	addr := strings.TrimPrefix(server.URL, "http://")
	fetcher := &RegistryFetcher{Timeout: 5 * time.Second}
	desc, err := fetcher.FetchManifest(context.Background(), fmt.Sprintf("%s/test/repo:v1", addr))
	if err != nil {
		t.Fatalf("expected success for unsupported media type (classification is the caller's job), got: %v", err)
	}
	if desc.MediaType != "application/vnd.unknown" {
		t.Errorf("got MediaType=%q, want application/vnd.unknown", desc.MediaType)
	}
}

func TestFetchManifest_ContextCancelledRaceWithTransientError(t *testing.T) {
	// Exercises the ctx.Err() guard (line 94): cancels the context inside the server
	// handler while returning a transient 502. Without the guard, isTransientError would
	// classify this as retryable and waste a retry iteration against a dead context.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/v2/":
			w.WriteHeader(http.StatusOK)
		case strings.Contains(r.URL.Path, "/manifests/"):
			cancel()
			w.WriteHeader(http.StatusBadGateway)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	addr := strings.TrimPrefix(server.URL, "http://")
	fetcher := &RegistryFetcher{Timeout: 30 * time.Second}
	_, err := fetcher.FetchManifest(ctx, fmt.Sprintf("%s/test/repo:v1", addr))
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if strings.Contains(err.Error(), "after transient error retries") {
		t.Error("expected context cancellation error, not retry exhaustion — ctx.Err() guard may be missing")
	}
}

func TestFetchManifest_502BadGateway_Retries(t *testing.T) {
	server, calls := fakeRegistryServer(t, 2, http.StatusBadGateway)
	defer server.Close()

	addr := strings.TrimPrefix(server.URL, "http://")
	fetcher := &RegistryFetcher{Timeout: 30 * time.Second}
	desc, err := fetcher.FetchManifest(context.Background(), fmt.Sprintf("%s/test/repo:v1", addr))
	if err != nil {
		t.Fatalf("expected success after retries, got: %v", err)
	}
	if desc == nil {
		t.Fatal("expected descriptor, got nil")
	}
	if calls.Load() != 3 {
		t.Errorf("expected 3 manifest calls (2 fail + 1 success), got %d", calls.Load())
	}
}
