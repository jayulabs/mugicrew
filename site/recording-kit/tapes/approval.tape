# Approval: the Execute tab lists a Checkpoint's Approval waiting on the human; open
# its Decision Packet and read it. Keys and position only. seed.sh files a small
# Checkpoint with every Build closed and a pending Approval, so no agent run is needed;
# approving itself happens in Luffy's session, which a recording cannot host, so the
# clip ends on the Packet.
Output "/out/approval.mp4"
Require mugicrew
Source /kit/settings.tape
Hide
Type "cd /tmp/demo && export PS1='$ ' && clear" Enter
Show
Type "mugicrew" Sleep 400ms Enter
Sleep 3s
Type "3" Sleep 3s
Type "j" Sleep 2s
Type "d" Sleep 3s
PageDown Sleep 2s
PageDown Sleep 2s
Type "d" Sleep 1s
Type "q" Sleep 500ms
