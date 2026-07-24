package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/lancekrogers/micbridge/internal/api"
	"github.com/lancekrogers/micbridge/internal/core"
	"github.com/lancekrogers/micbridge/internal/storage"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintf(os.Stderr, "micbridge: %v\n", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) == 0 {
		printUsage(os.Stdout)
		return nil
	}

	switch args[0] {
	case "version", "--version", "-V":
		fmt.Printf("%s %s\n", core.Name, core.Version)
		return nil
	case "status":
		return printStatus(args[1:])
	case "help", "-h", "--help":
		printUsage(os.Stdout)
		return nil
	default:
		return fmt.Errorf("unknown command %q (try 'micbridge help')", args[0])
	}
}

func printUsage(w *os.File) {
	_, _ = fmt.Fprintf(w, "Usage: %s <command>\n\n", core.Name)
	_, _ = fmt.Fprintln(w, "Commands:")
	_, _ = fmt.Fprintln(w, "  version   Print version")
	_, _ = fmt.Fprintln(w, "  status    Print daemon status (uses MICBRIDGE_STATE_DIR if set)")
	_, _ = fmt.Fprintln(w, "  help      Show this help")
}

func printStatus(args []string) error {
	stateDir := os.Getenv("MICBRIDGE_STATE_DIR")
	if stateDir == "" {
		// Bootstrap default: relative local state, not a system path.
		stateDir = filepath.Join(".", ".micbridge")
	}
	layout, err := storage.NewLayout(stateDir)
	if err != nil {
		return err
	}

	st := api.CurrentStatus()
	payload := map[string]any{
		"name":           st.Name,
		"version":        st.Version,
		"healthy":        st.Healthy,
		"state_dir":      layout.Root,
		"recordings_dir": layout.RecordingsDir(),
		"journals_dir":   layout.JournalsDir(),
	}
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(payload)
}
