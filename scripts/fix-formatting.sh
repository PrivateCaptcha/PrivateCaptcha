#!/usr/bin/env bash
set -euo pipefail

STAGED_GO_FILES=()
while IFS= read -r f; do
  STAGED_GO_FILES+=("$f")
done < <(git diff --cached --name-only --diff-filter=ACM -- '*.go' ':(exclude)vendor/*' || true)

if [ ${#STAGED_GO_FILES[@]} -eq 0 ]; then
  exit 0
fi

FORMATTER="gofmt"
if command -v goimports >/dev/null 2>&1; then
  FORMATTER="goimports"
fi

$FORMATTER -w "${STAGED_GO_FILES[@]}"
