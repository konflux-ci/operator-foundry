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
	"errors"
	"fmt"
	"log/slog"
	"math/rand"
	"net/http"
	"time"

	"github.com/google/go-containerregistry/pkg/authn"
	"github.com/google/go-containerregistry/pkg/name"
	"github.com/google/go-containerregistry/pkg/v1/remote"
	"github.com/google/go-containerregistry/pkg/v1/remote/transport"
)

const defaultTimeout = 120 * time.Second
const transientErrorRetries = 2

// ManifestFetcher abstracts registry access so the check logic can be tested with a mock
type ManifestFetcher interface {
	FetchManifest(ctx context.Context, imageRef string) (*remote.Descriptor, error)
}

// RegistryFetcher implements ManifestFetcher
type RegistryFetcher struct {
	Timeout time.Duration
}

// FetchManifest resolves an image reference and returns its manifest descriptor.
// Retries up to transientErrorRetries times with exponential backoff on transient HTTP errors (429, 502, 503, 504).
func (f *RegistryFetcher) FetchManifest(ctx context.Context, imageRef string) (*remote.Descriptor, error) {
	timeout := f.Timeout
	// Protection against setting the timeout to 0
	if timeout <= 0 {
		timeout = defaultTimeout
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	ref, err := name.ParseReference(imageRef)
	if err != nil {
		return nil, fmt.Errorf("failed to parse image reference %q: %w", imageRef, err)
	}

	var desc *remote.Descriptor
	// retry on transient HTTP errors (429, 502, 503, 504) with exponential backoff and jitter
	for attempt := range transientErrorRetries + 1 {
		// skip the wait on the first pass
		if attempt > 0 {

			// ~1s, ~2s delays + jitter to avoid retry waves
			delay := time.Duration(float64(int(1)<<(attempt-1)) * float64(time.Second) * (0.5 + rand.Float64()))
			slog.Warn("retrying transient registry error",
				"imageRef", imageRef,
				"attempt", attempt,
				"maxAttempts", transientErrorRetries,
				"delay", delay,
				"error", err,
			)
			timer := time.NewTimer(delay)
			select {
			case <-ctx.Done():
				timer.Stop()
				return nil, context.Cause(ctx)
			case <-timer.C:
			}
		}
		desc, err = f.fetchManifest(ctx, ref)
		if err == nil {
			return desc, nil
		}

		// A cancelled context can race with the HTTP response: the registry may return a 502/503
		// before the connection tears down, which isTransientError would classify as retryable.
		// Check the context first to avoid retrying on a context that is already done.
		if ctx.Err() != nil {
			return nil, context.Cause(ctx)
		}
		if !isTransientError(err) {
			return nil, fmt.Errorf("failed to fetch image manifest for %q: %w", imageRef, err)
		}
	}

	return nil, fmt.Errorf("failed to fetch image manifest for %q after transient error retries: %w", imageRef, err)
}

// fetchManifest performs a single registry fetch for the given parsed reference.
func (f *RegistryFetcher) fetchManifest(ctx context.Context, imageRef name.Reference) (*remote.Descriptor, error) {
	return remote.Get(imageRef, remote.WithContext(ctx), remote.WithAuthFromKeychain(authn.DefaultKeychain))
}

// isTransientError checks whether the error is a transient HTTP error from the container registry.
func isTransientError(err error) bool {
	var transportErr *transport.Error
	if errors.As(err, &transportErr) {
		switch transportErr.StatusCode {
		case http.StatusTooManyRequests,
			http.StatusServiceUnavailable,
			http.StatusBadGateway,
			http.StatusGatewayTimeout:
			return true
		}
	}
	return false
}
