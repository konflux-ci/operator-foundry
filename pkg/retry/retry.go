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

// Package retry provides context-aware retries for operations that can fail.
package retry

import (
	"context"
	"fmt"
	"time"
)

// Do calls operation once and retries failures up to retries times, waiting
// interval between attempts. Both settings must be non-negative.
// The operation returns shouldRetry and an error. A nil error means success;
// otherwise, false stops retries and true allows another attempt.
// Do returns the last operation error or the context error on cancellation.
// The operation must honor ctx to stop while running.
func Do(ctx context.Context, retries int, interval time.Duration, operation func(context.Context) (shouldRetry bool, err error)) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if retries < 0 {
		return fmt.Errorf("retries must be non-negative")
	}
	if interval < 0 {
		return fmt.Errorf("interval must be non-negative")
	}
	for attempt := 0; ; attempt++ {
		if err := ctx.Err(); err != nil {
			return err
		}
		shouldRetry, err := operation(ctx)
		if ctxErr := ctx.Err(); ctxErr != nil {
			return ctxErr
		}
		if err == nil {
			return nil
		}
		if !shouldRetry || attempt == retries {
			return err
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
