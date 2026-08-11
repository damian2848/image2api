#!/usr/bin/env bash
set -euo pipefail

if [[ ${EUID:-$(id -u)} -ne 0 ]]; then
  echo "Run this installer with sudo." >&2
  exit 1
fi

repo_dir="${1:-}"
if [[ -z "$repo_dir" || ! -d "$repo_dir/.git" ]]; then
  echo "Usage: sudo $0 /absolute/path/to/image2api" >&2
  exit 1
fi
repo_dir="$(cd "$repo_dir" && pwd)"

install -d -m 0750 /etc/image2api
if [[ ! -f /etc/image2api/updater.env ]]; then
  install -m 0600 "$repo_dir/ops/updater/updater.env.example" /etc/image2api/updater.env
  sed -i.bak "s|^IMAGE2API_REPO=.*|IMAGE2API_REPO=$repo_dir|" /etc/image2api/updater.env
  rm -f /etc/image2api/updater.env.bak
  echo "Fill UPDATER_TOKEN in /etc/image2api/updater.env before starting the service." >&2
fi

if command -v go >/dev/null 2>&1; then
  (cd "$repo_dir/ops/updater" && go build -trimpath -ldflags='-s -w' -o /usr/local/bin/image2api-updater .)
elif command -v docker >/dev/null 2>&1; then
  docker run --rm \
    -v "$repo_dir:/src:ro" \
    -v /usr/local/bin:/out \
    golang:1.26-bookworm \
    sh -c "cd /src/ops/updater && go build -trimpath -ldflags='-s -w' -o /out/image2api-updater ."
else
  echo "Go or Docker is required to build the updater binary." >&2
  exit 1
fi

install -m 0644 "$repo_dir/ops/updater/image2api-updater.service" /etc/systemd/system/image2api-updater.service
systemctl daemon-reload
systemctl enable image2api-updater.service

echo "Updater installed. After configuring /etc/image2api/updater.env, run:"
echo "  sudo systemctl restart image2api-updater"
