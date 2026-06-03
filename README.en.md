**English** | [简体中文](./README.md)

<p align="center">
  <img src="https://capsule-render.vercel.app/api?type=waving&color=0:8B0000,100:E97300&height=180&section=header&text=skillsig&fontSize=68&fontColor=ffffff&animation=fadeIn&fontAlignY=42&desc=signed%20manifest%20%2B%20scope-drift%20detector%20for%20Claude%20Code%20Skills&descAlignY=66&descSize=14" alt="skillsig banner"/>
</p>

<p align="center">
  <a href="https://github.com/SuperMarioYL/skillsig/actions/workflows/ci.yml"><img alt="CI" src="https://img.shields.io/github/actions/workflow/status/SuperMarioYL/skillsig/ci.yml?branch=main&label=CI&logo=github"/></a>
  <a href="https://github.com/SuperMarioYL/skillsig/releases"><img alt="Release" src="https://img.shields.io/github/v/release/SuperMarioYL/skillsig?include_prereleases&sort=semver&label=release"/></a>
  <a href="./LICENSE"><img alt="License" src="https://img.shields.io/badge/license-MIT-2ea44f"/></a>
  <img alt="Go" src="https://img.shields.io/badge/Go-1.24-00ADD8?logo=go&logoColor=white"/>
  <img alt="Claude Code" src="https://img.shields.io/badge/Claude%20Code-ready-7C3AED"/>
  <img alt="Skill" src="https://img.shields.io/badge/Skill-attested-DC2626"/>
</p>

> **skillsig is the signed manifest that catches scope-drift in Claude Code Skills before merge.**
> Stop a silent `claude skills update` from widening permissions in CI — not after.

## Table of contents

- [Why this exists](#why-this-exists)
- [Quickstart (60 seconds)](#quickstart-60-seconds)
- [Demo](#demo)
- [The core primitive: the skillsig manifest](#the-core-primitive-the-skillsig-manifest)
- [vs trending tools](#vs-trending-tools)
- [Configuration](#configuration)
- [Wire into CI](#wire-into-ci)
- [Roadmap](#roadmap)
- [Pricing (v0.2)](#pricing-v02)
- [Contributing & license](#contributing--license)
- [Share this](#share-this)

---

## Why this exists

Two curated registries — `ComposioHQ/awesome-claude-skills` and
`sickn33/antigravity-awesome-skills` — index **1,494+ installable Skills**
across ~102K combined stars, growing ~280 stars/day. Curators like
[@affaan-m](https://github.com/affaan-m) (the
[everything-claude-code](https://github.com/affaan-m/everything-claude-code)
collection) are doing the discovery work, but every Skill is a folder of
prompts + tool grants + filesystem/network scope that
[Claude Code](https://docs.claude.com/claude-code) loads with the user's
credentials, and `claude skills update` re-pulls the latest content at the
next session. The May 2026
[**jqwik prompt-injection incident**](https://arstechnica.com/security/2026/05/fed-up-with-vibe-coders-dev-sneaks-data-nuking-prompt-injection-into-their-code/)
confirmed the attack class is live: a trusted package shipped a prompt that
told coding agents to delete app output.

Today no mechanism answers the three questions a platform owner has:

- Of the Skills my team installed, which ones declare write access to `~/`?
- Was the manifest I audited last week the same one that just pulled?
- Did this Skill quietly add `Bash(rm -rf …)` between 0.3.0 and 0.3.1?

skillsig answers all three: a YAML manifest declaring the four-axis scope,
Sigstore keyless signing, and a `~/.skillsig/lock.yaml` baseline so drift
across versions fails CI even when the signature is valid.

## Quickstart (60 seconds)

```bash
# 1) install (Homebrew tap coming; use go install for now)
go install github.com/SuperMarioYL/skillsig/cmd/skillsig@latest

# 2) see it work immediately (the repo ships a jqwik-shaped fixture)
git clone https://github.com/SuperMarioYL/skillsig && cd skillsig
skillsig verify ./testdata/skills/

# 3) generate a manifest for your own Skill
cd ~/.claude/skills/my-skill
skillsig init
$EDITOR SKILLSIG.yaml          # tighten declares.fs_write / declares.network_egress
skillsig sign                  # ed25519 dev backend; pass --keyless for Sigstore Fulcio
```

<details>
<summary>sample output of <code>skillsig verify ./testdata/skills/</code></summary>

```text
SKILL                                       VERDICT        DETAILS
-----                                       -------        -------
skillsig-examples/jqwik-style-bad           SCOPE-DRIFTED  undeclared grant(s): Bash(rm -rf ~/.claude/*)
skillsig-examples/safe-skill                TRUSTED        scope matches declared manifest (sidecar)
scope-mismatch                              UNSIGNED       no skillsig manifest (sidecar or SKILLSIG.yaml)

3 skill(s): 1 trusted, 1 unsigned, 1 scope-drifted
```
</details>

## Demo

> 📼 Recording in progress — see [`assets/README.md`](./assets/README.md) for how
> to regenerate via [vhs](https://github.com/charmbracelet/vhs).
> Tape script: [`assets/demo.tape`](./assets/demo.tape) · target duration 30 s.

Once published, this README slot embeds:

```markdown
[![asciicast](https://asciinema.org/a/PLACEHOLDER.svg)](https://asciinema.org/a/PLACEHOLDER)
```

## The core primitive: the skillsig manifest

The new noun is the **manifest**: either embedded in `SKILL.md` as a fenced
YAML sidecar, or written as a sibling `SKILLSIG.yaml`. It declares four axes —
**model × tools × fs_write × network_egress**.

```yaml
skillsig: v1
skill_id: skillsig-examples/safe-skill
version: 0.1.0
declares:
  model: claude-opus-4-7        # author-declared target model (not in SKILL.md frontmatter)
  tools:                         # compared 1:1 against SKILL.md `allowed-tools`
    - Read
    - Edit
    - Bash(git status*)
    - Bash(git diff*)
  fs_write:                      # workspace-scoped, never $HOME
    - "${WORKSPACE}/**"
  network_egress: []             # empty = no network
attestation:
  sigstore_bundle: ./skillsig.bundle
```

`skillsig verify` treats `declares.tools` as an allowlist and scans the
actually-honored `allowed-tools` from `SKILL.md`. Any entry in the actuals
that isn't covered by the allowlist (modulo a `Tool(prefix*)` glob that
matches Claude Code's grant grammar) becomes the reason for `SCOPE-DRIFTED`
— exactly the lane the jqwik `Bash(rm -rf ~/.claude/*)` grant walked through.

## vs trending tools

| Capability | skillsig | awesome-list human review | Sigstore / SLSA alone | Host-native (rumored) |
| --- | --- | --- | --- | --- |
| Enumerate scope across 1,494+ Skills | ✓ | — | — | partial |
| Attest `(model, tools, fs_write, network)` tuple | ✓ | — | — | partial |
| Detect drift across `claude skills update` | ✓ | — | — | — |
| Sigstore keyless signing | ✓ (m2) | — | ✓ | partial |
| Cross-host (Cursor / Codex / Gemini / Antigravity) | ✓ format-portable | partial | ✓ | — |
| Works inside the GFW (CI signing + offline verify) | ✓ | ✓ | partial (Fulcio reachability) | — |

> Honest comparison:
> [awesome-claude-skills](https://github.com/ComposioHQ/awesome-claude-skills)
> and [antigravity-awesome-skills](https://github.com/sickn33/antigravity-awesome-skills)
> are still the best place to *find* a Skill — skillsig isn't trying to replace
> them, only to add a trust layer for what they already curate.

## Configuration

`skillsig verify` is zero-config by default; the knobs below opt in:

| Setting | Type | Default | Meaning |
| --- | --- | --- | --- |
| `SKILLSIG_HOME` | env | `$HOME/.skillsig` | Where the lockfile and ephemeral credentials live |
| `--ci` | flag | `false` | Exit non-zero on UNSIGNED or SCOPE-DRIFTED |
| `--no-color` | flag | `false` | Strip ANSI escapes (diffable output) |
| `attestation.sigstore_bundle` | yaml | `./skillsig.bundle` | Where verify expects the bundle |
| `~/.skillsig/lock.yaml` | yaml | auto | Per-`skill_id` baseline used for cross-version drift (m3) |

## Wire into CI

GitHub Actions:

```yaml
- name: verify installed skills have not drifted
  run: |
    go install github.com/SuperMarioYL/skillsig/cmd/skillsig@latest
    skillsig verify --ci ./skills/
```

GitLab CI:

```yaml
verify-skills:
  image: golang:1.24
  script:
    - go install github.com/SuperMarioYL/skillsig/cmd/skillsig@latest
    - skillsig verify --ci ./skills/
```

`--ci` makes any `UNSIGNED` / `SCOPE-DRIFTED` row a hard fail. Combine with
`--no-color` to get diffable plain-text output your CI provider can store.

## Roadmap

- [x] **m1** — manifest schema + parser + `skillsig verify` 3-color table + jqwik fixture
- [x] **m2** — `skillsig sign`: ed25519 dev backend + Sigstore keyless OIDC seam (sigstore-go integration)
- [ ] **m3** — `skillsig diff old/ new/` + `~/.skillsig/lock.yaml` cross-version drift
- [ ] **v0.2** — `skillsig.cloud` hosted mirror + team policy + Slack / Lark / WeChat webhooks
- [ ] **v0.3** — runtime hook: apply declared scope as a sandbox config before the host CLI loads the Skill

For per-release detail see [`CHANGELOG.md`](./CHANGELOG.md). After cloning,
recommend setting GitHub topics for discoverability:

```bash
gh repo edit --add-topic mcp --add-topic agent --add-topic claude-code --add-topic skill --add-topic supply-chain
```

## Pricing (v0.2)

The OSS half stays free forever and `verify` will always be free. The v0.1
README puts the paid path up front because the way this repo stays maintained
is the `skillsig.cloud` hosted mirror plus alerts tier.

| Tier | Price | What you get |
| --- | --- | --- |
| **Individual / OSS** | $0 | `verify` / `init` / `sign` / `diff`, self-hosted signing, community support |
| **Team** | $14/mo (≤10 seats) | `skillsig.cloud` mirror, team policy YAML, Slack / Lark / WeChat alerts, awesome-list drift subscriptions |
| **Enterprise** | $140/mo | SSO, on-prem deploy, audit log, signer identity reports, SLA |

CN pricing: ¥99 / ¥999. Stripe + Alipay + WeChat Pay all supported on
`skillsig.cloud` (closed beta at v0.1). Want to be on the early list?
[Open an issue](https://github.com/SuperMarioYL/skillsig/issues) and say hi.

## Contributing & license

- Got a fixture? Drop it into [`testdata/skills/`](./testdata/skills/) and PR.
- Want to wire a `signed:` column into an awesome-list? We're submitting
  sample PRs to ComposioHQ and sickn33 — join in.
- License: [MIT](./LICENSE). Always MIT. No CLA.

## Share this

```text
skillsig — signed manifest + scope-drift detector for Claude Code Skills.
After the jqwik incident, the only attestation built around the Skill format.
github.com/SuperMarioYL/skillsig
```
