package display

import (
	"encoding/binary"
	"fmt"
	"strings"
)

// Display 用來儲存解析後的 EDID 資訊
type Display struct {
	AdapterName    string
	AdapterString  string
	DeviceID       string
	ManufacturerID string
	ProductID      string
	Serial         string
	Week           int
	Year           int
	Version        string
	Revision       string
	Descriptor1    string
	Descriptor2    string
	Descriptor3    string
	Descriptor4    string
}

// parseManufacturerID 解析製造商ID
func parseManufacturerID(data []byte) string {
	val := binary.BigEndian.Uint16(data)
	c1 := ((val >> 10) & 0x1F) + 'A' - 1
	c2 := ((val >> 5) & 0x1F) + 'A' - 1
	c3 := (val & 0x1F) + 'A' - 1
	return fmt.Sprintf("%c%c%c", c1, c2, c3)
}

// equalBytes 檢查兩byte slice是否相同
func equalBytes(a, b []byte) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// parseDescriptor 解析詳細時脈或監視器描述符
func parseDescriptor(desc []byte) string {
	if len(desc) != 18 {
		return ""
	}

	pixelClock := binary.LittleEndian.Uint16(desc[0:2])
	if pixelClock == 0 {
		// Monitor descriptor
		tag := desc[3]
		switch tag {
		case 0xFC: // Monitor Name
			name := strings.TrimSpace(string(desc[5:18]))
			return fmt.Sprintf("Monitor Name: %s", name)
		case 0xFE: // Text
			text := strings.TrimSpace(string(desc[5:18]))
			return fmt.Sprintf("Text: %s", text)
		case 0xFF: // Serial String
			serial := strings.TrimSpace(string(desc[5:18]))
			return fmt.Sprintf("Monitor Serial: %s", serial)
		default:
			return fmt.Sprintf("Monitor Descriptor (Tag 0x%02X)", tag)
		}
	} else {
		// Detailed Timing Descriptor
		hActive := int(desc[2]) + ((int(desc[4]) & 0xF0) << 4)
		hBlank := int(desc[3]) + ((int(desc[4]) & 0x0F) << 8)
		vActive := int(desc[5]) + ((int(desc[7]) & 0xF0) << 4)
		vBlank := int(desc[6]) + ((int(desc[7]) & 0x0F) << 8)
		hTotal := hActive + hBlank
		vTotal := vActive + vBlank

		refresh := 0.0
		if hTotal != 0 && vTotal != 0 {
			refresh = float64(pixelClock) / float64(hTotal*vTotal/10000)
		}

		return fmt.Sprintf("Detailed Timing: %dx%d @ %.2fHz (PixelClock %.2fMHz)",
			hActive, vActive, refresh, float64(pixelClock)/100.0)
	}
}

// ParseEDID 解析整份EDID並返回 Display 結構
func ParseEDID(edid []byte,
	adapterName string,
	adapterString string,
	deviceID string,
) (*Display, error) {
	if len(edid) < 128 {
		return nil, fmt.Errorf("EDID data too short")
	}

	header := []byte{0x00, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0x00}
	if !equalBytes(edid[0:8], header) {
		return nil, fmt.Errorf("invalid EDID header")
	}

	manuID := parseManufacturerID(edid[0x08:0x0A])
	productID := binary.LittleEndian.Uint16(edid[0x0A:0x0C])
	serial := binary.LittleEndian.Uint32(edid[0x0C:0x10])
	week := int(edid[0x10])
	year := int(edid[0x11]) + 1990
	version := fmt.Sprintf("%d", edid[0x12])
	revision := fmt.Sprintf("%d", edid[0x13])

	// 四個 Descriptor
	offsets := []int{0x36, 0x48, 0x5A, 0x6C}
	descs := make([]string, 4)
	for i, off := range offsets {
		desc := edid[off : off+18]
		descs[i] = parseDescriptor(desc)
	}

	return &Display{
		AdapterName:    adapterName,
		AdapterString:  adapterString,
		DeviceID:       deviceID,
		ManufacturerID: manuID,
		ProductID:      fmt.Sprintf("0x%04X", productID),
		Serial:         fmt.Sprintf("0x%08X", serial),
		Week:           week,
		Year:           year,
		Version:        version,
		Revision:       revision,
		Descriptor1:    descs[0],
		Descriptor2:    descs[1],
		Descriptor3:    descs[2],
		Descriptor4:    descs[3],
	}, nil
}
