# operator-foundry
Go CLI for Konflux operator pipeline tasks

**Container image:** `quay.io/konflux-ci/operator-foundry`

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
```

---

## License

Apache License 2.0