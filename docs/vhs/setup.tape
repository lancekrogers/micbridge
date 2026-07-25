# Guided fake-device setup (hardware-free)
Output docs/vhs/setup.gif
Set Shell zsh
Set FontSize 14
Set Width 1000
Set Height 500
Set TypingSpeed 30ms

Env MICBRIDGE_STATE_DIR "/tmp/micbridge-vhs-setup"
Env MICBRIDGE_SOCK "/tmp/micbridge-vhs-setup/micbridge.sock"
Env MICBRIDGE_FAKE "1"

Type "rm -rf /tmp/micbridge-vhs-setup && mkdir -p /tmp/micbridge-vhs-setup"
Enter
Type "./bin/micbridge serve --json >/tmp/micbridge-vhs-setup/server.log 2>&1 & server_pid=$!"
Enter
Sleep 500ms
Type "./bin/micbridge doctor --json"
Enter
Sleep 300ms
Type "./bin/micbridge devices --json"
Enter
Sleep 500ms
Type "./bin/micbridge setup --json"
Enter
Sleep 750ms
Type "kill $server_pid && wait $server_pid 2>/dev/null || true"
Enter
