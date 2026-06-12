# operator-foundry
Go CLI for Konflux operator pipeline tasks

**Container image:** `quay.io/konflux-ci/operator-foundry`

---

## Usage

### `fbc` commands

#### `fbc inject-lifecycle`

Injects pre-generated `lifecycle.json` files into the catalog source directories
for the given OLM packages. Injection is skipped if not all targeted OCP versions
are >= 5.0.

```bash
operator-foundry fbc inject-lifecycle \
  --dockerfile  \
  --build-context  \
  --packages  \
  --lifecycle-dir 
```

| Scenario | Behavior |
|---|---|
| Dockerfile cannot be parsed | Exits with error |
| Not all OCP versions >= 5.0 | Skips injection silently, exit 0 |
| `lifecycle.json` missing for a package | Exits with error |
| `lifecycle.json` already exists at destination | Exits with error — refuses to overwrite |
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
./bin/operator-foundry fbc inject-lifecycle --help
```

---

## License

Apache License 2.0