// Package report renders the 3-color verify table. Color is opt-in via the
// boolean passed to Render: when stdout isn't a TTY (or a test is asserting on
// the rendered output) we want stable, lipgloss-stripped text.
package report

import (
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
