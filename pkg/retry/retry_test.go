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

package retry

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"
)

// Do stops on success and allows at most retries additional calls after the first.
func TestDoAttempts(t *testing.T) {
	failure := errors.New("operation failed")
	for _, tc := range []struct {
		name                         string
		retries, failures, wantCalls int
		wantErr                      bool
	}{
		{"immediate success", 3, 0, 1, false},
		{"eventual success", 3, 2, 3, false},
		{"no retries", 0, 1, 1, true},
		{"exhausted", 2, 4, 3, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			err := Do(t.Context(), tc.retries, 0, func(ctx context.Context) (bool, error) {
				if ctx != t.Context() {
					t.Fatal("operation received a different context")
				}
				calls++
				if calls <= tc.failures {
					return true, failure
				}
				return false, nil
			})
			if calls != tc.wantCalls || (tc.wantErr && err != failure) || (!tc.wantErr && err != nil) {
				t.Fatalf("Do = %v, calls = %d; want error=%v, calls=%d", err, calls, tc.wantErr, tc.wantCalls)
			}
		})
	}
}

// An operation returning false and an error stops retries immediately.
func TestDoNonRetryableError(t *testing.T) {
	failure := errors.New("cannot continue")
	calls := 0
	err := Do(t.Context(), 3, 0, func(context.Context) (bool, error) {
		calls++
		return false, fmt.Errorf("prepare: %w", failure)
	})
	if calls != 1 || !errors.Is(err, failure) || err.Error() != "prepare: cannot continue" {
		t.Fatalf("Do = %v, calls = %d", err, calls)
	}
}

// Negative retry counts or intervals are rejected before the operation runs.
func TestDoInvalidSettings(t *testing.T) {
	for _, tc := range []struct {
		retries  int
		interval time.Duration
	}{{-1, 0}, {0, -time.Second}} {
		err := Do(t.Context(), tc.retries, tc.interval, func(context.Context) (bool, error) {
			t.Fatal("operation called with invalid settings")
			return false, nil
		})
		if err == nil {
			t.Fatal("expected invalid settings error")
		}
	}
}

// An already canceled context prevents even the initial attempt.
func TestDoCanceledBeforeAttempt(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	err := Do(ctx, 3, 0, func(context.Context) (bool, error) {
		t.Fatal("operation called after cancellation")
		return false, nil
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Do = %v, want context.Canceled", err)
	}
}

// Cancellation during an attempt takes precedence over its result and stops retries.
func TestDoCanceledDuringOperation(t *testing.T) {
	for _, result := range []error{nil, errors.New("command stopped")} {
		ctx, cancel := context.WithCancel(t.Context())
		calls := 0
		err := Do(ctx, 3, 0, func(context.Context) (bool, error) {
			calls++
			cancel()
			return true, result
		})
		cancel()
		if calls != 1 || !errors.Is(err, context.Canceled) {
			t.Fatalf("Do = %v, calls = %d", err, calls)
		}
	}
}

// Do waits between failed attempts, but cancellation interrupts that wait.
// The hour-long interval ensures completion depends on cancellation, not timer expiry.
func TestDoIntervalAndCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	called := make(chan struct{}, 2)
	done := make(chan error, 1)
	go func() {
		done <- Do(ctx, 3, time.Hour, func(context.Context) (bool, error) {
			called <- struct{}{}
			return true, errors.New("try again")
		})
	}()
	select {
	case <-called:
	case <-time.After(2 * time.Second):
		t.Fatal("initial attempt did not start")
	}
	select {
	case <-called:
		t.Fatal("retry did not wait for interval")
	case <-time.After(20 * time.Millisecond):
	}
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("Do = %v, want context.Canceled", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("cancellation did not interrupt retry wait")
	}
}
