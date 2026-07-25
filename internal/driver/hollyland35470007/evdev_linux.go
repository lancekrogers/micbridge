//go:build linux

package hollyland35470007

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"
	"syscall"
	"time"

	"github.com/lancekrogers/micbridge/internal/driver"
)

const (
	evKey       = 0x01
	keyVolumeUp = 115
)

func readEvdev(ctx context.Context, path string, emit func(driver.ControlEvent) error) error {
	file, err := os.OpenFile(path, os.O_RDONLY|syscall.O_NONBLOCK, 0)
	if err != nil {
		return err
	}
	defer func() { _ = file.Close() }()
	var (
		raw      [24]byte
		pressID  string
		pressSeq uint64
	)
	for {
		if err := ctx.Err(); err != nil {
			return nil
		}
		n, err := file.Read(raw[:])
		if err != nil {
			if errors.Is(err, syscall.EAGAIN) || errors.Is(err, syscall.EWOULDBLOCK) {
				select {
				case <-ctx.Done():
					return nil
				case <-time.After(10 * time.Millisecond):
					continue
				}
			}
			return err
		}
		if n == 0 {
			return io.EOF
		}
		if n != len(raw) {
			return fmt.Errorf("hollyland: short evdev event %d", n)
		}
		typ := binary.NativeEndian.Uint16(raw[16:18])
		code := binary.NativeEndian.Uint16(raw[18:20])
		value := int32(binary.NativeEndian.Uint32(raw[20:24]))
		if typ != evKey || code != keyVolumeUp || value == 2 {
			continue
		}
		switch value {
		case 1:
			pressSeq++
			pressID = fmt.Sprintf("press_%d", pressSeq)
			if err := emit(driver.ControlEvent{
				Type: "pressed", Control: "primary", PressID: pressID,
				SideEffects: []string{"host.volume_increment"},
			}); err != nil {
				return err
			}
		case 0:
			if pressID == "" {
				continue
			}
			if err := emit(driver.ControlEvent{Type: "released", Control: "primary", PressID: pressID}); err != nil {
				return err
			}
			pressID = ""
		}
	}
}
