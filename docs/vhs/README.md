# Deterministic VHS workflows

These tape scripts document CLI flows without hardware. Render with
[VHS](https://github.com/charmbracelet/vhs) when available:

```bash
vhs docs/vhs/setup.tape
vhs docs/vhs/record-fake.tape
vhs docs/vhs/tui.tape
vhs docs/vhs/recovery.tape
```

Tapes use the current checkout's `./bin/micbridge` (via `PATH`) and a temporary
state directory, so an older installed binary cannot satisfy the evidence. They
must not capture real audio or require a physical receiver.

Setup (temp dirs, daemon start) and teardown are wrapped in VHS `Hide`/`Show`
so the GIFs only show the user-facing commands. The recovery tape still shows
the crash (`kill -KILL`) because that is the workflow being demonstrated.
