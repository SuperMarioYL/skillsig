[简体中文](./README.md) · [Website](https://skillsig.lei6393.com) · [GitHub](https://github.com/SuperMarioYL/skillsig)

<picture>
  <source media="(max-width: 600px) and (prefers-color-scheme: dark)" srcset="./assets/presentation/hero-mobile-dark.svg">
  <source media="(max-width: 600px)" srcset="./assets/presentation/hero-mobile-light.svg">
  <source media="(prefers-color-scheme: dark)" srcset="./assets/presentation/hero-dark.svg">
  <img src="./assets/presentation/hero-light.svg" width="960" alt="Hero diagram">
</picture>

# skillsig

**Review skill permissions before an update becomes trusted.**

skillsig compares a skill’s declared scope with its frontmatter grants and a saved baseline, then checks its signature bundle.

## Why use it

A skill update can broaden allowed tools without changing the task you installed it for. A reviewed manifest and baseline make that expansion visible during verification.

- **Compare declared grants** — Tool patterns are checked against manifest scope.
- **Detect baseline expansion** — A saved reviewed scope can expose later growth.
- **Use CI outputs** — JSON, SARIF and exit codes support automation.

## Architecture

<picture>
  <source media="(max-width: 600px) and (prefers-color-scheme: dark)" srcset="./assets/presentation/architecture-mobile-dark.svg">
  <source media="(max-width: 600px)" srcset="./assets/presentation/architecture-mobile-light.svg">
  <source media="(prefers-color-scheme: dark)" srcset="./assets/presentation/architecture-dark.svg">
  <img src="./assets/presentation/architecture-light.svg" width="960" alt="Architecture diagram">
</picture>

Manifest parsing combines SKILL.md and SKILLSIG.yaml inputs. The scope scanner checks grants and prior lock entries. Signature verification contributes to TRUSTED, UNSIGNED, SCOPE-DRIFTED or MALFORMED verdicts, which are rendered as a table, JSON or SARIF.

| Component | Responsibility |
| --- | --- |
| `Skill manifest` | internal/manifest |
| `Scope scanner` | internal/scope |
| `Signature verifier` | internal/verifier |
| `Table / JSON / SARIF` | internal/report |

## Install and quickstart

Build with the version declared in the repository manifest. Run the example from the repository root.

```bash
git clone https://github.com/SuperMarioYL/skillsig.git
cd skillsig
go build ./cmd/skillsig
```

Verify the bundled skill fixture directory against an empty isolated baseline path.

```bash
SKILLSIG_HOME=./examples/demo-empty-home go run ./cmd/skillsig verify ./testdata/skills/ --no-color
```

## Recorded demo

<picture>
  <source media="(max-width: 600px) and (prefers-color-scheme: dark)" srcset="./assets/presentation/process-mobile-dark.svg">
  <source media="(max-width: 600px)" srcset="./assets/presentation/process-mobile-light.svg">
  <source media="(prefers-color-scheme: dark)" srcset="./assets/presentation/process-dark.svg">
  <img src="./assets/presentation/process-light.svg" width="960" alt="Process diagram">
</picture>

The recorded report exposes the fixture scope mismatch and missing signature state without executing skill content.

```text
SKILL                              VERDICT        DETAILS
---------------------------------  -------------  -----------------------------------------------
skillsig-examples/jqwik-style-bad  SCOPE-DRIFTED  undeclared grant(s): Bash(rm -rf ~/.claude/*)
skillsig-examples/safe-skill       UNSIGNED       no valid signature bundle (unsigned)
scope-mismatch                     UNSIGNED       no skillsig manifest (sidecar or SKILLSIG.yaml)
3 skill(s): 0 trusted, 2 unsigned, 1 scope-drifted, 0 malformed
```

The complete command and output are recorded in [docs/demo-results.json](./docs/demo-results.json). Inputs and reproduction code are included in the repository.

![Existing terminal recording](./assets/demo.gif)

The existing recording is retained for context; the text example above documents the reproducible scenario.

## Usage

The CLI exposes the following operations. Commands after the example use your own paths or identifiers.

```bash
go run ./cmd/skillsig verify ./testdata/skills/ --no-color
go run ./cmd/skillsig verify ./testdata/skills/ --json
# CI rejects unsigned, drifted or unreadable rows:
skillsig verify ./skills --ci --sarif results.sarif
```

## Configuration

SKILLSIG_HOME relocates the local lock storage. Plain verify is read-only; --trust records only currently trusted scopes. --force-trust with --trust explicitly rebaselines drifted entries and should follow a scope review. --json and --sarif select structured reports.

## Integrations and responsibilities

<picture>
  <source media="(max-width: 600px) and (prefers-color-scheme: dark)" srcset="./assets/presentation/integrations-mobile-dark.svg">
  <source media="(max-width: 600px)" srcset="./assets/presentation/integrations-mobile-light.svg">
  <source media="(prefers-color-scheme: dark)" srcset="./assets/presentation/integrations-dark.svg">
  <img src="./assets/presentation/integrations-light.svg" width="960" alt="Integrations diagram">
</picture>

The following routes are implemented in the source. Choose the input that matches your task and keep the resulting artifact with your project.

| Route | Implemented role |
| --- | --- |
| SKILL.md | Declared allowed-tools grants |
| SKILLSIG.yaml | Intent and attestation metadata |
| Ed25519 bundle | Local signature verification |
| Lock baseline | Cross-version scope comparison |
| SARIF | Code-scanning report artifact |

## Limits and next steps

- The Sigstore keyless backend is not wired in this build; --dev uses an ephemeral Ed25519 key.
- Manifest scope checks do not enforce runtime filesystem or network isolation. A signature is not proof of benign behavior.
- The demo only inspects bundled synthetic skills and writes no trust baseline.

A real keyless signing backend and stronger identity policy require additional implementation. The current artifact is a local scope and signature verifier.

## License and contributions

See [LICENSE](./LICENSE). When reporting an issue, include a minimal input, the command, and the observed output.
