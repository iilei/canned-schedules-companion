#!/bin/sh
# upload-artifactory.sh OUT_DIR: a post-fetch script for the publish step that uploads publish prepare's output to
# JFrog Artifactory. Every file goes to $ARTIFACTORY_URL/$ARTIFACTORY_REPO/[$ARTIFACTORY_PATH/]<path> with its three
# checksums from the manifest (Artifactory verifies each upload and flags files without SHA-1 and MD5) and the
# schedule's metadata as properties. With ARTIFACTORY_LATEST=true, each schedule's JSON and provenance also go to
# <slug>/latest/, a stable address for consumers.
#
# Environment:
#   ARTIFACTORY_URL       the instance's base, e.g. https://acme.jfrog.io/artifactory
#   ARTIFACTORY_REPO      the repository, e.g. schedules-local
#   ARTIFACTORY_TOKEN     an access token (sent as a bearer token), or else
#   ARTIFACTORY_USER and ARTIFACTORY_PASSWORD
#   ARTIFACTORY_PATH      optional: a folder inside the repository
#   ARTIFACTORY_LATEST    optional: true also writes <slug>/latest/ (default false)
# Needs curl and jq; where apk exists (the components' Alpine image), they are installed when missing.
set -eu

fail() {
  printf 'upload-artifactory: %s\n' "$1" >&2
  exit 2
}

[ $# -eq 1 ] || fail 'usage: upload-artifactory.sh OUT_DIR'
for tool in curl jq; do
  if ! command -v "$tool" >/dev/null; then
    command -v apk >/dev/null || fail "$tool is missing"
    apk add --no-cache --quiet "$tool"
  fi
done

: "${ARTIFACTORY_URL:?set ARTIFACTORY_URL}" "${ARTIFACTORY_REPO:?set ARTIFACTORY_REPO}"
base=${ARTIFACTORY_URL%/}/$ARTIFACTORY_REPO${ARTIFACTORY_PATH:+/${ARTIFACTORY_PATH%/}}

# The credentials go to curl as a header file, never on its command line, where other processes could read them.
auth=$(mktemp)
trap 'rm -f "$auth"' EXIT INT TERM
if [ -n "${ARTIFACTORY_TOKEN:-}" ]; then
  printf 'Authorization: Bearer %s\n' "$ARTIFACTORY_TOKEN" > "$auth"
elif [ -n "${ARTIFACTORY_USER:-}" ] && [ -n "${ARTIFACTORY_PASSWORD:-}" ]; then
  printf 'Authorization: Basic %s\n' "$(printf '%s:%s' "$ARTIFACTORY_USER" "$ARTIFACTORY_PASSWORD" | base64 | tr -d '\n')" > "$auth"
else
  fail 'set ARTIFACTORY_TOKEN, or ARTIFACTORY_USER and ARTIFACTORY_PASSWORD'
fi

cd "$1"
[ "$(jq -r .version manifest.json)" = 1 ] || fail 'manifest.json is not version 1'

# One row per upload: the file, its digest and checksums, the properties, and where it goes.
jq -r --arg latest "${ARTIFACTORY_LATEST:-false}" '.schedules[] | .slug as $s | .artifactory as $p | .files[]
    | ([.path, .digest, .checksums.sha1, .checksums.md5, $p, .path],
       (select($latest == "true" and (.path | test("\\.canned-schedule\\.(json|provenance\\.json)$")))
        | [.path, .digest, .checksums.sha1, .checksums.md5, $p, ($s + "/latest/" + (.path | split("/") | last))]))
    | @tsv' manifest.json |
  while IFS="$(printf '\t')" read -r path digest sha1 md5 props target; do
    curl --fail --silent --show-error --output /dev/null --header @"$auth" \
      --header "X-Checksum-Sha256: ${digest#sha256:}" --header "X-Checksum-Sha1: $sha1" \
      --header "X-Checksum-Md5: $md5" --upload-file "$path" "$base/$target$props"
    printf 'uploaded %s\n' "$target"
  done
