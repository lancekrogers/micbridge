// Package tui implements Micbridge's responsive interactive terminal client.
package tui

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"

	"github.com/lancekrogers/micbridge/pkg/client"
)

type Service interface {
	Status(context.Context) (map[string]any, error)
	Devices(context.Context) ([]client.Device, error)
	CreateRecording(context.Context, string, string) (client.RecordingSnapshot, error)
	RecordingAction(context.Context, string, string, string) (client.RecordingSnapshot, error)
}

type refreshMsg struct {
	status  map[string]any
	devices []client.Device
	err     error
}

type actionMsg struct {
	snapshot client.RecordingSnapshot
	err      error
}

type tickMsg time.Time

// Model is the responsive recorder screen.
type Model struct {
	service       Service
	width         int
	height        int
	healthy       bool
	snapshot      client.RecordingSnapshot
	devices       []client.Device
	recordingsDir string
	pending       string
	busy          bool
	err           string
}

func NewModel(service Service) Model {
	return Model{
		service: service,
		width:   100,
		height:  30,
		snapshot: client.RecordingSnapshot{
			State: client.StateIdle,
		},
	}
}

func (m Model) Init() tea.Cmd {
	return tea.Batch(m.refresh(), tick())
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
	case tea.KeyMsg:
		switch msg.String() {
		case "q", "ctrl+c":
			return m, tea.Quit
		case "r":
			return m, m.refresh()
		case "esc", "n":
			m.pending = ""
		case "a":
			if !m.busy && (m.snapshot.State == client.StateIdle ||
				m.snapshot.State == client.StateFailed) {
				m.pending = "arm"
			}
		case "enter", " ":
			if m.busy {
				break
			}
			switch m.snapshot.State {
			case client.StateIdle, client.StateFailed:
				m.pending = "arm"
			case client.StateArmed, client.StateCompleted:
				m.pending = "start"
			case client.StateRecording:
				m.pending = "stop"
			}
		case "y":
			if m.pending != "" && !m.busy {
				action := m.pending
				m.pending = ""
				m.busy = true
				m.err = ""
				return m, m.runAction(action)
			}
		}
	case refreshMsg:
		m.busy = false
		if msg.err != nil {
			m.healthy = false
			m.err = "Daemon unavailable. Start it with `micbridge serve` in another terminal."
			break
		}
		m.err = ""
		m.devices = msg.devices
		if healthy, ok := msg.status["healthy"].(bool); ok {
			m.healthy = healthy
		}
		if recordingsDir, ok := msg.status["recordings_dir"].(string); ok {
			m.recordingsDir = recordingsDir
		}
		if raw, ok := msg.status["recording"]; ok {
			encoded, _ := json.Marshal(raw)
			_ = json.Unmarshal(encoded, &m.snapshot)
		}
	case actionMsg:
		m.busy = false
		if msg.err != nil {
			m.err = msg.err.Error()
		} else {
			m.snapshot = msg.snapshot
		}
		return m, m.refresh()
	case tickMsg:
		return m, tea.Batch(m.refresh(), tick())
	}
	return m, nil
}

func (m Model) refresh() tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		status, err := m.service.Status(ctx)
		if err != nil {
			return refreshMsg{err: err}
		}
		devices, err := m.service.Devices(ctx)
		return refreshMsg{status: status, devices: devices, err: err}
	}
}

func (m Model) runAction(action string) tea.Cmd {
	sessionID := m.snapshot.SessionID
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		var (
			snapshot client.RecordingSnapshot
			err      error
		)
		if action == "arm" {
			snapshot, err = m.service.CreateRecording(ctx, "arm", "")
		} else {
			snapshot, err = m.service.RecordingAction(ctx, sessionID, action, "")
		}
		return actionMsg{snapshot: snapshot, err: err}
	}
}

func tick() tea.Cmd {
	return tea.Tick(750*time.Millisecond, func(t time.Time) tea.Msg { return tickMsg(t) })
}

func (m Model) View() string {
	width := m.width
	if width < 36 {
		width = 36
	}
	header := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("#FFFFFF")).
		Background(lipgloss.Color("#6C5CE7")).
		Padding(0, 1).
		Width(width - 2).
		Render("MICBRIDGE  •  local wireless recorder")

	stateColor := lipgloss.Color("#74B9FF")
	switch m.snapshot.State {
	case client.StateArmed:
		stateColor = lipgloss.Color("#FDCB6E")
	case client.StateRecording:
		stateColor = lipgloss.Color("#FF7675")
	case client.StateCompleted:
		stateColor = lipgloss.Color("#55EFC4")
	case client.StateFailed:
		stateColor = lipgloss.Color("#E17055")
	}
	state := lipgloss.NewStyle().Bold(true).Foreground(stateColor).
		Render(strings.ToUpper(string(m.snapshot.State)))
	health := lipgloss.NewStyle().Foreground(lipgloss.Color("#FF7675")).Render("DEGRADED")
	if m.healthy {
		health = lipgloss.NewStyle().Foreground(lipgloss.Color("#55EFC4")).Render("READY")
	}

	deviceLines := []string{"No supported receiver detected"}
	if len(m.devices) > 0 {
		deviceLines = nil
		for _, device := range m.devices {
			deviceLines = append(deviceLines, fmt.Sprintf("● %s", device.Identity.DisplayName))
			deviceLines = append(deviceLines, fmt.Sprintf("  %s • %d surfaces • %d capabilities",
				device.Identity.DeviceID, len(device.Surfaces), len(device.Capabilities)))
			if device.Identity.DriverID == "hollyland-3547-0007" {
				deviceLines = append(deviceLines,
					"  Control  Double-press either LARK M2 microphone",
					"  Single press toggles noise cancellation only")
			}
		}
	}
	recordingPanel := panelStyle(width).Render(strings.Join([]string{
		"RECORDING",
		"",
		"State     " + state,
		"Session   " + fallback(m.snapshot.SessionID, "—"),
		"Daemon    " + health,
		"File      " + artifactFile(m.snapshot),
		"Folder    " + artifactFolder(m.snapshot, m.recordingsDir, width-18),
		"Last via  " + transitionSource(m.snapshot),
	}, "\n"))
	devicePanel := panelStyle(width).Render("DEVICE\n\n" + strings.Join(deviceLines, "\n"))

	body := ""
	if width < 72 {
		body = lipgloss.JoinVertical(lipgloss.Left, recordingPanel, devicePanel)
	} else {
		panelWidth := (width - 5) / 2
		recordingPanel = panelStyle(panelWidth).Render(strings.Join([]string{
			"RECORDING", "", "State     " + state,
			"Session   " + fallback(m.snapshot.SessionID, "—"),
			"Daemon    " + health,
			"File      " + artifactFile(m.snapshot),
			"Folder    " + artifactFolder(m.snapshot, m.recordingsDir, panelWidth-18),
			"Last via  " + transitionSource(m.snapshot),
		}, "\n"))
		devicePanel = panelStyle(panelWidth).Render("DEVICE\n\n" + strings.Join(deviceLines, "\n"))
		body = lipgloss.JoinHorizontal(lipgloss.Top, recordingPanel, " ", devicePanel)
	}

	footer := "↵/space action  a arm  r refresh  q quit"
	if m.snapshot.State == client.StateCompleted {
		footer = "Ready for next recording • ↵/space start  r refresh  q quit"
		if hasLARKM2(m.devices) {
			footer = "Ready for next double-press • ↵/space start  r refresh  q quit"
		}
	}
	if m.pending != "" {
		footer = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#FDCB6E")).
			Render(fmt.Sprintf("Confirm %s?  y yes  n/esc cancel", m.pending))
	} else if m.busy {
		footer = "Working…"
	}
	if m.err != "" {
		footer += "\n" + lipgloss.NewStyle().Foreground(lipgloss.Color("#FF7675")).Render("Error: "+m.err)
	}
	return lipgloss.JoinVertical(lipgloss.Left, header, "", body, "", footer)
}

func transitionSource(snapshot client.RecordingSnapshot) string {
	if snapshot.Actor == "" {
		return "—"
	}
	if snapshot.Reason == "" {
		return string(snapshot.Actor)
	}
	return string(snapshot.Actor) + " • " + snapshot.Reason
}

func artifactFile(snapshot client.RecordingSnapshot) string {
	if snapshot.OutputPath == "" {
		return "—"
	}
	return filepath.Base(snapshot.OutputPath)
}

func artifactFolder(snapshot client.RecordingSnapshot, recordingsDir string, width int) string {
	path := recordingsDir
	if snapshot.OutputPath != "" {
		path = filepath.Dir(snapshot.OutputPath)
	}
	path = fallback(path, "—")
	runes := []rune(path)
	if width < 12 || len(runes) <= width {
		return path
	}
	left := (width - 1) / 2
	right := width - left - 1
	return string(runes[:left]) + "…" + string(runes[len(runes)-right:])
}

func hasLARKM2(devices []client.Device) bool {
	for _, device := range devices {
		if device.Identity.DriverID == "hollyland-3547-0007" {
			return true
		}
	}
	return false
}

func panelStyle(width int) lipgloss.Style {
	if width < 32 {
		width = 32
	}
	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("#636E72")).
		Padding(1, 2).
		Width(width - 2)
}

func fallback(value, otherwise string) string {
	if value == "" {
		return otherwise
	}
	return value
}

// Run starts the alternate-screen interactive program.
func Run(ctx context.Context, service Service) error {
	// Interactive Micbridge screens deliberately use semantic color. Bubble
	// Tea owns a real terminal here; forcing true color also keeps PTY/VHS
	// review output faithful when capability detection sees an intermediary.
	lipgloss.SetColorProfile(termenv.TrueColor)
	program := tea.NewProgram(NewModel(service), tea.WithAltScreen(), tea.WithContext(ctx))
	_, err := program.Run()
	return err
}
