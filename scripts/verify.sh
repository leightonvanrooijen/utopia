#!/bin/bash
set -e

# Ensure Go binaries are in PATH
export PATH="$PATH:$(go env GOPATH)/bin"

# Get Go files changed since last commit
CHANGED_FILES=$(git diff --name-only HEAD~1 -- '*.go' 2>/dev/null || true)

if [ -n "$CHANGED_FILES" ]; then
  # Get unique directories containing changed files, formatted as ./dir/path
  # Filter out directories that no longer exist (e.g., deleted in the commit)
  CHANGED_PACKAGES=$(echo "$CHANGED_FILES" | xargs -n1 dirname | sort -u | while read -r dir; do
    if [ -d "$dir" ]; then
      echo "./$dir"
    fi
  done | tr '\n' ' ')

  echo "==> Formatting changed files..."
  for file in $CHANGED_FILES; do
    if [ -f "$file" ]; then
      go fmt "$file" 2>/dev/null || true
    fi
  done

  # goimports if available
  if command -v goimports &> /dev/null; then
    for file in $CHANGED_FILES; do
      if [ -f "$file" ]; then
        goimports -w "$file" 2>/dev/null || true
      fi
    done
  fi

  # Only run staticcheck if there are packages to lint
  if [ -n "$CHANGED_PACKAGES" ]; then
    echo "==> Linting changed packages: $CHANGED_PACKAGES"
    # Run staticcheck on packages containing changed files
    LINT_OUTPUT=$(staticcheck $CHANGED_PACKAGES 2>&1) || {
      echo "$LINT_OUTPUT"
      echo ""
      echo "=========================================="
      echo "STATICCHECK FAILED"
      echo "=========================================="
      echo "To investigate, run: make health"
      echo "To see issues in a specific file: staticcheck ./path/to/package"
      echo "=========================================="
      exit 1
    }

    if [ -n "$LINT_OUTPUT" ]; then
      echo "$LINT_OUTPUT"
    fi
  else
    echo "==> No existing packages to lint (files may have been deleted)"
  fi
fi

echo "==> Running tests..."
go test ./...
