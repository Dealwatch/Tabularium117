#!/usr/bin/env sh
# Run every check this repository knows how to run.
#
# This is the verification entry point: what the pre-push hook runs, what the
# release workflow runs, and what a contributor runs before pushing. One
# command, so there is no ambiguity about what "verified" means here.
set -eu

root=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
cd "$root"

if ! command -v go >/dev/null 2>&1; then
    echo "check.sh: go not found on PATH; it is required to run the checks." >&2
    exit 127
fi

# gofmt -l prints the files it would change; any output is a failure.
unformatted=$(gofmt -l cmd internal tools)
if [ -n "$unformatted" ]; then
    echo "check.sh: these files are not gofmt-formatted:" >&2
    echo "$unformatted" >&2
    exit 1
fi
go vet ./...
go test ./...

# The browser UI has no build step and therefore no runner of its own; these
# tests give its logic one (scripts/ui.test.mjs). Node is not a dependency of
# this project, so a machine without it is told rather than failed - CI has
# node and runs them for everyone.
if command -v node >/dev/null 2>&1; then
    node --test scripts/ui.test.mjs
else
    echo "check.sh: node not found; skipping the UI tests (scripts/ui.test.mjs)." >&2
fi

# The checks passing says nothing about whether they run on push. A clone that
# never enabled the hook has no protection at all, and silence here would let
# it believe otherwise.
if [ "$(git config core.hooksPath 2>/dev/null || true)" != ".githooks" ]; then
    echo ""
    echo "All checks passed -- but the pre-push hook is NOT enabled in this"
    echo "clone, so pushes run no checks. Enable it with:"
    echo "    git config core.hooksPath .githooks"
    exit 0
fi

echo "All checks passed."
