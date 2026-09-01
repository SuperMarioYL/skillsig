package main

import (
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
