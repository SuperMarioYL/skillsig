package main

import (
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/spf13/cobra"

	"github.com/SuperMarioYL/skillsig/internal/manifest"
	"github.com/SuperMarioYL/skillsig/internal/report"
	"github.com/SuperMarioYL/skillsig/internal/scope"
)

// ErrCIDrift is returned (and surfaces as exit-1) when --ci is set and any row
// of the verify table is UNSIGNED or SCOPE-DRIFTED. Exposed as a value so
// scripts can grep for it in stderr if they ever want to.
var ErrCIDrift = errors.New("skill scope drift detected (--ci)")

func newVerifyCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "verify [path]",
		Short: "Walk a skills directory and print a TRUSTED / UNSIGNED / SCOPE-DRIFTED table",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			path := "."
			if len(args) == 1 {
				path = args[0]
			}
			ci, _ := cmd.Flags().GetBool("ci")
			noColor, _ := cmd.Flags().GetBool("no-color")
			asJSON, _ := cmd.Flags().GetBool("json")
			sarifPath, _ := cmd.Flags().GetString("sarif")
			return runVerify(cmd.OutOrStdout(), path, ci, !noColor, asJSON, sarifPath)
		},
	}
	cmd.Flags().Bool("ci", false, "exit non-zero on UNSIGNED or SCOPE-DRIFTED rows")
	cmd.Flags().Bool("no-color", false, "disable color output (stable for diffing)")
	cmd.Flags().Bool("json", false, "emit a machine-readable JSON report instead of the table")
	cmd.Flags().String("sarif", "", "also write a SARIF 2.1.0 report to this path (\"-\" for stdout) for GitHub code-scanning")
	return cmd
}

// runVerify is the testable core. It walks path, evaluates each skill, prints
// the report (table, or JSON when asJSON is set), optionally writes a SARIF
// report (when sarifPath is non-empty), and (optionally) returns ErrCIDrift.
func runVerify(out io.Writer, path string, ci, allowColor, asJSON bool, sarifPath string) error {
	info, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("verify: %w", err)
	}
	if !info.IsDir() {
		return fmt.Errorf("verify: %s is not a directory", path)
	}

	dirs, err := manifest.FindSkillDirs(path)
	if err != nil {
		return fmt.Errorf("verify: scan: %w", err)
	}
	if len(dirs) == 0 {
		if asJSON {
			if err := report.RenderJSON(out, nil); err != nil {
				return err
			}
		} else {
			fmt.Fprintf(out, "no SKILL.md files found under %s\n", path)
		}
		// An empty tree still gets an empty-but-valid SARIF run when requested,
		// so a CI step that always uploads has a file to upload.
		if sarifPath != "" {
			if err := writeSARIF(out, sarifPath, nil); err != nil {
				return err
			}
		}
		return nil
	}

	skills := make([]*manifest.Skill, 0, len(dirs))
	for _, d := range dirs {
		s, err := manifest.ParseSkill(d)
		if err != nil {
			return fmt.Errorf("verify: %w", err)
		}
		skills = append(skills, s)
	}

	results := scope.EvaluateAll(skills)
	if asJSON {
		if err := report.RenderJSON(out, results); err != nil {
			return err
		}
	} else {
		useColor := allowColor && isTTY(out)
		if err := report.Render(out, results, useColor); err != nil {
			return err
		}
		fmt.Fprintln(out, report.Summary(results))
	}

	if sarifPath != "" {
		if err := writeSARIF(out, sarifPath, results); err != nil {
			return err
		}
	}

	if ci {
		for _, r := range results {
			if r.Verdict == scope.VerdictUnsigned || r.Verdict == scope.VerdictScopeDrifted {
				return ErrCIDrift
			}
		}
	}
	return nil
}

// writeSARIF emits the SARIF 2.1.0 report to sarifPath, or to out when sarifPath
// is "-". A file target is created/truncated; its directory is assumed to exist.
func writeSARIF(out io.Writer, sarifPath string, results []scope.Result) error {
	if sarifPath == "-" {
		return report.RenderSARIF(out, results)
	}
	f, err := os.Create(sarifPath)
	if err != nil {
		return fmt.Errorf("verify: sarif: %w", err)
	}
	defer f.Close()
	if err := report.RenderSARIF(f, results); err != nil {
		return fmt.Errorf("verify: sarif: %w", err)
	}
	return nil
}

// isTTY checks whether w is a terminal so we don't paint ANSI escapes into a
// pipe or test buffer. Stdlib-only — inspects the file mode for a character
// device, which catches the common case (terminal stdout) without needing
// golang.org/x/term.
func isTTY(w io.Writer) bool {
	if _, set := os.LookupEnv("NO_COLOR"); set {
		return false
	}
	f, ok := w.(*os.File)
	if !ok {
		return false
	}
	info, err := f.Stat()
	if err != nil {
		return false
	}
	return info.Mode()&os.ModeCharDevice != 0
}
