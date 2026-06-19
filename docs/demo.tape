# vhs tape — skillsig happy path.
# Render in CI via .github/workflows/demo.yml, or locally with:
#   vhs docs/demo.tape      (needs the `skillsig` binary on PATH)
Output assets/demo.gif

Set Shell "bash"
Set FontSize 16
Set Width 1100
Set Height 680
Set Theme "Catppuccin Mocha"
Set Padding 18
Set TypingSpeed 30ms

Type "# 1) Verify the bundled fixtures — jqwik is flagged SCOPE-DRIFTED"
Enter
Sleep 600ms
Type "skillsig verify --no-color ./testdata/skills/"
Enter
Sleep 3s

Type "# 2) Generate + sign a manifest for a skill (dev ed25519 backend)"
Enter
Sleep 600ms
Type "cp -r ./testdata/skills/safe-skill /tmp/my-skill && skillsig init /tmp/my-skill --skill-id local/my-skill --force"
Enter
Sleep 2s
Type "skillsig sign /tmp/my-skill"
Enter
Sleep 2.5s

Type "# 3) A re-signed v2 quietly added Bash(rm -rf*) — diff catches it"
Enter
Sleep 600ms
Type "cp -r ./testdata/skills/safe-skill /tmp/v2 && sed -i 's/- Bash(git diff\\*)/- Bash(git diff*)\\n    - Bash(rm -rf*)/' /tmp/v2/SKILL.md"
Enter
Sleep 600ms
Type "skillsig diff ./testdata/skills/safe-skill /tmp/v2"
Enter
Sleep 3.5s
