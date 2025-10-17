//go:build !windows

package edidhelper

import (
	display "GMTAUXOneKeyBuild/struct"
	"errors"
)

// errUnsupported 說明此功能僅支援在 Windows 平台上列舉顯示器。
var errUnsupported = errors.New("display enumeration is only supported on Windows")

// GetScreens 在非 Windows 平台上僅回傳錯誤，提示使用者不受支援。
func GetScreens() ([]*display.Display, error) {
	return nil, errUnsupported
}
