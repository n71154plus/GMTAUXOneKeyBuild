//go:build !windows

package edidhelper

import (
	display "GMTAUXOneKeyBuild/struct"
	"errors"
)

var errUnsupported = errors.New("display enumeration is only supported on Windows")

// GetScreens returns an error on non-Windows platforms.
func GetScreens() ([]*display.Display, error) {
	return nil, errUnsupported
}
