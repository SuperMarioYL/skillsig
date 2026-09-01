package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	skillsig "github.com/SuperMarioYL/skillsig"
)

// version reports the skillsig release version. It is initialized from the
// VERSION file embedded by the module-root skillsig package, so EVERY build
// path reports the same version: a plain
// `go install github.com/SuperMarioYL/skillsig/cmd/skillsig@latest` (no
// -ldflags) embeds the VERSION file at the tagged commit and reports the real
// release version, not the stale "0.1.0-dev" sentinel it used to ship. The
// goreleaser `-X main.version={{.Version}}` ldflags override
// (.goreleaser.yaml) still wins at link time when present, so the
// release-artifact path is unchanged.
var version = skillsig.Version

func main() {
	if err := newRootCmd().Execute(); err != nil {
		fmt.Fprintln(os.Stderr, "skillsig:", err)
		os.Exit(1)
	}
}

func newRootCmd() *cobra.Command {
	root := &cobra.Command{
		Use:           "skillsig",
		Short:         "Signed manifest + scope-drift detector for Claude Code skills",
		Long:          rootLongHelp,
		SilenceUsage:  true,
		SilenceErrors: true,
		Version:       version,
	}

	root.AddCommand(
		newVerifyCmd(),
		newSignCmd(),
		newInitCmd(),
		newDiffCmd(),
	)
	return root
}

const rootLongHelp = `skillsig verifies the declared permission scope of installed agent skills
(Claude Code and format-compatible hosts) against a Sigstore-signed manifest,
and flags scope drift between versions of the same skill.

The verifier is local-only; signing uses Sigstore keyless OIDC.`

// The four subcommands below are scaffolded as stubs in this stage; later
// build stages flesh out their flags and behavior. Returning a clear "not
// implemented" message keeps `skillsig --help` and `go build` honest while
// the rest of the scaffold lands.

// newVerifyCmd lives in verify.go (m1 implementation).
// newSignCmd lives in sign.go (m2 implementation).
// newInitCmd lives in init.go.
// newDiffCmd lives in diff.go (m3 implementation).
