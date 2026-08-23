# Responsive interactive recorder
Output docs/vhs/tui.gif
Set Shell zsh
Set FontSize 14
Set Width 1100
Set Height 650
Set TypingSpeed 25ms

Env MICBRIDGE_STATE_DIR "/tmp/micbridge-vhs-tui"
Env MICBRIDGE_SOCK "/tmp/micbridge-vhs-tui/micbridge.sock"
Env MICBRIDGE_FAKE "1"

Hide
Type `export PATH="$PWD/bin:$PATH"`
Enter
Type "rm -rf /tmp/micbridge-vhs-tui && mkdir -p /tmp/micbridge-vhs-tui"
Enter
Type "micbridge serve --json >/tmp/micbridge-vhs-tui/server.log 2>&1 & server_pid=$!"
Enter
Sleep 500ms
Type "clear"
Enter
Show

Type "micbridge ui"
Enter
Sleep 1s
Enter
Sleep 300ms
Type "y"
Sleep 1s
Enter
Sleep 300ms
Type "y"
Sleep 1s
Enter
Sleep 300ms
Type "y"
Sleep 1s
Type "q"
Sleep 500ms

Hide
Type "kill $server_pid && wait $server_pid 2>/dev/null || true"
Enter
