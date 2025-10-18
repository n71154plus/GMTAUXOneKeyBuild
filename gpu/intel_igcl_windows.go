//go:build windows

package gpu

import (
	"errors"
	"fmt"
	"runtime"
	"sync"
)

// intelIGCLDriver provides an adapter implementation that mirrors the
// Intel Graphics Control Library behaviour. The public surface matches the
// Driver interface so it can be registered alongside the existing COM based
// driver.
type intelIGCLDriver struct {
	ctrl *IntelIGCL
	mu   sync.Mutex
}

func init() {
	registerProviderNamed("intel-igcl", newIntelIGCLDriver)
}

func newIntelIGCLDriver() (Driver, error) {
	ctrl, err := NewIntelIGCL()
	if err != nil {
		if errors.Is(err, errIntelUnavailable) {
			return nil, ErrNoDriver
		}
		return nil, err
	}

	_, _, err = findIntelDisplay(ctrl.IntelCUI)
	if err != nil {
		ctrl.Close()
		if errors.Is(err, errIntelNoDisplay) {
			return nil, ErrNoDriver
		}
		return nil, err
	}

	d := &intelIGCLDriver{ctrl: ctrl}
	runtime.SetFinalizer(d, func(driver *intelIGCLDriver) {
		driver.ctrl.Close()
	})
	return d, nil
}

func (d *intelIGCLDriver) Name() string {
	return "Intel Graphics Control Library"
}

func (d *intelIGCLDriver) ReadDPCD(addr uint32, length uint32) ([]byte, error) {
	if length == 0 {
		return nil, fmt.Errorf("dpcd read length must be greater than zero")
	}

	const maxChunk = uint32(16)
	remaining := length
	offset := addr
	result := make([]byte, 0, length)

	d.mu.Lock()
	defer d.mu.Unlock()

	for remaining > 0 {
		chunk := remaining
		if chunk > maxChunk {
			chunk = maxChunk
		}
		data, err := d.ctrl.ReadDPCD(offset, chunk)
		if err != nil {
			return nil, err
		}
		result = append(result, data...)
		offset += chunk
		remaining -= chunk
	}
	return result, nil
}

func (d *intelIGCLDriver) WriteDPCD(addr uint32, data []byte) error {
	if len(data) == 0 {
		return nil
	}

	const maxChunk = 16
	offset := addr
	remaining := data

	d.mu.Lock()
	defer d.mu.Unlock()

	for len(remaining) > 0 {
		chunk := remaining
		if len(chunk) > maxChunk {
			chunk = chunk[:maxChunk]
		}
		if err := d.ctrl.WriteDPCD(offset, chunk); err != nil {
			return err
		}
		offset += uint32(len(chunk))
		remaining = remaining[len(chunk):]
	}
	return nil
}

func (d *intelIGCLDriver) ReadI2C(addr uint32, length uint32) ([]byte, error) {
	if length == 0 {
		return []byte{}, nil
	}

	slave, reg := decodeI2CAddress(addr)

	d.mu.Lock()
	defer d.mu.Unlock()

	return d.ctrl.I2CRead(slave, reg, int(length))
}

func (d *intelIGCLDriver) WriteI2C(addr uint32, data []byte) error {
	if len(data) == 0 {
		return nil
	}

	if len(data) > 1 {
		return ErrNotImplemented
	}

	slave, _ := decodeI2CAddress(addr)

	d.mu.Lock()
	defer d.mu.Unlock()

	return d.ctrl.I2CWrite(slave, data[0])
}

// IntelIGCL is a thin adapter that currently re-uses the IntelCUI implementation.
// This allows callers to explicitly select the IGCL provider while sharing the
// established AUX and I2C routines.
type IntelIGCL struct {
	*IntelCUI
}

// NewIntelIGCL creates a new IGCL control instance.
func NewIntelIGCL() (*IntelIGCL, error) {
	cui, err := NewIntelCUI()
	if err != nil {
		return nil, err
	}
	return &IntelIGCL{IntelCUI: cui}, nil
}
