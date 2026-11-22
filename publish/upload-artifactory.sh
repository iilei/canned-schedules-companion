#!/bin/sh
# upload-artifactory.sh OUT_DIR: a post-fetch script for the publish step that uploads publish prepare's output to
# JFrog Artifactory. Every file goes to $ARTIFACTORY_URL/$ARTIFACTORY_REPO/[$ARTIFACTORY_PATH/]<path> with its three
# checksums from the manifest (Artifactory verifies each upload and flags files without SHA-1 and MD5) and the
# schedule's metadata as properties.
#
# It never overwrites, so it suits repositories that deny it (immutable artifacts): a file already there with the same
# SHA-256 is skipped, so a rerun of the job passes, and one with another SHA-256 fails the upload. Consumers find the
# newest publish of a schedule by its properties (schedule, merged-at), e.g. with an AQL query.
#
# Environment:
#   ARTIFACTORY_URL       the instance's base, e.g. https://acme.jfrog.io/artifactory
#   ARTIFACTORY_REPO      the repository, e.g. schedules-local
#   ARTIFACTORY_TOKEN     an access token (sent as a bearer token), or else
#   ARTIFACTORY_USER and ARTIFACTORY_PASSWORD
#   ARTIFACTORY_PATH      optional: a folder inside the repository
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
folder=$ARTIFACTORY_REPO${ARTIFACTORY_PATH:+/${ARTIFACTORY_PATH%/}}
base=${ARTIFACTORY_URL%/}/$folder
storage=${ARTIFACTORY_URL%/}/api/storage/$folder

# The credentials go to curl as a header file, never on its command line, where other processes could read them.
auth=$(mktemp)
info=$(mktemp)
trap 'rm -f "$auth" "$info"' EXIT INT TERM
if [ -n "${ARTIFACTORY_TOKEN:-}" ]; then
  printf 'Authorization: Bearer %s\n' "$ARTIFACTORY_TOKEN" > "$auth"
elif [ -n "${ARTIFACTORY_USER:-}" ] && [ -n "${ARTIFACTORY_PASSWORD:-}" ]; then
  printf 'Authorization: Basic %s\n' "$(printf '%s:%s' "$ARTIFACTORY_USER" "$ARTIFACTORY_PASSWORD" | base64 | tr -d '\n')" > "$auth"
else
  fail 'set ARTIFACTORY_TOKEN, or ARTIFACTORY_USER and ARTIFACTORY_PASSWORD'
fi

cd "$1"
[ "$(jq -r .version manifest.json)" = 1 ] || fail 'manifest.json is not version 1'

# One row per file: its path, digest and checksums, and the properties.
jq -r '.schedules[] | .artifactory as $p | .files[] | [.path, .digest, .checksums.sha1, .checksums.md5, $p] | @tsv' \
  manifest.json |
  while IFS="$(printf '\t')" read -r path digest sha1 md5 props; do
    sha256=${digest#sha256:}
    code=$(curl --silent --show-error --output "$info" --write-out '%{http_code}' --header @"$auth" "$storage/$path")
    case $code in
      200)
        [ "$(jq -r '.checksums.sha256 // empty' "$info")" = "$sha256" ] ||
          fail "$path is already there with other content: not overwritten"
        printf 'already there %s\n' "$path"
        continue
        ;;
      404) ;;
      *) fail "looking up $path: HTTP $code" ;;
    esac
    curl --fail --silent --show-error --output /dev/null --header @"$auth" \
      --header "X-Checksum-Sha256: $sha256" --header "X-Checksum-Sha1: $sha1" \
      --header "X-Checksum-Md5: $md5" --upload-file "$path" "$base/$path$props"
    printf 'uploaded %s\n' "$path"
  done
