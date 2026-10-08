# Catalog extractor fixtures

These are streams of indented JSON objects, not arrays. Go tests use fixed
expected values and require no Bash, jq, OPM, network, or adjacent checkout.

- `mixed.json` exercises package order, bundle image order/repeats, filtering,
  and sorted unique related images. Each package and bundle record has a distinct
  name; repeating an image reference is intentional. The two bundles sharing
  related-image name `operator` are also checked as a fragment of this fixture
  so other names cannot hide erroneous deduplication by name.
- `deprecations.json` distinguishes an `olm.deprecations` record from the
  `olm.deprecated` bundle property. Tests place the record before and after bundles.
- `no-related-images.json` is a complete catalog with a package, channel and one
  active bundle that omits the optional related-image list.
- `bash-happy-path.json` preserves the original six Bats examples' data, including
  their simplified metadata and placeholder references. Only whitespace changed;
  this legacy fixture is not presented as a fully valid FBC catalog.

Validate each fixture in a separate directory: they represent independent catalogs. `bash-happy-path.json` preserves the original Bats data and is not expected to pass `opm validate`.
