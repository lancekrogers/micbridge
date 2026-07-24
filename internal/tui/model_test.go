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

func (fakeService) Status(context.Context) (map[string]any, error) {
	return map[string]any{
		"healthy": true,
		"recording": client.RecordingSnapshot{
			SessionID: "rec_test", State: client.StateArmed,
		},
	}, nil
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
