# AGENTS.md

## Repository Overview & Commands

- `operator-foundry` is the Konflux CLI for operator pipeline tasks. `cmd/operator-foundry/main.go`
  delegates to Cobra in `internal/cli/`. Reusable catalog, lifecycle, image, OCP version, media type,
  retry, and Tekton result logic lives in `pkg/`.
- `make build` writes `bin/operator-foundry`. `make test` runs `go test ./... -v -race -count=1`.
  For a focused run, use `go test -race -count=1 ./pkg/catalog -run TestRenderOPM`.
- `make lint` runs `golangci-lint run ./...` with the standard set in `.golangci.yml`. `go.mod`
  specifies the minimum Go version and supplies the linter's Go version. For CI parity, use the Go
  and golangci-lint versions pinned in `.github/workflows/ci.yaml`. CI also runs govulncheck and Hadolint.

## Go Conventions & Idioms

- Keep flag parsing, command defaults, and output formatting in `internal/cli/`. Reuse `parseBuildArgs`
  in `internal/cli/fbc/` for `--build-arg`. Update `README.md` when command flags or behavior change.
- Put diagnostics in structured `slog` messages with key/value attributes. `internal/cli/root.go`
  controls the log level and sends logs to stderr. Preserve result output, including empty output and
  trailing newlines, for both stdout and `--output` where supported. Use `testoutput.MakeResultJSON`
  for Tekton `TEST_OUTPUT`. Use counts in result notes and put per-image details in task logs.
- Add operation context first and wrap the underlying error last, e.g.
  `fmt.Errorf("failed to render catalog: %w", err)`. Return an already contextual error directly when
  the caller has nothing to add. Keep existing `Err…` sentinels in `pkg/catalog/errors.go`,
  `pkg/image/errors.go`, and `pkg/retry/retry.go` discoverable by `errors.Is`. Image inspection can
  wrap both a sentinel and the underlying error. One-off validation errors can be plain errors.
- Reuse `readCatalog` and `catalogRecord.nonEmptyString` in `pkg/catalog/catalog_reader.go`.
  Rendered catalogs are streams of JSON objects. The reader adds a one-based record number and
  leaves closing to its caller. Check required field presence before decoding and report
  `"<field> must be present"` for a missing field.
- Share validation across paths that promise the same behavior. `pkg/catalog/catalog_differential.go`
  uses the same extractor on both sides, scoped to packages declared by the fragment. Apply each
  query's schema and package filters first, then decode only the fields it needs. Catalog extractors
  discard partial results on error. Use `bundleDeprecated` for the `olm.deprecated` bundle property.
- Preserve first-seen order for unique package names and bundle images, sorted unique related
  images, and each API's documented empty result. Slice-returning catalog extractors return
  non-nil empty slices for successful queries with no matches.
- Keep coupled collections in sync at one registration point. See `ensurePackage` in
  `pkg/catalog/catalog_extractors.go`, which initializes every per-package entry together. Preallocate
  mapping and filtering slices with `make([]T, 0, capacity)` when the size or upper bound is known.
  Preserve a documented nil result, such as when `lifecycle.FilterPackages` filters everything.
- Reuse `resolveDockerfilePath`, `resolveAndValidatePath`, and `ParseCopyInstructionsForConfigs` for
  lifecycle paths and Dockerfile inputs. Preserve resolving `--dockerfile` as given before falling
  back to the build context. Reuse `pkg/ocp` for version checks and `image.ParseImageURL` when
  reference parsing must preserve an absent tag or distinguish tag and digest.
- Pass `cmd.Context()` into cancellable package operations and propagate it to registry requests,
  subprocesses, and retry waits. OPM rendering uses the fixed-interval `retry.Do`. Registry fetches
  in `pkg/mediatype/media_type_fetcher.go` have their own transient HTTP backoff.
- Preserve the cache identity in `pkg/catalog/render_opm.go` across OPM binary hashes and image
  reference variants. Clear partial output before a retry and publish only completed renders.
- For concurrent registry checks, follow `pkg/mediatype/check_related_images_mediatype.go`.
  Bound concurrency with `errgroup.SetLimit`, guard shared results with a mutex, give each pass a
  fresh group derived from the caller's context, and sort results after workers finish.

## Testing Conventions

- Use same-package tests and the standard `testing` package. Both standalone `TestX_Scenario`
  cases and table-driven `t.Run` subtests are established. See `pkg/catalog/catalog_extractors_test.go`
  and `pkg/catalog/render_opm_test.go`. Reuse `t.Helper()` assertions where they clarify the test.
- Check sentinel and wrapped error identity with `errors.Is`. Check error text when it is the
  contract, such as a missing catalog field and record number. Use standard comparisons such as
  `slices.Equal` for ordered slices. Check nil explicitly when the contract distinguishes nil and empty.
- Prefer `t.Context()` for new tests, as in `pkg/catalog/render_opm_test.go` and `pkg/retry/retry_test.go`.
  Derive a cancelable context or timeout when exercising cancellation. Existing `context.Background()`
  tests in `pkg/image/` and `pkg/mediatype/` are valid.
- Use `t.TempDir()` for files, fixtures in `pkg/catalog/testdata/` for representative streams, and
  inline data for small edge cases. Fake the OPM runner or registry interface for logic tests. Use
  a loopback `httptest` registry for HTTP behavior, as in `pkg/image/inspect_test.go`.
- Test the caller-visible edge that changes. For catalog comparisons, pair malformed in-scope and
  out-of-scope bundles on the fragment and index where applicable. For retry or cache changes, check
  cancellation, attempt counts, and whether a failed attempt leaves a published result.

## Documentation & Comment Style

- Document new exported APIs with comments that start with the identifier. Describe ordering,
  deduplication, ownership, and error or empty-result behavior when callers depend on them. Comment
  internal helpers when their contract or rationale needs explaining. End the final sentence of code
  comments with a period.
- Both prose and numbered test comments are valid. See `pkg/catalog/render_opm_test.go` and
  `pkg/catalog/catalog_differential_test.go`. Describe the scenario and expected behavior the test
  actually asserts. Skip steps already obvious from the setup and assertions.

## AI Code Review & PR Guidelines

- Tie findings to changed behavior and give a concrete input, call path, or test that demonstrates
  the impact. Reserve blocking findings for material defects. Leave formatting to tooling and treat
  minor wording or style nits as nonblocking. Accept the sibling patterns documented above.
- Read the nearest callers, tests, and package helpers before suggesting a change or introducing a
  helper. Check ordering, nil/empty output, and error contracts before proposing unification. Use
  the documented workspace assumptions in `resolveAndValidatePath` and `RenderOPM` for path reviews.
- Lead PR descriptions with what changes and why it matters to the pipeline. Give concrete inputs,
  outputs, and scope. For stacked PRs, identify the layer under review and its boundaries, then
  remove obsolete stack notes after the lower PR merges.
- Reply to review threads briefly. State exactly what changed, or give the decision and reason
  when leaving the code as-is.
