# Fake record/stop via CLI JSON
Output docs/vhs/record-fake.gif
Set Shell zsh
Set FontSize 14
Set Width 1000
Set Height 500

Env MICBRIDGE_STATE_DIR /tmp/micbridge-vhs-rec
Env MICBRIDGE_SOCK /tmp/micbridge-vhs-rec/micbridge.sock

Type "micbridge version"  
Enter
Sleep 200ms
Type "micbridge doctor --json"  
Enter
Sleep 500ms
