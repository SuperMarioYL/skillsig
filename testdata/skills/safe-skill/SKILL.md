---
name: safe-skill
description: A minimal example of a Claude Code skill whose runtime grants stay inside the scope its skillsig manifest declared.
allowed-tools:
  - Read
  - Edit
  - Bash(git status*)
  - Bash(git diff*)
---

# safe-skill

A toy fixture used by `skillsig verify` to demonstrate the happy path: every
entry in `allowed-tools` above is covered by the embedded `skillsig:` block
below, so the verdict comes out **TRUSTED**.

## Embedded skillsig manifest

```yaml
skillsig: v1
skill_id: skillsig-examples/safe-skill
version: 0.1.0
declares:
  model: claude-opus-4-7
  tools:
    - Read
    - Edit
    - Bash(git status*)
    - Bash(git diff*)
  fs_write:
    - "${WORKSPACE}/**"
  network_egress: []
attestation:
  sigstore_bundle: ./skillsig.bundle
```
