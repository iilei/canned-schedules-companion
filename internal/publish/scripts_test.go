package publish_test

import (
	"encoding/base64"
	"encoding/hex"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/iilei/canned-schedules-companion/internal/publish"
)

// The upload scripts teams copy (publish/upload-*.sh), run on a real output with stand-ins for curl and the AWS CLI
// that record their arguments, one per line, a blank line after each call.
const recorder = "#!/bin/sh\nfor a in \"$@\"; do printf '%s\\n' \"$a\"; done >> \"$CALLS\"\necho >> \"$CALLS\"\n"

func runScript(t *testing.T, script string, env ...string) []string {
	t.Helper()

	if runtime.GOOS == "windows" {
		t.Skip("the scripts are POSIX sh")
	}

	for _, tool := range []string{"sh", "jq", "xxd", "base64"} {
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
	for _, name := range []string{"curl", "aws"} {
		if err := os.WriteFile(
			filepath.Join(bin, name),
			[]byte(recorder),
			0o700,
		); err != nil {
			t.Fatal(err)
		}
	}

	log := filepath.Join(t.TempDir(), "calls")
	cmd := exec.CommandContext(t.Context(), "sh", filepath.Join("..", "..", "publish", script), outDir)
	cmd.Env = append([]string{"PATH=" + bin + string(os.PathListSeparator) + os.Getenv("PATH"), "CALLS=" + log}, env...)

	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("%s: %v\n%s", script, err, out)
	}

	raw, err := os.ReadFile(log)
	if err != nil {
		t.Fatal(err)
	}

	return strings.Split(strings.TrimSpace(string(raw)), "\n\n")
}

func TestUploadArtifactory(t *testing.T) {
	t.Parallel()

	calls := runScript(t, "upload-artifactory.sh", "ARTIFACTORY_URL=https://art.example/artifactory/",
		"ARTIFACTORY_REPO=schedules-local", "ARTIFACTORY_PATH=catalog", "ARTIFACTORY_TOKEN=s3cret",
		"ARTIFACTORY_LATEST=true")

	// Five files, plus the JSON and the provenance under latest/.
	if len(calls) != 7 {
		t.Fatalf("%d calls:\n%s", len(calls), strings.Join(calls, "\n--\n"))
	}

	json := ""

	for _, c := range calls {
		if strings.Contains(c, "s3cret") {
			t.Errorf("the token is on curl's command line:\n%s", c)
		}

		if strings.Contains(c, "/catalog/night-shift/latest/night-shift.canned-schedule.json;") {
			json = c
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

	if json == "" {
		t.Errorf("no latest/ upload of the JSON in\n%s", strings.Join(calls, "\n--\n"))
	}
}

func TestUploadS3(t *testing.T) {
	t.Parallel()

	calls := runScript(t, "upload-s3.sh", "S3_BUCKET=catalog", "S3_PREFIX=schedules/")

	// No latest/ unless asked for.
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
}
