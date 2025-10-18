package gpu

import (
	"errors"
	"sync"
)

var (
	ErrNoDriver       = errors.New("gpu: no compatible driver found")
	ErrNotImplemented = errors.New("gpu: operation not implemented")
)

type Driver interface {
	Name() string
	ReadDPCD(addr uint32, length uint32) ([]byte, error)
	WriteDPCD(addr uint32, data []byte) error
	ReadI2C(addr uint32, length uint32) ([]byte, error)
	WriteI2C(addr uint32, data []byte) error
}

type providerFunc func() (Driver, error)

var (
	providersMu sync.RWMutex
	providers   []providerFunc
)

func registerProvider(fn providerFunc) {
	providersMu.Lock()
	defer providersMu.Unlock()
	providers = append(providers, fn)
}

func Detect() (Driver, error) {
	providersMu.RLock()
	list := append([]providerFunc(nil), providers...)
	providersMu.RUnlock()

	if len(list) == 0 {
		return nil, ErrNoDriver
	}

	var joined error
	for _, fn := range list {
		driver, err := fn()
		if err == nil {
			return driver, nil
		}
		if errors.Is(err, ErrNoDriver) {
			continue
		}
		if joined == nil {
			joined = err
		} else {
			joined = errors.Join(joined, err)
		}
	}

	if joined != nil {
		return nil, joined
	}

	return nil, ErrNoDriver
}
