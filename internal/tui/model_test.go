package tui_test

import (
	"context"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/lancekrogers/micbridge/internal/tui"
	"github.com/lancekrogers/micbridge/pkg/client"
)

type fakeService struct{}

type larkService struct{ fakeService }

func (larkService) Devices(context.Context) ([]client.Device, error) {
	return []client.Device{{
		Identity: client.Identity{
			DeviceID: "dev_lark", DriverID: "hollyland-3547-0007",
			DisplayName: "Hollyland LARK M2 receiver (3547:0007)",
		},
	}}, nil
}

func (fakeService) Status(context.Context) (map[string]any, error) {
	return map[string]any{
		"healthy":        true,
		"recordings_dir": "/home/test/.local/state/micbridge/recordings",
		"recording": client.RecordingSnapshot{
			SessionID: "rec_test", State: client.StateArmed,
		},
	}, nil
}

type completedService struct{ fakeService }

type completedLARKService struct{ completedService }

func (completedLARKService) Devices(context.Context) ([]client.Device, error) {
	return larkService{}.Devices(context.Background())
}

func (completedService) Status(context.Context) (map[string]any, error) {
	return map[string]any{
		"healthy":        true,
		"recordings_dir": "/state/recordings",
		"recording": client.RecordingSnapshot{
			SessionID: "rec_done", State: client.StateCompleted,
			Actor: "binding", Reason: "receiver pressed", OutputPath: "/state/recordings/rec_done.wav",
		},
	}, nil
}

func TestLARKM2ControlGuidanceIsVisible(t *testing.T) {
	model := tui.NewModel(larkService{})
	batch, ok := model.Init()().(tea.BatchMsg)
	if !ok || len(batch) == 0 {
		t.Fatalf("init did not return a refresh batch")
	}
	updated, _ := model.Update(batch[0]())
	view := updated.(tui.Model).View()
	if !strings.Contains(view, "Double-press either LARK M2") ||
		!strings.Contains(view, "Single press toggles noise cancellation") {
		t.Fatalf("LARK M2 guidance missing: %q", view)
	}
}
func (fakeService) Devices(context.Context) ([]client.Device, error) {
	return []client.Device{{
		Identity: client.Identity{DeviceID: "dev_test", DisplayName: "Test receiver"},
	}}, nil
}
func (fakeService) CreateRecording(context.Context, string, string) (client.RecordingSnapshot, error) {
	return client.RecordingSnapshot{SessionID: "rec_test", State: client.StateArmed}, nil
}
func (fakeService) RecordingAction(context.Context, string, string, string) (client.RecordingSnapshot, error) {
	return client.RecordingSnapshot{SessionID: "rec_test", State: client.StateRecording}, nil
}

func TestResponsiveViewAndMutationConfirmation(t *testing.T) {
	model := tui.NewModel(fakeService{})
	updated, cmd := model.Update(tea.WindowSizeMsg{Width: 60, Height: 24})
	model = updated.(tui.Model)
	if cmd != nil {
		t.Fatal("resize unexpectedly returned command")
	}
	view := model.View()
	if !strings.Contains(view, "MICBRIDGE") || !strings.Contains(view, "No supported receiver") {
		t.Fatalf("narrow view=%q", view)
	}
	updated, _ = model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	model = updated.(tui.Model)
	if view = model.View(); !strings.Contains(view, "Confirm arm?") {
		t.Fatalf("mutation was not confirmed: %q", view)
	}
	updated, action := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'y'}})
	if action == nil {
		t.Fatal("confirmation did not schedule action")
	}
	model = updated.(tui.Model)
	updated, _ = model.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	if view = updated.(tui.Model).View(); !strings.Contains(view, "DEVICE") {
		t.Fatalf("wide view=%q", view)
	}
}

func TestCompletedRecordingIsReadyForNextStartAndShowsArtifact(t *testing.T) {
	model := tui.NewModel(completedService{})
	batch := model.Init()().(tea.BatchMsg)
	updated, _ := model.Update(batch[0]())
	model = updated.(tui.Model)
	view := model.View()
	for _, want := range []string{"Ready for next recording", "rec_done.wav", "/state/recordings", "binding • receiver pressed"} {
		if !strings.Contains(view, want) {
			t.Fatalf("completed view missing %q: %q", want, view)
		}
	}
	updated, _ = model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if view = updated.(tui.Model).View(); !strings.Contains(view, "Confirm start?") {
		t.Fatalf("completed action should start next recording: %q", view)
	}
}

func TestCompletedLARKRecordingExplainsDoublePressReadiness(t *testing.T) {
	model := tui.NewModel(completedLARKService{})
	batch := model.Init()().(tea.BatchMsg)
	updated, _ := model.Update(batch[0]())
	if view := updated.(tui.Model).View(); !strings.Contains(view, "Ready for next double-press") {
		t.Fatalf("LARK completed hint missing: %q", view)
	}
}
