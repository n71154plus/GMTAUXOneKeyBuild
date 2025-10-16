package edidhelper

import (
	display "GMTAUXOneKeyBuild/struct"
	"fmt"
	"strings"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows/registry"
)

type DISPLAY_DEVICE struct {
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

func EnumDisplayDevices(device string, devNum uint32) (*DISPLAY_DEVICE, bool) {
	var dd DISPLAY_DEVICE
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
func GetScreen() []*display.Display {
	displays := []*display.Display{}
	for i := uint32(0); ; i++ {
		adapter, ok := EnumDisplayDevices("", i)
		if !ok {
			break
		}
		fmt.Printf("%X\n", adapter.cb)
		adapterName := syscall.UTF16ToString(adapter.DeviceName[:])
		adapterString := syscall.UTF16ToString(adapter.DeviceString[:])
		for j := uint32(0); ; j++ {
			monitor, ok := EnumDisplayDevices(adapterName, j)
			if !ok {
				break
			}
			if monitor.StateFlags&0x1 == 0 { // DISPLAY_DEVICE_ACTIVE
				continue
			}
			deviceID := strings.TrimSpace(syscall.UTF16ToString(monitor.DeviceID[:]))
			// 讀 EDID
			edid := readEDIDFromRegistry(deviceID)
			if edid != nil {
				info, err := display.ParseEDID(edid, adapterName, adapterString, deviceID)
				if err != nil {
					break
				}
				displays = append(displays, info)
			} else {
				fmt.Println("    EDID: <無法讀取，請以管理員身份執行>")
			}
		}
	}
	// 簡單等待 Enter
	return displays
}

// 從 DeviceKey 讀 EDID
// 從 Registry DISPLAY 找到對應 DeviceID 的 EDID
func readEDIDFromRegistry(deviceID string) []byte {
	parts := strings.SplitN(deviceID, "\\", 2)
	if len(parts) != 2 {
		return nil
	}
	regPath := `SYSTEM\CurrentControlSet\Enum\DISPLAY`

	key, err := registry.OpenKey(registry.LOCAL_MACHINE, regPath, registry.READ)
	if err != nil {
		return nil
	}
	defer key.Close()

	pnpIDs, _ := key.ReadSubKeyNames(-1)
	for _, pnpID := range pnpIDs {
		instanceKey, err := registry.OpenKey(key, pnpID, registry.READ)
		if err != nil {
			continue
		}
		instances, _ := instanceKey.ReadSubKeyNames(-1)
		for _, inst := range instances {
			attr, err1 := registry.OpenKey(instanceKey, inst, registry.READ)
			if err1 != nil {
				continue
			}
			defer attr.Close()
			driver, _, err2 := attr.GetStringValue("Driver")
			if err2 != nil {
				continue
			}
			if strings.Contains(deviceID, driver) {
				fmt.Printf("\nMONITOR\\%s\\%s   %s  \n", pnpID, driver, deviceID)
				edidKey, err3 := registry.OpenKey(attr, "Device Parameters", registry.READ)
				if err3 != nil {
					continue
				}
				defer edidKey.Close()
				edid, _, err := edidKey.GetBinaryValue("EDID")
				if err == nil && len(edid) > 0 {
					return edid
				}
			}
		}

	}
	return nil
}
