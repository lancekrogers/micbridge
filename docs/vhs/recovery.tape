# Crash and recoverable-partial workflow
Output docs/vhs/recovery.gif
Set Shell zsh
Set FontSize 14
Set Width 1050
Set Height 560
Set TypingSpeed 25ms

Env MICBRIDGE_STATE_DIR "/tmp/micbridge-vhs-recovery"
Env MICBRIDGE_SOCK "/tmp/micbridge-vhs-recovery/micbridge.sock"
Env MICBRIDGE_FAKE "1"

Type "rm -rf /tmp/micbridge-vhs-recovery && mkdir -p /tmp/micbridge-vhs-recovery"
Enter
Type "micbridge serve --json >/tmp/micbridge-vhs-recovery/server.log 2>&1 & server_pid=$!"
Enter
Sleep 500ms
Type "micbridge record --json"
Enter
Sleep 750ms
Type "kill -KILL $server_pid && wait $server_pid 2>/dev/null || true"
Enter
Sleep 300ms
Type "micbridge recover --json"
Enter
Sleep 1s
