package hollyland35470007

import (
	"fmt"

	"github.com/lancekrogers/micbridge/internal/driver"
)

// HID press/release measured reports (Consumer Control Volume Increment path).
var (
	HIDPress   = []byte{0x06, 0x01, 0x00}
	HIDRelease = []byte{0x06, 0x00, 0x00}
)

// ParseHIDReport normalizes measured HID reports to control events.
func ParseHIDReport(report []byte, pressID string) (driver.ControlEvent, error) {
	if len(report) < 3 {
		return driver.ControlEvent{}, fmt.Errorf("hollyland: short HID report")
	}
	switch {
	case report[0] == 0x06 && report[1] == 0x01 && report[2] == 0x00:
		return driver.ControlEvent{
			Type: "pressed", Control: "primary", PressID: pressID,
			SideEffects: []string{"host.volume_increment"},
		}, nil
	case report[0] == 0x06 && report[1] == 0x00 && report[2] == 0x00:
		return driver.ControlEvent{
			Type: "released", Control: "primary", PressID: pressID,
		}, nil
	default:
		return driver.ControlEvent{}, fmt.Errorf("hollyland: unrecognized HID report")
	}
}

// ParseEvdevKey maps Linux KEY_VOLUMEUP (115) press/release.
func ParseEvdevKey(code int, value int, pressID string) (driver.ControlEvent, error) {
	// value: 1 press, 0 release, 2 repeat ignored
	if code != 115 {
		return driver.ControlEvent{}, fmt.Errorf("hollyland: not volume-up key")
	}
	switch value {
	case 1:
		return driver.ControlEvent{
			Type: "pressed", Control: "primary", PressID: pressID,
			SideEffects: []string{"host.volume_increment"},
		}, nil
	case 0:
		return driver.ControlEvent{Type: "released", Control: "primary", PressID: pressID}, nil
	default:
		return driver.ControlEvent{}, fmt.Errorf("hollyland: ignore key repeat")
	}
}
