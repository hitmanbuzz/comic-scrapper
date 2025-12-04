#!/bin/bash
# Pre-commit hook for comicrawl
# Automatically formats code and runs linters before commit

set -e

echo "Running pre-commit checks..."

# Get list of staged Go files
STAGED_GO_FILES=$(git diff --cached --name-only --diff-filter=ACM | grep '\.go$' || true)

if [ -z "$STAGED_GO_FILES" ]; then
    echo "No Go files staged, skipping checks"
    exit 0
fi

echo "Formatting staged files with goimports..."
for file in $STAGED_GO_FILES; do
    # Format the file
    goimports -w "$file"
    # Re-add the file to staging after formatting
    git add "$file"
done

echo "Running golangci-lint on staged files..."
if [ -n "$STAGED_GO_FILES" ]; then
    golangci-lint run --fix $(echo "$STAGED_GO_FILES" | tr '\n' ' ')
    
    # Re-add any auto-fixed files
    for file in $STAGED_GO_FILES; do
        git add "$file"
    done
fi

echo "Pre-commit checks passed"
exit 0
