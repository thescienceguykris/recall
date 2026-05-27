#!/usr/bin/env bash
set -eu

repo_root="$(git rev-parse --show-toplevel)"
scan_root="$(mktemp -d)"

cleanup() {
	rm -rf "$scan_root"
}

trap cleanup EXIT INT TERM

cd "$repo_root"

cp .gitleaks.toml "$scan_root/.gitleaks.toml"

git ls-files -z | while IFS= read -r -d '' path; do
	target_dir="$scan_root/$(dirname "$path")"
	mkdir -p "$target_dir"
	cp "$path" "$scan_root/$path"
done

docker run --rm \
  -v "$scan_root:/repo" \
  -w /repo \
  zricethezav/gitleaks:latest \
  detect --source . --no-git --config .gitleaks.toml --redact
