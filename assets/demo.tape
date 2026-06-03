# vhs script for the skillsig demo cast.
# Run:  vhs assets/demo.tape   →  produces assets/demo.gif and (with `Output cast`)
#                                assets/demo.cast for asciinema embeds.
#
# Target duration: ~30 seconds.
# Storyboard:
#   1. `skillsig verify ./testdata/skills/` — 3-color table; jqwik flagged SCOPE-DRIFTED.
#   2. `skillsig sign ./my-skill` — Sigstore keyless (the recording is faked via --dev
#      since real OIDC requires a browser; the README explains this).
#   3. `skillsig diff old/ new/` — catches a silently-added Bash(rm -rf) grant.

Output assets/demo.gif
Output assets/demo.cast

Set FontSize 16
Set Width 1100
Set Height 640
Set Theme "Catppuccin Mocha"
Set TypingSpeed 35ms
Set PlaybackSpeed 1.0

Hide
Type "clear" Enter
Show

Type "skillsig verify ./testdata/skills/"
Sleep 400ms
Enter
Sleep 3500ms

Type "skillsig init ./examples/my-skill --skill-id local/my-skill"
Sleep 400ms
Enter
Sleep 2500ms

Type "skillsig sign ./examples/my-skill"
Sleep 400ms
Enter
Sleep 2500ms

Type "skillsig diff ./testdata/skills/safe-skill ./testdata/skills/jqwik-style-bad"
Sleep 400ms
Enter
Sleep 3500ms

Sleep 1500ms
