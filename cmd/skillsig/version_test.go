package main

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	skillsig "github.com/SuperMarioYL/skillsig"
)

// TestVersion_ReportsEmbeddedNotDev is the regression for the version-drift
// defect: a binary built via the README's documented
// `go install github.com/SuperMarioYL/skillsig/cmd/skillsig@latest` path
// (plain `go build`/`go install`, NO -ldflags) used to report
// "skillsig version 0.1.0-dev" because cmd/skillsig/main.go shipped
// `var version = "0.1.0-dev"` and only the goreleaser ldflags path overrode
// it. Now main.version is initialized from the embedded VERSION file, so
// EVERY build path reports the same version. This test builds the binary
// WITHOUT ldflags (exactly what `go install ...@latest` does) and asserts
// --version reports the embedded release version, not the stale 0.1.0-dev
// sentinel.
func TestVersion_ReportsEmbeddedNotDev(t *testing.T) {
	bin := filepath.Join(t.TempDir(), "skillsig-test")
	if runtime.GOOS == "windows" {
		bin += ".exe"
	}
	// No -ldflags here — this is the `go install ...@latest` build path, the
	// one the README and web/site.json point users at.
	build := exec.Command("go", "build", "-o", bin, ".")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("go build (no ldflags): %v\n%s", err, out)
	}
	out, err := exec.Command(bin, "--version").Output()
	if err != nil {
		t.Fatalf("run --version: %v", err)
	}
	got := strings.TrimSpace(string(out))
	want := "skillsig version " + skillsig.Version
	if got != want {
		t.Fatalf("--version (plain build, no ldflags): got %q, want %q — a "+
			"`go install ...@latest` build must report the embedded VERSION, "+
			"not the stale 0.1.0-dev sentinel", got, want)
	}
	if strings.Contains(got, "0.1.0-dev") {
		t.Fatalf("--version still reports the stale dev sentinel: %q", got)
	}
}

// TestVersion_LockstepWithVersionFile asserts every in-source version surface
// equals the canonical VERSION file so a future bump cannot silently drift
// (the defect this fix closes: v0.9.0's main.version was "0.1.0-dev" while
// VERSION was "0.9.0", and web/site.json content_version was "v0.9.0"). Every
// surface that carries the release version must agree with the VERSION file,
// which is the single source of truth.
func TestVersion_LockstepWithVersionFile(t *testing.T) {
	want := readVersionFile(t)

	if version != want {
		t.Fatalf("main.version %q != VERSION file %q (the in-source version "+
			"surface drifted from the canonical VERSION file)", version, want)
	}
	if skillsig.Version != want {
		t.Fatalf("embedded skillsig.Version %q != VERSION file %q",
			skillsig.Version, want)
	}
	if rc := newRootCmd(); rc.Version != want {
		t.Fatalf("root command Version %q != VERSION file %q", rc.Version, want)
	}

	// web/site.json content_version is the site surface; it carries a leading
	// "v" by convention, so strip it before comparing to the bare VERSION.
	siteVer := readSiteContentVersion(t)
	if strings.TrimPrefix(siteVer, "v") != want {
		t.Fatalf("web/site.json content_version %q != VERSION file %q "+
			"(the site version surface drifted)", siteVer, want)
	}

	// CHANGELOG.md must carry a real section for the released version — a bump
	// with no changelog entry is the same drift class in prose form.
	cl, err := os.ReadFile("../../CHANGELOG.md")
	if err != nil {
		t.Fatalf("read ../../CHANGELOG.md: %v", err)
	}
	if !strings.Contains(string(cl), "## ["+want+"]") {
		t.Fatalf("CHANGELOG.md has no \"## [%s]\" section (the version was "+
			"bumped without a changelog entry)", want)
	}
}

// TestDemoResults_MatchRenderedOutput pins docs/demo-results.json as a lockstep
// surface. web/site.json names it as meta.demo_source and quotes its recorded
// output verbatim on the published site, so the recorded output must equal what
// the current binary prints for the bundled fixtures. A report-rendering change
// that is not re-captured here (e.g. v0.12.0's malformed tally, which appended
// ", 0 malformed" to the summary line) would otherwise silently publish output
// the shipped binary no longer produces.
//
// The recorded run used a SKILLSIG_HOME pointing at a path that does not exist
// ("an empty isolated baseline path"), so no cross-version drift applies;
// hermeticLock reproduces that with an empty temp home. The test drives
// runVerify — the same path the recorded command takes — so signature verdicts
// are folded in exactly as the CLI does.
func TestDemoResults_MatchRenderedOutput(t *testing.T) {
	raw, err := os.ReadFile("../../docs/demo-results.json")
	if err != nil {
		t.Fatalf("read ../../docs/demo-results.json: %v", err)
	}
	var recorded struct {
		Steps []struct {
			Command string `json:"command"`
			Output  string `json:"output"`
		} `json:"steps"`
	}
	if err := json.Unmarshal(raw, &recorded); err != nil {
		t.Fatalf("parse docs/demo-results.json: %v", err)
	}
	if len(recorded.Steps) == 0 {
		t.Fatal("docs/demo-results.json has no steps")
	}

	hermeticLock(t)
	var b bytes.Buffer
	if err := runVerify(&b, verifyOpts{path: filepath.Join("..", "..", "testdata", "skills")}); err != nil {
		t.Fatalf("verify fixtures: %v", err)
	}
	// The recorded output is the terminal text with its trailing newline
	// stripped; runVerify ends with fmt.Fprintln(summary).
	got := strings.TrimRight(b.String(), "\n")
	if want := recorded.Steps[0].Output; got != want {
		t.Errorf("docs/demo-results.json is stale — its recorded output does "+
			"not match what the current binary prints for the bundled fixtures.\n"+
			"Re-run the recorded command and update the file:\n  %s\n"+
			"--- recorded ---\n%s\n--- current ---\n%s",
			recorded.Steps[0].Command, want, got)
	}
}

// readVersionFile reads the canonical VERSION file at the module root. The
// test runs from cmd/skillsig, so the file is two directories up.
func readVersionFile(t *testing.T) string {
	t.Helper()
	b, err := os.ReadFile("../../VERSION")
	if err != nil {
		t.Fatalf("read ../../VERSION: %v", err)
	}
	return strings.TrimSpace(string(b))
}

// readSiteContentVersion reads the content_version field from web/site.json
// at the module root. The test runs from cmd/skillsig, so the file is two
// directories up.
func readSiteContentVersion(t *testing.T) string {
	t.Helper()
	b, err := os.ReadFile("../../web/site.json")
	if err != nil {
		t.Fatalf("read ../../web/site.json: %v", err)
	}
	var s struct {
		ContentVersion string `json:"content_version"`
	}
	if err := json.Unmarshal(b, &s); err != nil {
		t.Fatalf("parse web/site.json: %v", err)
	}
	if s.ContentVersion == "" {
		t.Fatal("web/site.json has no content_version field")
	}
	return s.ContentVersion
}
