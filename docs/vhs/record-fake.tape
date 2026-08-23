# Fake record/stop via CLI JSON
Output docs/vhs/record-fake.gif
Set Shell zsh
Set FontSize 14
Set Width 1000
Set Height 500

Env MICBRIDGE_STATE_DIR "/tmp/micbridge-vhs-rec"
Env MICBRIDGE_SOCK "/tmp/micbridge-vhs-rec/micbridge.sock"
Env MICBRIDGE_FAKE "1"

Hide
Type `export PATH="$PWD/bin:$PATH"`
Enter
Type "rm -rf /tmp/micbridge-vhs-rec && mkdir -p /tmp/micbridge-vhs-rec"
Enter
Type "micbridge serve --json >/tmp/micbridge-vhs-rec/server.log 2>&1 & server_pid=$!"
Enter
Sleep 500ms
Type "clear"
Enter
Show

Type "micbridge record arm --json"
Enter
Sleep 300ms
Type "micbridge record --json"
Enter
Sleep 300ms
Type "micbridge stop --json"
Enter
Sleep 300ms
Type "micbridge status --json"
Enter
Sleep 500ms
Type "file /tmp/micbridge-vhs-rec/recordings/rec_1.wav"
Enter
Sleep 500ms

Hide
Type "kill $server_pid && wait $server_pid 2>/dev/null || true"
Enter
