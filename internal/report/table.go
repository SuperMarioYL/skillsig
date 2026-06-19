// Package report renders the 3-color verify table. Color is opt-in via the
// boolean passed to Render: when stdout isn't a TTY (or a test is asserting on
// the rendered output) we want stable, lipgloss-stripped text.
package report

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/SuperMarioYL/skillsig/internal/scope"
)

// Style holds the lipgloss styles for each verdict. They are package-level so
// tests can introspect them, and so they're computed once.
var (
	styleTrusted  = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("10")) // green
	styleUnsigned = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("11")) // yellow
	styleDrifted  = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("9"))  // red
	styleHeader   = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("12"))
	styleMuted    = lipgloss.NewStyle().Faint(true)
)

// Render writes a fixed-width 3-column table (Skill | Verdict | Details) to w.
// When color is false, all styling is stripped — that mode is what the CI
// output and the test assertions consume.
func Render(w io.Writer, results []scope.Result, color bool) error {
	rows := make([][3]string, 0, len(results)+1)
	rows = append(rows, [3]string{"SKILL", "VERDICT", "DETAILS"})
	for _, r := range results {
		rows = append(rows, [3]string{r.SkillID, string(r.Verdict), r.Details})
	}
	widths := [3]int{0, 0, 0}
	for _, row := range rows {
		for i, cell := range row {
			if n := lipgloss.Width(cell); n > widths[i] {
				widths[i] = n
			}
		}
	}

	var b strings.Builder
	writeRow := func(row [3]string, isHeader bool, verdict scope.Verdict) {
		for i, cell := range row {
			padded := padRight(cell, widths[i])
			styled := padded
			if color {
				switch {
				case isHeader:
					styled = styleHeader.Render(padded)
				case i == 1:
					styled = verdictStyle(verdict).Render(padded)
				case i == 2:
					styled = styleMuted.Render(padded)
				}
			}
			b.WriteString(styled)
			if i < len(row)-1 {
				b.WriteString("  ")
			}
		}
		b.WriteByte('\n')
	}

	writeRow(rows[0], true, "")
	// Underline header row with dashes; helps the eye even without color.
	underline := [3]string{
		strings.Repeat("-", widths[0]),
		strings.Repeat("-", widths[1]),
		strings.Repeat("-", widths[2]),
	}
	writeRow(underline, true, "")
	for i, row := range rows[1:] {
		writeRow(row, false, results[i].Verdict)
	}

	_, err := fmt.Fprint(w, b.String())
	return err
}

// jsonSkill is the per-skill shape emitted by RenderJSON. Field names are
// snake_case and stable so CI consumers can pin them; verdicts are the same
// uppercase strings the table prints (TRUSTED / UNSIGNED / SCOPE-DRIFTED).
type jsonSkill struct {
	SkillID string `json:"skill_id"`
	Dir     string `json:"dir"`
	Verdict string `json:"verdict"`
	Details string `json:"details"`
}

// jsonReport is the top-level object RenderJSON writes: the per-skill rows plus
// a tally and a single drift flag. drift is true when any row is UNSIGNED or
// SCOPE-DRIFTED — the same condition `verify --ci` exits non-zero on — so a CI
// step can branch on `.drift` without re-deriving it from the counts.
type jsonReport struct {
	Skills  []jsonSkill `json:"skills"`
	Summary jsonSummary `json:"summary"`
	Drift   bool        `json:"drift"`
}

type jsonSummary struct {
	Total      int `json:"total"`
	Trusted    int `json:"trusted"`
	Unsigned   int `json:"unsigned"`
	ScopeDrift int `json:"scope_drifted"`
}

// RenderJSON writes the results as a single indented JSON object to w. This is
// the machine-readable counterpart to Render, intended for `verify --json` so
// CI pipelines can parse verdicts instead of scraping the colored table.
func RenderJSON(w io.Writer, results []scope.Result) error {
	rep := jsonReport{Skills: make([]jsonSkill, 0, len(results))}
	for _, r := range results {
		rep.Skills = append(rep.Skills, jsonSkill{
			SkillID: r.SkillID,
			Dir:     r.Dir,
			Verdict: string(r.Verdict),
			Details: r.Details,
		})
		rep.Summary.Total++
		switch r.Verdict {
		case scope.VerdictTrusted:
			rep.Summary.Trusted++
		case scope.VerdictUnsigned:
			rep.Summary.Unsigned++
			rep.Drift = true
		case scope.VerdictScopeDrifted:
			rep.Summary.ScopeDrift++
			rep.Drift = true
		}
	}
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(rep)
}

// Summary is a one-line tally printed under the table. Useful for CI logs.
func Summary(results []scope.Result) string {
	var t, u, d int
	for _, r := range results {
		switch r.Verdict {
		case scope.VerdictTrusted:
			t++
		case scope.VerdictUnsigned:
			u++
		case scope.VerdictScopeDrifted:
			d++
		}
	}
	return fmt.Sprintf("%d skill(s): %d trusted, %d unsigned, %d scope-drifted",
		len(results), t, u, d)
}

func verdictStyle(v scope.Verdict) lipgloss.Style {
	switch v {
	case scope.VerdictTrusted:
		return styleTrusted
	case scope.VerdictUnsigned:
		return styleUnsigned
	case scope.VerdictScopeDrifted:
		return styleDrifted
	}
	return lipgloss.NewStyle()
}

func padRight(s string, width int) string {
	n := lipgloss.Width(s)
	if n >= width {
		return s
	}
	return s + strings.Repeat(" ", width-n)
}
