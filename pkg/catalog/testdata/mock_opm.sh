#!/bin/sh
# Test-only stand-in for "opm render"; it never invokes the real OPM binary.
# RenderOPM tests run this script instead to simulate successful output, partial
# output followed by failure, a failure before a successful retry, or a running
# process that must be canceled. It prints a prepared payload and returns the
# requested exit status so tests can check how RenderOPM handles each outcome.
# Calls are recorded, and all state lives in a per-test temporary directory.
set -eu

if [ "$#" -ne 2 ] || [ "$1" != render ]; then
    echo "expected: render <target>" >&2
    exit 64
fi

state=${TEST_OPM_STATE:?}
printf '%s\n' "$2" >> "$state/calls"
mode=$(cat "$state/mode")

case "$mode" in
    fail)
        printf 'partial output that must never survive a failed render or retry'
        echo "mock render failed" >&2
        exit 1
        ;;
    fail-once)
        if [ ! -f "$state/failed" ]; then
            : > "$state/failed"
            printf 'partial output that must never survive a failed render or retry'
            exit 1
        fi
        ;;
    block)
        printf 'partial output'
        : > "$state/started"
        # Replace the shell so cancellation has no child process to orphan.
        exec sleep 30
        ;;
    success)
        ;;
    *)
        echo "unknown mock mode: $mode" >&2
        exit 64
        ;;
esac

cat "$state/payload"
