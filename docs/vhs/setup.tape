# Guided fake-device setup (hardware-free)
Output docs/vhs/setup.gif
Set Shell zsh
Set FontSize 14
Set Width 1000
Set Height 500
Set TypingSpeed 30ms

Env MICBRIDGE_STATE_DIR /tmp/micbridge-vhs-setup
Env MICBRIDGE_SOCK /tmp/micbridge-vhs-setup/micbridge.sock
Env MICBRIDGE_SERVE_ONCE 1

Type "micbridge serve --json"  
Enter
Sleep 500ms
Type "micbridge doctor --json"  
Enter
Sleep 300ms
Type "micbridge devices --json"  
Enter
Sleep 500ms
