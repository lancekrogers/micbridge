// Package tui implements Micbridge's responsive interactive terminal client.
package tui

import (
	"context"
	"encoding/json"
	"fmt"
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
	service  Service
	width    int
	height   int
	healthy  bool
	snapshot client.RecordingSnapshot
	devices  []client.Device
	pending  string
	busy     bool
	err      string
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
				m.snapshot.State == client.StateCompleted ||
				m.snapshot.State == client.StateFailed) {
				m.pending = "arm"
			}
		case "enter", " ":
			if m.busy {
				break
			}
			switch m.snapshot.State {
			case client.StateIdle, client.StateCompleted, client.StateFailed:
				m.pending = "arm"
			case client.StateArmed:
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
			m.err = msg.err.Error()
			break
		}
		m.err = ""
		m.devices = msg.devices
		if healthy, ok := msg.status["healthy"].(bool); ok {
			m.healthy = healthy
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
		}
	}
	recordingPanel := panelStyle(width).Render(strings.Join([]string{
		"RECORDING",
		"",
		"State     " + state,
		"Session   " + fallback(m.snapshot.SessionID, "—"),
		"Daemon    " + health,
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
		}, "\n"))
		devicePanel = panelStyle(panelWidth).Render("DEVICE\n\n" + strings.Join(deviceLines, "\n"))
		body = lipgloss.JoinHorizontal(lipgloss.Top, recordingPanel, " ", devicePanel)
	}

	footer := "↵/space action  a arm  r refresh  q quit"
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
