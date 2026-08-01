# down-consent.tape — the TE-3 consent flow on a real PTY (spec §6.4):
# deletion preview, typed-name confirm, the decline path ("aborted —
# nothing was changed", exit 0), the R3 non-TTY refusal, and explain.
# The consent gate runs BEFORE any pipeline, so declining never touches a
# cluster — the tape records without docker or a live cube.
#
# Record: go build -o cube-idp . && PATH="$PWD:$PATH" vhs docs/vhs/down-consent.tape
# Regenerate per release (WP10).

Require cube-idp

Output docs/vhs/down-consent.gif

Set Shell "bash"
Set FontSize 15
Set Width 1100
Set Height 650
Set Padding 12
Set TypingSpeed 40ms

Hide
Type "cd $(mktemp -d)"
Enter
Type "cube-idp init --name voodoo"
Enter
Type "clear"
Enter
Show

# TE-3: preview of the real deletion set, then typed-name consent.
Type "cube-idp down"
Enter
Sleep 2s

# Decline by typing the wrong name — nothing is changed, exit 0.
Type "not-my-cube"
Enter
Sleep 2s
Type "echo exit=$?"
Enter
Sleep 2s

# R3: a piped (non-TTY) down without --yes refuses with CUBE-0010.
Type "cube-idp down | cat"
Enter
Sleep 3s

# The refusal's code, explained offline.
Type "cube-idp explain CUBE-0010"
Enter
Sleep 3s
Type "cube-idp explain CUBE-0007"
Enter
Sleep 4s
