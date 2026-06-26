package report

import (
	"encoding/json"
	"io"

	"github.com/SuperMarioYL/skillsig/internal/scope"
)

// SARIF 2.1.0 emission for `skillsig verify --ci --sarif`. The point is the CI
// merge-gate wedge: a GitHub Actions `github/codeql-action/upload-sarif` step
// turns each SCOPE-DRIFTED / UNSIGNED skill into an inline annotation on the
// pull request that introduced the drift, instead of a buried log line.
//
// Only the subset of the SARIF 2.1.0 schema that GitHub code-scanning requires
// is emitted (version, $schema, one run, a tool driver with rules, and results
// with a ruleId / level / message / physical location). TRUSTED skills produce
// no result — a clean PR shows zero annotations.

const (
	sarifVersion = "2.1.0"
	sarifSchema  = "https://raw.githubusercontent.com/oasis-tcs/sarif-spec/master/Schemata/sarif-schema-2.1.0.json"
	sarifToolURI = "https://github.com/SuperMarioYL/skillsig"
)

type sarifLog struct {
	Schema  string     `json:"$schema"`
	Version string     `json:"version"`
	Runs    []sarifRun `json:"runs"`
}

type sarifRun struct {
	Tool    sarifTool     `json:"tool"`
	Results []sarifResult `json:"results"`
}

type sarifTool struct {
	Driver sarifDriver `json:"driver"`
}

type sarifDriver struct {
	Name           string      `json:"name"`
	InformationURI string      `json:"informationUri"`
	Rules          []sarifRule `json:"rules"`
}

type sarifRule struct {
	ID               string         `json:"id"`
	Name             string         `json:"name"`
	ShortDescription sarifText      `json:"shortDescription"`
	Properties       sarifRuleProps `json:"properties"`
}

type sarifRuleProps struct {
	Tags []string `json:"tags"`
}

type sarifResult struct {
	RuleID    string          `json:"ruleId"`
	Level     string          `json:"level"`
	Message   sarifText       `json:"message"`
	Locations []sarifLocation `json:"locations"`
}

type sarifText struct {
	Text string `json:"text"`
}

type sarifLocation struct {
	PhysicalLocation sarifPhysicalLocation `json:"physicalLocation"`
}

type sarifPhysicalLocation struct {
	ArtifactLocation sarifArtifactLocation `json:"artifactLocation"`
}

type sarifArtifactLocation struct {
	URI string `json:"uri"`
}

// the two rules skillsig can flag; SCOPE-DRIFTED is an error (it should block a
// merge), UNSIGNED is a warning (a brand-new skill is not yet attested).
const (
	ruleScopeDrift = "skillsig/scope-drifted"
	ruleUnsigned   = "skillsig/unsigned"
)

// RenderSARIF writes a SARIF 2.1.0 log to w. One result is emitted per
// SCOPE-DRIFTED (level=error) or UNSIGNED (level=warning) skill; TRUSTED skills
// produce no result. The rules array is always the full set so GitHub renders a
// stable rule metadata block even on a clean run.
func RenderSARIF(w io.Writer, results []scope.Result) error {
	run := sarifRun{
		Tool: sarifTool{Driver: sarifDriver{
			Name:           "skillsig",
			InformationURI: sarifToolURI,
			Rules: []sarifRule{
				{
					ID:               ruleScopeDrift,
					Name:             "ScopeDrifted",
					ShortDescription: sarifText{Text: "A signed skill escalated its declared scope since it was last verified."},
					Properties:       sarifRuleProps{Tags: []string{"security", "supply-chain"}},
				},
				{
					ID:               ruleUnsigned,
					Name:             "Unsigned",
					ShortDescription: sarifText{Text: "A skill has no skillsig attestation."},
					Properties:       sarifRuleProps{Tags: []string{"security", "supply-chain"}},
				},
			},
		}},
		Results: make([]sarifResult, 0, len(results)),
	}

	for _, r := range results {
		var ruleID, level string
		switch r.Verdict {
		case scope.VerdictScopeDrifted:
			ruleID, level = ruleScopeDrift, "error"
		case scope.VerdictUnsigned:
			ruleID, level = ruleUnsigned, "warning"
		default:
			continue // TRUSTED → no annotation
		}
		uri := r.Dir
		if uri == "" {
			uri = r.SkillID
		}
		run.Results = append(run.Results, sarifResult{
			RuleID:  ruleID,
			Level:   level,
			Message: sarifText{Text: r.SkillID + ": " + r.Details},
			Locations: []sarifLocation{{
				PhysicalLocation: sarifPhysicalLocation{
					ArtifactLocation: sarifArtifactLocation{URI: uri},
				},
			}},
		})
	}

	log := sarifLog{Schema: sarifSchema, Version: sarifVersion, Runs: []sarifRun{run}}
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(log)
}
