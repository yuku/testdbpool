# Git Hooks

This directory contains Git hooks for the testdbpool project.

## Setup

To use these hooks in your local repository, run:

```bash
git config core.hooksPath .githooks
```

This will configure Git to use the hooks in this directory instead of `.git/hooks`.

## Available Hooks

### pre-commit

The pre-commit hook runs the following checks before allowing a commit:

1. **go fmt** - Ensures all Go files are properly formatted
2. **golangci-lint** - Runs linting checks (if installed)

If any of these checks fail, the commit will be aborted.

## Installing golangci-lint

If you don't have golangci-lint installed, you can install it with:

```bash
curl -sSfL https://raw.githubusercontent.com/golangci/golangci-lint/master/install.sh | sh -s -- -b $(go env GOPATH)/bin
```

## Bypassing Hooks

If you need to bypass the pre-commit hook (not recommended), you can use:

```bash
git commit --no-verify
```

However, please ensure your code passes all checks before pushing to the repository.