#!/bin/sh
# Builds the documentation site into dist/docs (mise run docs:build): Sphinx, Furo and MyST from the hash-pinned
# docs/requirements.txt (the same as canned-schedules') in a cached virtualenv, with warnings as errors.
set -eu

venv=${XDG_CACHE_HOME:-$HOME/.cache}/canned-schedules-companion/docs-venv
want=$(sha256sum docs/requirements.txt | cut -d' ' -f1)
if [ "$(cat "$venv/requirements.sha256" 2>/dev/null)" != "$want" ]; then
  rm -rf "$venv"
  python3 -m venv "$venv"
  "$venv/bin/pip" install --quiet --disable-pip-version-check --require-hashes -r docs/requirements.txt
  printf '%s\n' "$want" > "$venv/requirements.sha256"
fi

rm -rf dist/docs
"$venv/bin/sphinx-build" -W --keep-going -q -b html docs dist/docs
echo "built dist/docs ($(du -sh dist/docs | cut -f1))"
