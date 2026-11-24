# Agent Conventions

This file documents Go conventions for automated review agents. Follow
these rules instead of inferring conventions from adjacent files.

## Test function doc comments

Both styles below are intentional and correct. Do not flag either style
or recommend converting one to the other.

- **Single-line prose** summarizing what the test verifies:
  `// Only olm.package names are returned, in input order, from a mixed catalog.`
- **Prose summary followed by numbered steps** for multi-phase tests:
  ```go
  // Package-scoped extraction validates malformed matching bundles.
  // 1. Supply a valid bundle followed by a malformed bundle.
  // 2. Extract related images.
  // 3. Verify no results are returned and the error is reported.
  ```

Choose whichever style fits the test. Simple assertion tests use
single-line prose. Multi-step integration tests may use numbered steps
to clarify the sequence when the prose summary alone is not enough.

## Exported function and type comments

Use standard Go doc comments: start with the identifier name, describe
behavior and contracts in complete sentences. Multi-paragraph comments
are fine for complex APIs. This follows the Go project's own convention
(<https://go.dev/doc/comment>).

## Test context

Both `t.Context()` and `context.Background()` appear in existing tests.
Prefer `t.Context()` in new code (available since Go 1.24), but do not
flag existing uses of `context.Background()` as errors.

## Error handling

Use `errors.Is` and `errors.As` for sentinel and typed error checks.
Wrap errors with `fmt.Errorf("context: %w", err)` to preserve the
chain.

## Linting

golangci-lint v2 with `default: standard` linters (see `.golangci.yml`).
Do not suggest enabling additional linters beyond this set.
