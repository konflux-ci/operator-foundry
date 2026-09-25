# OPM rendering contract

The tests define the agreed contract for `RenderOPM`. Its body is currently an
unimplemented stub for the tests-first review checkpoint.

`RenderOPM(ctx, target, opmPath, cacheDir)` returns a rendered file path or an
error. Targets are image references or existing local FBC files/directories.
Callers supply the OPM binary and a cache directory dedicated to that version.

- Image results use `<cacheDir>/<registry>/<repository>/[tag]/[digest]/catalog`.
  A completed regular file is a cache hit. Output is written to a unique file
  beside the final result and renamed only after successful rendering.
- Local inputs are rendered on every call into a unique temporary file within
  `cacheDir`. The caller removes successful local results after reading them.
- Failed or canceled renders remove their temporary output. Each retry starts
  with an empty output file.
- `RETRY_COUNT` is the number of retries after the first attempt; `0` disables
  retries. `RETRY_INTERVAL` is the delay in seconds, including fractional values.
  Unset or empty values default to 3 retries and 5 seconds. Invalid or negative
  settings return an error. Cancellation stops the command and retry wait.

## Intentional differences from Bash (for the implementation PR)

Source: `konflux-test/test/utils.sh`, `render_opm` and `retry`.

- Check the completed result file rather than just its directory.
- Publish only successful output; discard partial output between attempts.
- Always render local inputs again, with explicit caller ownership of successful
  temporary results. Bash applies its persistent cache logic to all targets.
- Return a file path and Go error instead of printing either contents or a path
  and exiting the shell on failure.
- Accept the OPM binary and cache location as arguments instead of finding `opm`
  on PATH and reading `OPM_RENDER_CACHE`.
- Use the existing Go image-reference parser, which validates references and
  requires an explicit registry, instead of Bash string splitting.
- Support context cancellation and explicitly validate retry settings.

Both retry environment variables, the default retry behavior, and local file
inputs remain supported. CLI and pipeline integration are separate tasks.
