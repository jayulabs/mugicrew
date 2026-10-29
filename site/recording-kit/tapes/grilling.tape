# Grilling session: the Defog tab, a grilling bead opened, one question answered.
# Keys and position only (never an id). The Defog list sorts by priority, then newest
# first, and seed.sh makes the grilling P1, so it is the first row and one j from
# the top lands on it.
# The session is stub-bin/claude, a scripted stand-in: a live claude needs a
# sign-in and real plan usage, and a fresh one stops at its theme picker.
Output "/out/grilling.mp4"
Require mugicrew
Source /kit/settings.tape
Hide
Type "cd /tmp/demo && export PS1='$ ' PATH=/kit/stub-bin:$PATH && clear" Enter
Show
Type "mugicrew" Sleep 400ms Enter
Sleep 3s
Type "2" Sleep 3s
Type "j" Sleep 1s
Enter Sleep 8s
Type "Finance needs date, customer and total." Sleep 1s Enter
Sleep 8s
