//go:build linux

package hollyland35470007

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/lancekrogers/micbridge/internal/driver"
)

// DiscoverLinux finds exact-profile USB parents and groups their audio/control
// evidence before any device node is opened.
func DiscoverLinux(ctx context.Context, usbRoot, inputClassRoot, devRoot string) ([]driver.Candidate, error) {
	if usbRoot == "" {
		usbRoot = "/sys/bus/usb/devices"
	}
	if inputClassRoot == "" {
		inputClassRoot = "/sys/class/input"
	}
	if devRoot == "" {
		devRoot = "/dev"
	}
	entries, err := os.ReadDir(usbRoot)
	if err != nil {
		return nil, err
	}
	var out []driver.Candidate
	for _, entry := range entries {
		if contextErr := ctx.Err(); contextErr != nil {
			return nil, contextErr
		}
		if !entry.IsDir() && entry.Type()&os.ModeSymlink == 0 {
			continue
		}
		parent := filepath.Join(usbRoot, entry.Name())
		vid := readTrimmed(filepath.Join(parent, "idVendor"))
		pid := readTrimmed(filepath.Join(parent, "idProduct"))
		if !strings.EqualFold(vid, USBVendorID) || !strings.EqualFold(pid, USBProductID) {
			continue
		}
		var (
			interfaceNums []int
			hasAudio      bool
			hasHID        bool
			hasVendor     bool
			descriptors   []InterfaceDescriptor
			digest        string
		)
		rawDescriptors, descriptorErr := os.ReadFile(filepath.Join(parent, "descriptors"))
		if descriptorErr == nil {
			descriptors, err = parseUSBDescriptors(rawDescriptors)
			if err != nil {
				return nil, err
			}
			sum := sha256.Sum256(rawDescriptors)
			digest = fmt.Sprintf("%x", sum[:])
			interfaceNums, hasAudio, hasHID, hasVendor = summarizeDescriptors(descriptors)
		} else {
			interfaceNums, hasAudio, hasHID, hasVendor, err = inspectInterfaces(usbRoot, entry.Name())
			if err != nil {
				return nil, err
			}
		}
		descriptorJSON, err := json.Marshal(descriptors)
		if err != nil {
			return nil, err
		}
		controlPath := findInputNode(parent, inputClassRoot, devRoot)
		candidate := driver.Candidate{
			Attributes: map[string]string{
				"usb_vid":               vid,
				"usb_pid":               pid,
				"manufacturer":          readTrimmed(filepath.Join(parent, "manufacturer")),
				"product":               readTrimmed(filepath.Join(parent, "product")),
				"serial":                readTrimmed(filepath.Join(parent, "serial")),
				"interfaces":            joinInts(interfaceNums),
				"has_audio":             strconv.FormatBool(hasAudio),
				"has_hid":               strconv.FormatBool(hasHID),
				"has_vendor":            strconv.FormatBool(hasVendor),
				"descriptor_digest":     digest,
				"interface_descriptors": string(descriptorJSON),
				"sysfs_name":            entry.Name(),
			},
			TransientPaths: map[string]string{
				"control": controlPath,
				"usb":     parent,
			},
		}
		out = append(out, candidate)
	}
	return out, nil
}

func parseUSBDescriptors(raw []byte) ([]InterfaceDescriptor, error) {
	var (
		out     []InterfaceDescriptor
		current *InterfaceDescriptor
	)
	for offset := 0; offset < len(raw); {
		if len(raw)-offset < 2 {
			return nil, fmt.Errorf("hollyland: truncated USB descriptor header")
		}
		length := int(raw[offset])
		if length < 2 || offset+length > len(raw) {
			return nil, fmt.Errorf("hollyland: invalid USB descriptor length %d", length)
		}
		descriptor := raw[offset : offset+length]
		switch descriptor[1] {
		case 4:
			if length < 9 {
				return nil, fmt.Errorf("hollyland: short interface descriptor")
			}
			out = append(out, InterfaceDescriptor{
				Number: int(descriptor[2]), Alternate: int(descriptor[3]),
				Class: fmt.Sprintf("%02x", descriptor[5]), Subclass: fmt.Sprintf("%02x", descriptor[6]),
				Protocol: fmt.Sprintf("%02x", descriptor[7]),
			})
			current = &out[len(out)-1]
		case 5:
			if length < 7 || current == nil {
				return nil, fmt.Errorf("hollyland: endpoint without valid interface descriptor")
			}
			transfers := []string{"control", "isochronous", "bulk", "interrupt"}
			current.Endpoints = append(current.Endpoints, EndpointDescriptor{
				Address:        fmt.Sprintf("%02x", descriptor[2]),
				Transfer:       transfers[int(descriptor[3]&0x03)],
				MaxPacketBytes: int(binary.LittleEndian.Uint16(descriptor[4:6]) & 0x07ff),
				Interval:       int(descriptor[6]),
			})
		}
		offset += length
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("hollyland: USB descriptor has no interfaces")
	}
	return out, nil
}

func summarizeDescriptors(descriptors []InterfaceDescriptor) ([]int, bool, bool, bool) {
	unique := map[int]struct{}{}
	var audio, hid, vendor bool
	for _, descriptor := range descriptors {
		unique[descriptor.Number] = struct{}{}
		switch strings.ToLower(descriptor.Class) {
		case "01":
			audio = true
		case "03":
			hid = true
		case "ff":
			if descriptor.Number == DefaultProfile().VendorIF &&
				strings.EqualFold(descriptor.Subclass, "f0") &&
				strings.EqualFold(descriptor.Protocol, "00") {
				vendor = true
			}
		}
	}
	numbers := make([]int, 0, len(unique))
	for number := range unique {
		numbers = append(numbers, number)
	}
	sort.Ints(numbers)
	return numbers, audio, hid, vendor
}

func inspectInterfaces(root, parentName string) ([]int, bool, bool, bool, error) {
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil, false, false, false, err
	}
	var nums []int
	var audio, hid, vendor bool
	prefix := parentName + ":"
	for _, entry := range entries {
		if !strings.HasPrefix(entry.Name(), prefix) {
			continue
		}
		dir := filepath.Join(root, entry.Name())
		numberHex := readTrimmed(filepath.Join(dir, "bInterfaceNumber"))
		number, err := strconv.ParseInt(numberHex, 16, 32)
		if err != nil {
			return nil, false, false, false, fmt.Errorf("hollyland: malformed interface number %q", numberHex)
		}
		nums = append(nums, int(number))
		class := strings.ToLower(readTrimmed(filepath.Join(dir, "bInterfaceClass")))
		switch class {
		case "01":
			audio = true
		case "03":
			hid = true
		case "ff":
			vendor = true
		}
	}
	sort.Ints(nums)
	return nums, audio, hid, vendor, nil
}

func findInputNode(usbParent, classRoot, devRoot string) string {
	usbReal, err := filepath.EvalSymlinks(usbParent)
	if err != nil {
		usbReal = filepath.Clean(usbParent)
	}
	entries, err := filepath.Glob(filepath.Join(classRoot, "event*"))
	if err != nil {
		return ""
	}
	for _, eventPath := range entries {
		deviceReal, err := filepath.EvalSymlinks(filepath.Join(eventPath, "device"))
		if err != nil {
			continue
		}
		rel, err := filepath.Rel(usbReal, deviceReal)
		if err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return filepath.Join(devRoot, "input", filepath.Base(eventPath))
		}
	}
	return ""
}

func readTrimmed(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(data))
}

func joinInts(values []int) string {
	parts := make([]string, len(values))
	for i, value := range values {
		parts[i] = strconv.Itoa(value)
	}
	return strings.Join(parts, ",")
}
