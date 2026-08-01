# up.tape — the human-eye complement to the TE golden tests (spec §6.4):
# styled help (fang), the init wizard short-circuit, and the TE-2 failure
# frame (full diagnosis box + explain footer) — all WITHOUT a live cluster
# or docker, so the tape records anywhere.
#
# Record: go build -o cube-idp . && PATH="$PWD:$PATH" vhs docs/vhs/up.tape
# Regenerate per release (WP10).

Require cube-idp

Output docs/vhs/up.gif

Set Shell "bash"
Set FontSize 15
Set Width 1100
Set Height 650
Set Padding 12
Set TypingSpeed 40ms

# Work in a scratch directory so the demo never touches a real cube.
Hide
Type "cd $(mktemp -d)"
Enter
Type "clear"
Enter
Show

# Styled help/usage via fang — CUBE boxes stay ours.
Type "cube-idp --help"
Enter
Sleep 3s

# up with no cube.yaml: the failure frame — diagnosis box LAST, with the
# explain footer (TE-2). No cluster is created.
Type "cube-idp up"
Enter
Sleep 3s

# The footer's promise holds offline.
Type "cube-idp explain CUBE-0001"
Enter
Sleep 3s

# init with a flag short-circuits the wizard; up would now build the cube.
Type "cube-idp init --name demo"
Enter
Sleep 2s
Type "cat cube.yaml | head -12"
Enter
Sleep 4s
