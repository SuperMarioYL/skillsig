package main

import (
	"errors"
	"fmt"
	"io"

	"github.com/spf13/cobra"

	"github.com/SuperMarioYL/skillsig/internal/scope"
)

// ErrScopeEscalation is returned (and surfaces as exit-1) when `diff` finds the
// new version broadened its declared scope vs. the old version. Exposed as a
// value so CI scripts and tests can match it. This is the m3 acceptance gate: a
// re-signed skill that quietly added a grant FAILS diff even with a valid
// signature, because diff compares declared scope, not signatures.
var ErrScopeEscalation = errors.New("scope escalation between skill versions")

func newDiffCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "diff <old-dir> <new-dir>",
		Short: "Flag scope escalations (added tools, broader fs writes, new egress) between two skill versions",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runDiff(cmd.OutOrStdout(), args[0], args[1])
		},
	}
	return cmd
}

// runDiff is the testable core. It loads the declared scope from each dir,
// prints any escalations in a readable form, and returns ErrScopeEscalation
// (non-zero exit) when at least one escalation is found.
func runDiff(out io.Writer, oldDir, newDir string) error {
	escalations, err := scope.DiffSkills(oldDir, newDir)
	if err != nil {
		return fmt.Errorf("diff: %w", err)
	}
	if len(escalations) == 0 {
		fmt.Fprintf(out, "no scope escalation: %s is within the declared scope of %s\n", newDir, oldDir)
		return nil
	}
	fmt.Fprintf(out, "scope escalation detected (%s → %s):\n", oldDir, newDir)
	for _, e := range escalations {
		fmt.Fprintf(out, "  - %s\n", e)
	}
	return ErrScopeEscalation
}
