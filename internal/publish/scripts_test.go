package publish_test

import (
	"encoding/base64"
	"encoding/hex"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/iilei/canned-schedules-companion/internal/publish"
)

// The upload scripts teams copy (publish/upload-*.sh), run on a real output with stand-ins for curl and the AWS CLI.
// Uploads are recorded, one argument per line and a blank line after each call. Lookups answer "not there"; with
// HAVE=same every file is already there with its own SHA-256, with HAVE=other with another one.
const (
	curlStandIn = `#!/bin/sh
case "$*" in
*/api/storage/*)
	[ -n "${HAVE:-}" ] || { printf 404; exit 0; }
	url=$(for a in "$@"; do :; done; printf '%s' "$a")
	sum=$(sha256sum "${url#*/api/storage/schedules-local/catalog/}" | cut -d' ' -f1)
	[ "$HAVE" = same ] || sum=0000
	while [ "$1" != --output ]; do shift; done
	printf '{"checksums": {"sha256": "%s"}}' "$sum" > "$2"
	printf 200
	exit 0 ;;
esac
for a in "$@"; do printf '%s\n' "$a"; done >> "$CALLS"
echo >> "$CALLS"
`
	awsStandIn = `#!/bin/sh
if [ "$2" = head-object ]; then
	[ -n "${HAVE:-}" ] || { echo 'An error occurred (404) when calling the HeadObject operation: Not Found' >&2; exit 254; }
	while [ "$1" != --key ]; do shift; done
	[ "$HAVE" = same ] || { echo AAAA; exit 0; }
	sha256sum "${2#schedules/}" | cut -d' ' -f1 | xxd -r -p | base64
	exit 0
fi
for a in "$@"; do printf '%s\n' "$a"; done >> "$CALLS"
echo >> "$CALLS"
`
)

func runScript(t *testing.T, script string, env ...string) ([]string, string, error) {
	t.Helper()

	if runtime.GOOS == "windows" {
		t.Skip("the scripts are POSIX sh")
	}

	for _, tool := range []string{"sh", "jq", "xxd", "base64", "sha256sum"} {
		if _, err := exec.LookPath(tool); err != nil {
			t.Skipf("%s is not installed", tool)
		}
	}

	r, err := publish.Prepare(input(t, nil), []publish.File{{Path: nightPath, Src: []byte(night)}},
		options(t, "{slug}/{mr}", "schedule={name}\nmerged-by={merged-by}"))
	if err != nil {
		t.Fatal(err)
	}

	outDir := t.TempDir()
	for p, data := range r.Files {
		if err := os.MkdirAll(filepath.Dir(filepath.Join(outDir, p)), 0o750); err != nil {
			t.Fatal(err)
		}

		if err := os.WriteFile(filepath.Join(outDir, p), data, 0o600); err != nil {
			t.Fatal(err)
		}
	}

	bin := t.TempDir()
	for name, standIn := range map[string]string{"curl": curlStandIn, "aws": awsStandIn} {
		if err := os.WriteFile(filepath.Join(bin, name), []byte(standIn), 0o700); err != nil {
			t.Fatal(err)
		}
	}

	log := filepath.Join(t.TempDir(), "calls")
	cmd := exec.CommandContext(t.Context(), "sh", filepath.Join("..", "..", "publish", script), outDir)
	cmd.Env = append([]string{"PATH=" + bin + string(os.PathListSeparator) + os.Getenv("PATH"), "CALLS=" + log}, env...)

	out, err := cmd.CombinedOutput()
	if err != nil {
		return nil, string(out), err
	}

	raw, err := os.ReadFile(log)
	if errors.Is(err, os.ErrNotExist) {
		return nil, string(out), nil
	} else if err != nil {
		t.Fatal(err)
	}

	return strings.Split(strings.TrimSpace(string(raw)), "\n\n"), string(out), nil
}

func TestUploadArtifactory(t *testing.T) {
	t.Parallel()

	env := []string{
		"ARTIFACTORY_URL=https://art.example/artifactory/", "ARTIFACTORY_REPO=schedules-local",
		"ARTIFACTORY_PATH=catalog", "ARTIFACTORY_TOKEN=s3cret",
	}

	calls, out, err := runScript(t, "upload-artifactory.sh", env...)
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}

	// Five files, nothing else: an immutable repository gets no copies to overwrite later.
	if len(calls) != 5 {
		t.Fatalf("%d calls:\n%s", len(calls), strings.Join(calls, "\n--\n"))
	}

	for _, c := range calls {
		if strings.Contains(c, "s3cret") {
			t.Errorf("the token is on curl's command line:\n%s", c)
		}
	}

	digest := publish.Digest([]byte(night))
	md := calls[0]

	for _, want := range []string{
		"X-Checksum-Sha256: " + strings.TrimPrefix(digest, "sha256:"),
		"X-Checksum-Sha1: 16fb31ba8c44ffebc1c2d8ea8308bcb638f01e36",
		"X-Checksum-Md5: 9a6f7ebd01276068e8496d7a59e38444",
		"https://art.example/artifactory/schedules-local/catalog/night-shift/42/night-shift.canned-schedule.md" +
			";schedule=Night%20Shift;merged-by=J%C3%BCrgen",
	} {
		if !strings.Contains(md, want+"\n") && !strings.HasSuffix(md, want) {
			t.Errorf("missing %q in\n%s", want, md)
		}
	}

	alreadyThere(t, "upload-artifactory.sh", env...)
}

// alreadyThere runs a script where every file exists: with the same content it uploads nothing and passes (a rerun),
// with other content it fails and overwrites nothing.
func alreadyThere(t *testing.T, script string, env ...string) {
	t.Helper()

	if calls, out, err := runScript(t, script, append(env, "HAVE=same")...); err != nil || len(calls) != 0 ||
		strings.Count(out, "already there ") != 5 {
		t.Errorf("same content: %v, %d uploads\n%s", err, len(calls), out)
	}

	if calls, out, err := runScript(t, script, append(env, "HAVE=other")...); err == nil || len(calls) != 0 ||
		!strings.Contains(out, "already there with other content: not overwritten") {
		t.Errorf("other content: %v, %d uploads\n%s", err, len(calls), out)
	}
}

func TestUploadS3(t *testing.T) {
	t.Parallel()

	env := []string{"S3_BUCKET=catalog", "S3_PREFIX=schedules/"}

	calls, out, err := runScript(t, "upload-s3.sh", env...)
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}

	if len(calls) != 5 {
		t.Fatalf("%d calls:\n%s", len(calls), strings.Join(calls, "\n--\n"))
	}

	sum, err := hex.DecodeString(strings.TrimPrefix(publish.Digest([]byte(night)), "sha256:"))
	if err != nil {
		t.Fatal(err)
	}

	md := calls[0]
	for _, want := range []string{
		"s3api\nput-object", "--bucket\ncatalog", "--key\nschedules/night-shift/42/night-shift.canned-schedule.md",
		"--content-type\ntext/markdown; charset=utf-8", "--checksum-sha256\n" + base64.StdEncoding.EncodeToString(sum),
		`--metadata` + "\n" + `{"merged-by":"=?UTF-8?b?SsO8cmdlbg==?=","schedule":"Night Shift"}`,
	} {
		if !strings.Contains(md, want) {
			t.Errorf("missing %q in\n%s", want, md)
		}
	}

	alreadyThere(t, "upload-s3.sh", env...)
}
