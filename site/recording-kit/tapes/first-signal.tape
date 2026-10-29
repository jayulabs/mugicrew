# Filing and triaging a Signal in the TUI: Ideate tab, the new row, then triage it.
# Keys and position only. The form saves on ctrl+s. Triage (enter on the Signal row)
# opens a claude session, which is stub-triage/claude, a scripted stand-in: a live
# one needs a sign-in and real plan usage. The new Signal is the newest at its
# priority, so it is the first row under Actionable: g t goes to the top, then
# j j passes '+ New Signal' and the Actionable header onto it.
Output "/out/first-signal.mp4"
Require mugicrew
Source /kit/settings.tape
Hide
Type "cd /tmp/demo && export PS1='$ ' PATH=/kit/stub-triage:$PATH && clear" Enter
Show
Type "mugicrew" Sleep 400ms Enter
Sleep 3s
Type "1" Sleep 2s
Enter Sleep 1s
Type "Add a health-check endpoint" Sleep 1s
Tab Sleep 500ms
Type "A /healthz route for load balancers." Sleep 1s
Ctrl+S Sleep 3s
Type "g" Sleep 200ms
Type "t" Sleep 500ms
Type "j" Sleep 300ms
Type "j" Sleep 2s
Enter Sleep 10s
