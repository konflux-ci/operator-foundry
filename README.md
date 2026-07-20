# operator-foundry
Go CLI for Konflux operator pipeline tasks

**Container image:** `quay.io/konflux-ci/operator-foundry`

---

## Usage

## `fbc` commands

### `fbc check-lifecycle-eligibility`

Checks whether the File-Based Catalog (FBC) is eligible for lifecycle
injection, based on whether all OCP versions targeted by the Dockerfile are
>= the minimum supported version.

```bash
operator-foundry fbc check-lifecycle-eligibility \
  --dockerfile <path-to-Dockerfile> \
  --build-context <path-to-build-context> \
  [--output <path-to-output-file>]
```

| Scenario | Behavior |
|---|---|
| Dockerfile cannot be parsed | Exits with error |
| All targeted OCP versions >= 5.0 | Writes `true`, exit 0 |
| Not all targeted OCP versions >= 5.0 | Writes `false`, exit 0 |

### `fbc get-packages`

Determines the OLM packages included in a File-Based Catalog (FBC) by parsing
the `COPY`/`ADD` instructions in the provided Dockerfile and inspecting the
corresponding catalog subdirectories in the build context.

```bash
operator-foundry fbc get-packages \
  --dockerfile <path-to-Dockerfile> \
  --build-context <path-to-build-context> \
  [--output <path-to-output-file>]
```

| Scenario | Behavior |
|---|---|
| Dockerfile cannot be parsed | Exits with error |
| No `COPY`/`ADD` targeting `/configs` found | Exits with error |
| No packages found in catalog directories | Exits with error |

### `fbc inject-lifecycle`

Injects pre-generated `lifecycle.json` files into the catalog source directories
for the given OLM packages. Does not check lifecycle-injection eligibility —
callers should run `fbc check-lifecycle-eligibility` first.

```bash
operator-foundry fbc inject-lifecycle \
  --dockerfile <path-to-Dockerfile> \
  --build-context <path-to-build-context> \
  --packages <comma-separated-package-names> \
  --lifecycle-dir <path-to-lifecycle-dir>
```

| Scenario | Behavior |
|---|---|
| Dockerfile cannot be parsed | Exits with error |
| `lifecycle.json` missing for a package | Exits with error |
| lifecycle schema already exists at destination | Exits with error — refuses to overwrite |
| No matching catalog directory found for package | Exits with error |
| Invalid package name (path traversal, empty) | Exits with error |
| Destination path deeper than `/configs/<package-name>` | Exits with error — not a valid FBC path |

---

### `make-result-json`

Generates a Tekton `TEST_OUTPUT` JSON result for use in pipeline tasks.

```bash
operator-foundry make-result-json \
  --result <SUCCESS|FAILURE|ERROR|WARNING|SKIPPED> \
  [--note <note>] \
  [--namespace <namespace>] \
  [--successes <n>] \
  [--failures <n>] \
  [--warnings <n>]
```

| Scenario | Behavior |
|---|---|
| Invalid result value | Exits with error |
| `--result` not provided | Exits with error |

---

## Development

### Prerequisites

- Go 1.26.3+
- `golangci-lint` for linting

### Commands

```bash
make build   # build the binary to bin/operator-foundry
make test    # run all tests
make lint    # run linter
make clean   # remove build artifacts
```

### Verify

```bash
./bin/operator-foundry --help
./bin/operator-foundry fbc --help
./bin/operator-foundry fbc check-lifecycle-eligibility --help
./bin/operator-foundry fbc get-packages --help
./bin/operator-foundry fbc inject-lifecycle --help
```

---

## License

Apache License 2.0
