# Deterministic VHS workflows

These tape scripts document CLI flows without hardware. Render with
[VHS](https://github.com/charmbracelet/vhs) when available:

```bash
vhs docs/vhs/setup.tape
vhs docs/vhs/record-fake.tape
vhs docs/vhs/tui.tape
vhs docs/vhs/recovery.tape
```

Tapes use `micbridge` from PATH and a temporary state directory. They must not
capture real audio or require a physical receiver.
