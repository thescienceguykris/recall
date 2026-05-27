#!/usr/bin/env bash
set -eu

repo_root="$(git rev-parse --show-toplevel)"

cd "$repo_root"

chmod +x .githooks/pre-commit scripts/run-gitleaks.sh
git config core.hooksPath .githooks

printf 'Configured git hooks to use %s/.githooks\n' "$repo_root"
