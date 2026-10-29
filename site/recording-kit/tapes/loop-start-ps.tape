# loop start plus ps: the loop dispatches unattended work, ps lists the live efforts.
# Efforts the loop dispatches run stub-loop/claude, which stays up, so ps lists them alive.
Output "/out/loop-start-ps.mp4"
Require mugicrew
Source /kit/settings.tape
Hide
Type "cd /tmp/demo && export PS1='$ ' PATH=/kit/stub-loop:$PATH && clear" Enter
Show
Type "mugicrew loop start" Sleep 400ms Enter
Sleep 8s
Type "mugicrew ps" Sleep 400ms Enter
Sleep 5s
Type "mugicrew loop status" Sleep 400ms Enter
Sleep 3s
Hide
Type "mugicrew loop stop --force" Enter
Sleep 3s
