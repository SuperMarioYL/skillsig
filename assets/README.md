# assets/

This directory holds the rendered demo cast referenced from the project READMEs.

## Files

| Path | What | How it gets here |
| --- | --- | --- |
| `demo.tape` | [vhs](https://github.com/charmbracelet/vhs) script that drives the recording | hand-edited |
| `demo.cast` | asciinema cast embedded in the READMEs | generated from `demo.tape` |
| `demo.gif` | fallback GIF for renderers that won't load asciinema | generated from `demo.tape` |

## Generating the demo

```bash
# one-time
brew install vhs ffmpeg ttyd

# regenerate both assets
vhs assets/demo.tape
```

The script targets ~30 seconds and runs through:

1. `skillsig verify ./testdata/skills/` — the 3-color table with the jqwik
   fixture flagged `SCOPE-DRIFTED`.
2. `skillsig init` then `skillsig sign` — the author flow.
3. `skillsig diff` — catches a silently-added `Bash(rm -rf ...)` grant.

The `sign` step uses the `--dev` ed25519 backend because real keyless OIDC
requires a browser handoff that the recording can't fake. The README calls
this out explicitly.
