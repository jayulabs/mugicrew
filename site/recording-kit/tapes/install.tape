# fixture: none
# Install -> init -> doctor -> doctor fix, in the fresh-install container's own
# state: no seeded fixture (inside.sh skips seed.sh for a tape marked above).
Output "/out/install.mp4"
Require mugicrew
Source /kit/settings.tape
Hide
Type "npm rm -g @jayulabs/mugicrew >/dev/null 2>&1; export PS1='$ ' && mkdir -p /tmp/fresh && cd /tmp/fresh && git init -q -b main && git -c user.name=demo -c user.email=demo@example.invalid commit -q --allow-empty -m init && clear" Enter
Sleep 6s
Show
Type "npm i -g @jayulabs/mugicrew" Sleep 400ms Enter
Sleep 30s
Type "mugicrew init" Sleep 400ms Enter
Sleep 2s
# init asks for an issue prefix on a terminal; answer it, then wait out bd init
# (it starts the Dolt server) before the next command is typed.
Type "fresh" Sleep 400ms Enter
Sleep 20s
Type "mugicrew doctor" Sleep 400ms Enter
Sleep 6s
Type "mugicrew doctor fix" Sleep 400ms Enter
Sleep 10s
