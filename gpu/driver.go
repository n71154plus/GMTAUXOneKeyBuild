package gpu

import (
	"errors"
	"strings"
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

type providerEntry struct {
	name string
	fn   providerFunc
}

var (
	providersMu sync.RWMutex
	providers   []providerEntry
)

func registerProvider(fn providerFunc) {
	registerProviderNamed("", fn)
}

func registerProviderNamed(name string, fn providerFunc) {
	providersMu.Lock()
	defer providersMu.Unlock()
	providers = append(providers, providerEntry{name: strings.ToLower(name), fn: fn})
}

func Detect() (Driver, error) {
	providersMu.RLock()
	list := append([]providerEntry(nil), providers...)
	providersMu.RUnlock()

	if len(list) == 0 {
		return nil, ErrNoDriver
	}

	var joined error
	for _, entry := range list {
		driver, err := entry.fn()
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

func DetectByName(name string) (Driver, error) {
	providersMu.RLock()
	list := append([]providerEntry(nil), providers...)
	providersMu.RUnlock()

	if name == "" {
		return Detect()
	}

	target := strings.ToLower(name)
	var joined error
	for _, entry := range list {
		if entry.name == "" || entry.name != target {
			continue
		}
		driver, err := entry.fn()
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
