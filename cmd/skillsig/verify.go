package main

import (
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/spf13/cobra"

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
			trust, _ := cmd.Flags().GetBool("trust")
			return runVerify(cmd.OutOrStdout(), verifyOpts{
				path:       path,
				ci:         ci,
				allowColor: !noColor,
				asJSON:     asJSON,
				sarifPath:  sarifPath,
				trust:      trust,
			})
		},
	}
	cmd.Flags().Bool("ci", false, "exit non-zero on UNSIGNED or SCOPE-DRIFTED rows")
	cmd.Flags().Bool("no-color", false, "disable color output (stable for diffing)")
	cmd.Flags().Bool("json", false, "emit a machine-readable JSON report instead of the table")
	cmd.Flags().String("sarif", "", "also write a SARIF 2.1.0 report to this path (\"-\" for stdout) for GitHub code-scanning")
	cmd.Flags().Bool("trust", false, "record every TRUSTED skill's current scope into the lock as the drift baseline (seed ~/.skillsig/lock.yaml)")
	return cmd
}

// verifyOpts bundles the verify command's flags so runVerify keeps one stable
// signature as the lock-aware (--trust) and SARIF modes were layered on. Tests
// construct it directly.
type verifyOpts struct {
	path       string
	ci         bool
	allowColor bool
	asJSON     bool
	sarifPath  string
	trust      bool
}

// runVerify is the testable core. It walks path through the LOCK-AWARE scanner
// (so a re-signed skill that broadened its grants vs. the recorded baseline is
// flagged SCOPE-DRIFTED, not just in-version drift), prints the report (table,
// or JSON when asJSON is set), optionally writes a SARIF report (when sarifPath
// is non-empty), and (optionally) returns ErrCIDrift.
//
// Two correctness fixes vs. the v0.3.0 behaviour:
//   - verify now goes through scope.DefaultScanner().Scan(path) instead of bare
//     scope.EvaluateAll, so the cross-version (lock) drift check the product
//     exists to catch actually runs in `verify --ci` and the SARIF annotations,
//     not only in `skillsig diff`. With opts.trust set, the corpus's TRUSTED
//     scopes are first recorded as the baseline (ScanAndTrust).
//   - when sarifPath is "-" (stdout), SARIF is the SOLE stdout artifact: the
//     human table / --json object is NOT also written to stdout, so the output
//     is a single valid SARIF document that github/codeql-action/upload-sarif
//     (or any JSON parser) can read.
func runVerify(out io.Writer, opts verifyOpts) error {
	info, err := os.Stat(opts.path)
	if err != nil {
		return fmt.Errorf("verify: %w", err)
	}
	if !info.IsDir() {
		return fmt.Errorf("verify: %s is not a directory", opts.path)
	}

	// When --sarif writes to stdout, that document must stand alone, so suppress
	// the human-readable table / --json from stdout (it would otherwise be
	// concatenated with the SARIF JSON and parse as neither).
	sarifToStdout := opts.sarifPath == "-"

	scanner := scope.DefaultScanner()
	var results []scope.Result
	if opts.trust {
		// Seed (or refresh) the lock baseline from the currently-TRUSTED corpus,
		// then report. ScanAndTrust persists ~/.skillsig/lock.yaml so the NEXT
		// plain verify catches a skill that quietly broadens scope.
		results, err = scanner.ScanAndTrust(opts.path)
	} else {
		results, err = scanner.Scan(opts.path)
	}
	if err != nil {
		return fmt.Errorf("verify: scan: %w", err)
	}

	if len(results) == 0 {
		if !sarifToStdout {
			if opts.asJSON {
				if err := report.RenderJSON(out, nil); err != nil {
					return err
				}
			} else {
				fmt.Fprintf(out, "no SKILL.md files found under %s\n", opts.path)
			}
		}
		// An empty tree still gets an empty-but-valid SARIF run when requested,
		// so a CI step that always uploads has a file to upload.
		if opts.sarifPath != "" {
			if err := writeSARIF(out, opts.sarifPath, nil); err != nil {
				return err
			}
		}
		return nil
	}

	if !sarifToStdout {
		if opts.asJSON {
			if err := report.RenderJSON(out, results); err != nil {
				return err
			}
		} else {
			useColor := opts.allowColor && isTTY(out)
			if err := report.Render(out, results, useColor); err != nil {
				return err
			}
			fmt.Fprintln(out, report.Summary(results))
		}
	}

	if opts.sarifPath != "" {
		if err := writeSARIF(out, opts.sarifPath, results); err != nil {
			return err
		}
	}

	if opts.ci {
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
