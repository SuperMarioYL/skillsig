---
name: scope-mismatch
description: A fixture that ships no skillsig manifest at all — neither an embedded sidecar nor a sibling SKILLSIG.yaml — so it surfaces as UNSIGNED.
allowed-tools:
  - Read
  - WebFetch
  - Bash(npm test*)
---

# scope-mismatch

This fixture has runtime tool grants but ships **without** a skillsig manifest
— the most common state for skills pulled from a curated awesome-list today.
`skillsig verify` reports it as **UNSIGNED** so the platform owner can decide
whether to keep it, vendor it, or ask the author to sign.

There is no embedded `skillsig:` YAML block below, and no
sibling `SKILLSIG.yaml` in this directory. That absence — not a malformed
manifest — is what UNSIGNED means.
