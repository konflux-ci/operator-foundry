# operator-foundry
Go CLI for Konflux operator pipeline tasks

**Container image:** `quay.io/konflux-ci/operator-foundry`

---

## Usage

## `fbc` commands

`--dockerfile` is resolved as given (relative to the current working
directory, or absolute) if that path exists; otherwise it is resolved
relative to `--build-context`. This supports Dockerfiles in a subdirectory
of the build context, e.g. `--dockerfile catalog.Dockerfile --build-context
v5.0` resolves to `v5.0/catalog.Dockerfile`.

### `fbc check-lifecycle-eligibility`

Checks whether the File-Based Catalog (FBC) is eligible for lifecycle
injection, based on whether all OCP versions targeted by the Dockerfile are
>= the minimum supported version.

If the base image tag is not a fixed OCP version but references a build ARG
(e.g. `FROM registry.redhat.io/openshift4/ose-operator-registry-rhel9:${CATALOG_VERSION}`),
pass its value with `--build-arg` so it resolves to the same tag the image is
actually built with. If omitted, the ARG's own default value from the
Dockerfile is used instead.

```bash
operator-foundry fbc check-lifecycle-eligibility \
  --dockerfile <path-to-Dockerfile> \
  --build-context <path-to-build-context> \
  [--build-arg KEY=VALUE]... \
  [--output <path-to-output-file>]
```

| Scenario | Behavior |
|---|---|
| Dockerfile cannot be parsed | Exits with error |
| Base image tag references an ARG with no default and no matching `--build-arg` | Exits with error |
| All targeted OCP versions >= 5.0 | Writes `true`, exit 0 |
| Not all targeted OCP versions >= 5.0 | Writes `false`, exit 0 |

### `fbc check-related-images-mediatype`

Checks whether a list of related bundle images use OCI media types that are
compatible with the target OCP version. Images with an OCI media type are
incompatible with OCP versions earlier than v4.21.

The related images are provided as a JSON file containing a list of image
references.

```bash
operator-foundry fbc check-related-images-mediatype \
  --ocp-version <version> \
  --related-images-json-path <path-to-json> \
  [--output <path-to-output-file>]
```

| Scenario | Behavior |
|---|---|
| Related images JSON cannot be read or parsed | Exits with error |
| OCP version is malformed | Exits with error |
| OCP version >= 4.21 (OCI supported natively) | Writes `SUCCESS` result, exit 0 (check skipped) |
| All related images use compatible media types | Writes `SUCCESS` result, exit 0 |
| One or more images use incompatible media types or cannot be fetched | Writes `FAILURE` result listing the failed images, exit 0 |

### `fbc get-packages`

Determines the OLM packages included in a File-Based Catalog (FBC) by parsing
the `COPY`/`ADD` instructions in the provided Dockerfile and inspecting the
corresponding catalog subdirectories in the build context.

If a `COPY`/`ADD` source path references a build ARG (e.g. `COPY
./${INPUT_DIR}/ /configs/my-operator`), pass its value with `--build-arg` so
it resolves to the same path the image is actually built with. If omitted,
the ARG's own default value from the Dockerfile is used instead.

```bash
operator-foundry fbc get-packages \
  --dockerfile <path-to-Dockerfile> \
  --build-context <path-to-build-context> \
  [--build-arg KEY=VALUE]... \
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

If a COPY/ADD source path references a build ARG (e.g. COPY ./${INPUT_DIR}/ /configs/my-operator), pass its value with --build-arg so
the actual catalog directory on disk can be located. If omitted, the ARG's
own default value from the Dockerfile is used instead.

```bash
operator-foundry fbc inject-lifecycle \
  --dockerfile <path-to-Dockerfile> \
  --build-context <path-to-build-context> \
  --packages <comma-separated-package-names> \
  --lifecycle-dir <path-to-lifecycle-dir> \
  [--build-arg KEY=VALUE]...
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
./bin/operator-foundry fbc check-related-images-mediatype --help
./bin/operator-foundry fbc get-packages --help
./bin/operator-foundry fbc inject-lifecycle --help
```

---

## License

Apache License 2.0