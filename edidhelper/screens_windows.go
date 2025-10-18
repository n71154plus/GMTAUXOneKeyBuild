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

// displayDeviceActive 表示裝置目前已啟用的旗標值。
const displayDeviceActive = 0x1

// displayDevice 對應 Win32 API 的 DISPLAY_DEVICE 結構，用來接收列舉結果。
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

// GetScreens 列舉所有啟用中的顯示器並解析 EDID，回傳顯示器資訊清單與最後錯誤。
func GetScreens() ([]*display.Display, error) {
	var (
		displays []*display.Display
		lastErr  error
	)

	for adapterIndex := uint32(0); ; adapterIndex++ {
		// 依序列舉顯示卡，沒有更多資料時結束迴圈。
		adapter, ok := enumDisplayDevices("", adapterIndex)
		if !ok {
			break
		}

		// 將 UTF-16 結果轉換成 Go 字串以利後續使用。
		adapterName := syscall.UTF16ToString(adapter.DeviceName[:])
		adapterString := syscall.UTF16ToString(adapter.DeviceString[:])

		for monitorIndex := uint32(0); ; monitorIndex++ {
			// 針對每張顯示卡列舉所連接的顯示器。
			monitor, ok := enumDisplayDevices(adapterName, monitorIndex)
			if !ok {
				break
			}
			if monitor.StateFlags&displayDeviceActive == 0 {
				// 未啟用的顯示器不需處理。
				continue
			}

			// 轉換顯示器的裝置識別碼並移除前後空白。
			deviceID := strings.TrimSpace(syscall.UTF16ToString(monitor.DeviceID[:]))
			// 從登錄檔讀出對應的 EDID。
			edid, err := readEDIDFromRegistry(deviceID)
			if err != nil {
				lastErr = err
				continue
			}

			// 解析 EDID 內容並加入結果清單。
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
	// 必須指定結構大小，API 才能寫入正確的欄位資料。
	dd.cb = uint32(unsafe.Sizeof(dd))

	var devicePtr *uint16
	if device != "" {
		// 將 Go 字串轉為 UTF-16，供 Win32 API 使用。
		devicePtr, _ = syscall.UTF16PtrFromString(device)
	}

	// 呼叫 Win32 API 取得指定索引的顯示卡或顯示器資訊。
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

	// 列出所有 PnP 裝置代碼以便逐一搜尋符合條件的實例。
	pnpIDs, err := rootKey.ReadSubKeyNames(-1)
	if err != nil {
		return nil, err
	}

	var lastErr error
	for _, pnpID := range pnpIDs {
		// 逐一開啟每個裝置節點並嘗試取得 EDID。
		instanceKey, err := registry.OpenKey(rootKey, pnpID, registry.READ)
		if err != nil {
			lastErr = err
			continue
		}

		edid, err := readEDIDFromInstance(instanceKey, deviceID)
		instanceKey.Close()

		if err == nil && len(edid) > 0 {
			// 一旦找到符合的資料即可回傳，無需再繼續搜尋。
			return edid, nil
		}
		if err != nil {
			lastErr = err
		}
	}

	if lastErr == nil {
		// 若沒有取得任何資料也沒有具體錯誤，回傳預設的找不到訊息。
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
		// 開啟具體的裝置實例節點以查詢驅動名稱。
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

		// 尋找 Device Parameters 子鍵以讀取 EDID 原始資料。
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
		// 沒有符合的實例時回傳統一錯誤訊息。
		lastErr = errors.New("edid not found for device")
	}
	return nil, lastErr
}
