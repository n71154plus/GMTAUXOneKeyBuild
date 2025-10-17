//go:build windows

package edidhelper

import (
	display "GMTAUXOneKeyBuild/struct"
	"errors"
	"strings"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows/registry"
)

const displayDeviceActive = 0x1

// displayDevice mirrors the DISPLAY_DEVICE structure from the Win32 API.
type displayDevice struct {
	cb           uint32
	DeviceName   [32]uint16
	DeviceString [128]uint16
	StateFlags   uint32
	DeviceID     [128]uint16
	DeviceKey    [128]uint16
}

var (
	user32                  = syscall.NewLazyDLL("user32.dll")
	procEnumDisplayDevicesW = user32.NewProc("EnumDisplayDevicesW")
)

// GetScreens queries the available displays on Windows and parses the EDID
// information for each active monitor. It returns a slice of Display values.
// If some devices fail to produce EDID data the method still returns the
// displays gathered so far alongside the last error encountered.
func GetScreens() ([]*display.Display, error) {
	var (
		displays []*display.Display
		lastErr  error
	)

	for adapterIndex := uint32(0); ; adapterIndex++ {
		adapter, ok := enumDisplayDevices("", adapterIndex)
		if !ok {
			break
		}

		adapterName := syscall.UTF16ToString(adapter.DeviceName[:])
		adapterString := syscall.UTF16ToString(adapter.DeviceString[:])

		for monitorIndex := uint32(0); ; monitorIndex++ {
			monitor, ok := enumDisplayDevices(adapterName, monitorIndex)
			if !ok {
				break
			}
			if monitor.StateFlags&displayDeviceActive == 0 {
				continue
			}

			deviceID := strings.TrimSpace(syscall.UTF16ToString(monitor.DeviceID[:]))
			edid, err := readEDIDFromRegistry(deviceID)
			if err != nil {
				lastErr = err
				continue
			}

			info, err := display.ParseEDID(edid, adapterName, adapterString, deviceID)
			if err != nil {
				lastErr = err
				continue
			}
			displays = append(displays, info)
		}
	}

	if len(displays) == 0 && lastErr != nil {
		return nil, lastErr
	}
	return displays, lastErr
}

func enumDisplayDevices(device string, devNum uint32) (*displayDevice, bool) {
	var dd displayDevice
	dd.cb = uint32(unsafe.Sizeof(dd))

	var devicePtr *uint16
	if device != "" {
		devicePtr, _ = syscall.UTF16PtrFromString(device)
	}

	ret, _, _ := procEnumDisplayDevicesW.Call(
		uintptr(unsafe.Pointer(devicePtr)),
		uintptr(devNum),
		uintptr(unsafe.Pointer(&dd)),
		0,
	)

	return &dd, ret != 0
}

func readEDIDFromRegistry(deviceID string) ([]byte, error) {
	const regPath = `SYSTEM\CurrentControlSet\Enum\DISPLAY`

	rootKey, err := registry.OpenKey(registry.LOCAL_MACHINE, regPath, registry.READ)
	if err != nil {
		return nil, err
	}
	defer rootKey.Close()

	pnpIDs, err := rootKey.ReadSubKeyNames(-1)
	if err != nil {
		return nil, err
	}

	var lastErr error
	for _, pnpID := range pnpIDs {
		instanceKey, err := registry.OpenKey(rootKey, pnpID, registry.READ)
		if err != nil {
			lastErr = err
			continue
		}

		edid, err := readEDIDFromInstance(instanceKey, deviceID)
		instanceKey.Close()

		if err == nil && len(edid) > 0 {
			return edid, nil
		}
		if err != nil {
			lastErr = err
		}
	}

	if lastErr == nil {
		lastErr = errors.New("edid not found in registry")
	}
	return nil, lastErr
}

func readEDIDFromInstance(instanceKey registry.Key, deviceID string) ([]byte, error) {
	instances, err := instanceKey.ReadSubKeyNames(-1)
	if err != nil {
		return nil, err
	}

	var lastErr error
	for _, inst := range instances {
		attrKey, err := registry.OpenKey(instanceKey, inst, registry.READ)
		if err != nil {
			lastErr = err
			continue
		}

		driver, _, err := attrKey.GetStringValue("Driver")
		if err != nil {
			attrKey.Close()
			lastErr = err
			continue
		}

		if !strings.Contains(deviceID, driver) {
			attrKey.Close()
			continue
		}

		edidKey, err := registry.OpenKey(attrKey, "Device Parameters", registry.READ)
		if err != nil {
			attrKey.Close()
			lastErr = err
			continue
		}

		edid, _, err := edidKey.GetBinaryValue("EDID")
		edidKey.Close()
		attrKey.Close()

		if err != nil {
			lastErr = err
			continue
		}
		if len(edid) == 0 {
			lastErr = errors.New("edid data is empty")
			continue
		}
		return edid, nil
	}

	if lastErr == nil {
		lastErr = errors.New("edid not found for device")
	}
	return nil, lastErr
}
