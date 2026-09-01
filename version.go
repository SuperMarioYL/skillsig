// Package skillsig is the module root package. It exposes the canonical
// release version embedded from the VERSION file so EVERY build path reports
// the same version — plain `go install ...@latest`, `go build`, and the
// goreleaser `-X main.version=...` ldflags path all agree.
//
// Before this existed, cmd/skillsig/main.go shipped
// `var version = "0.1.0-dev"` and only the goreleaser ldflags overrode it; the
// README's documented `go install github.com/SuperMarioYL/skillsig/cmd/skillsig@latest`
// path (which passes no ldflags) produced a binary reporting "0.1.0-dev" for
// every release. Embedding the VERSION file makes it the single source of
// truth: a `go install ...@v0.10.0` build embeds the VERSION file from that
// tag and reports "0.10.0", with no manual in-source literal to drift.
package skillsig

import (
	_ "embed"
	"strings"
)

//go:embed VERSION
var rawVersion string

// Version is the canonical skillsig release version, read from the embedded
// VERSION file (whitespace-trimmed). The goreleaser ldflags override
// (`-X main.version={{.Version}}` in .goreleaser.yaml) still wins at link time
// when present, so the release-artifact path is unchanged; without it this
// embedded value stands, so `go install ...@latest` no longer mis-reports the
// stale "0.1.0-dev" sentinel.
var Version = strings.TrimSpace(rawVersion)
