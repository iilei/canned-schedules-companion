#!/bin/sh
# upload-s3.sh OUT_DIR: a post-fetch script for the publish step that uploads publish prepare's output to Amazon S3
# (or a compatible store). Every file goes to s3://$S3_BUCKET/[$S3_PREFIX/]<path> with its media type, the manifest's
# SHA-256 as the checksum S3 verifies, and the schedule's metadata as x-amz-meta-* (left out, with a warning, when the
# manifest has none because it exceeds S3's 2 KB). With S3_LATEST=true, each schedule's JSON and provenance also go
# to <slug>/latest/, a stable address for consumers.
#
# Environment:
#   S3_BUCKET    the bucket
#   S3_PREFIX    optional: a prefix inside the bucket
#   S3_LATEST    optional: true also writes <slug>/latest/ (default false)
#   and the AWS CLI's own configuration (credentials, region, AWS_ENDPOINT_URL for a compatible store).
# Needs the AWS CLI, jq and xxd; where apk exists, jq is installed when missing (Alpine's busybox has xxd).
set -eu

fail() {
  printf 'upload-s3: %s\n' "$1" >&2
  exit 2
}

[ $# -eq 1 ] || fail 'usage: upload-s3.sh OUT_DIR'
command -v aws >/dev/null || fail 'the AWS CLI is missing'
command -v xxd >/dev/null || fail 'xxd is missing'
if ! command -v jq >/dev/null; then
  command -v apk >/dev/null || fail 'jq is missing'
  apk add --no-cache --quiet jq
fi

: "${S3_BUCKET:?set S3_BUCKET}"
prefix=${S3_PREFIX:+${S3_PREFIX%/}/}

cd "$1"
[ "$(jq -r .version manifest.json)" = 1 ] || fail 'manifest.json is not version 1'

# One row per upload: the file, its digest, media type, metadata as base64 JSON (or empty), and where it goes.
jq -r --arg latest "${S3_LATEST:-false}" '.schedules[] | .slug as $s
    | (if .s3 == null then "" else (.s3 | tojson | @base64) end) as $m | .files[]
    | ([.path, .digest, ."media-type", $m, .path],
       (select($latest == "true" and (.path | test("\\.canned-schedule\\.(json|provenance\\.json)$")))
        | [.path, .digest, ."media-type", $m, ($s + "/latest/" + (.path | split("/") | last))]))
    | @tsv' manifest.json |
  while IFS="$(printf '\t')" read -r path digest type meta target; do
    set -- --bucket "$S3_BUCKET" --key "$prefix$target" --body "$path" --content-type "$type" \
      --checksum-sha256 "$(printf '%s' "${digest#sha256:}" | xxd -r -p | base64)"
    if [ -n "$meta" ]; then
      set -- "$@" --metadata "$(printf '%s' "$meta" | base64 -d)"
    else
      printf 'upload-s3: %s goes without metadata: it exceeds S3'"'"'s 2 KB\n' "$path" >&2
    fi
    aws s3api put-object "$@" --output text --query ETag > /dev/null
    printf 'uploaded %s\n' "$target"
  done
