package gpu

import (
	"errors"
	"strings"
	"sync"
)

var (
	// ErrNoDriver 表示找不到任何可用的驅動程式。
	ErrNoDriver = errors.New("gpu: no compatible driver found")
	// ErrNotImplemented 表示目前驅動尚未實作該操作。
	ErrNotImplemented = errors.New("gpu: operation not implemented")
)

// Driver 介面定義與 GPU 通訊所需的方法。
type Driver interface {
	Name() string
	ReadDPCD(addr uint32, length uint32) ([]byte, error)
	WriteDPCD(addr uint32, data []byte) error
	ReadI2C(addr uint32, length uint32) ([]byte, error)
	WriteI2C(addr uint32, data []byte) error
}

// providerFunc 為動態註冊驅動供應者的工廠函式定義。
type providerFunc func() (Driver, error)

// providerEntry 儲存驅動名稱與對應的建構函式。
type providerEntry struct {
	name string
	fn   providerFunc
}

var (
	providersMu sync.RWMutex
	providers   []providerEntry
)

// registerProvider 以匿名名稱註冊新的驅動供應者。
func registerProvider(fn providerFunc) {
	registerProviderNamed("", fn)
}

// registerProviderNamed 以特定名稱註冊新的驅動供應者。
func registerProviderNamed(name string, fn providerFunc) {
	providersMu.Lock()
	defer providersMu.Unlock()
	providers = append(providers, providerEntry{name: strings.ToLower(name), fn: fn})
}

// Detect 依序呼叫所有註冊供應者，回傳第一個成功建立的驅動。
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
			// ErrNoDriver 表示該供應者不適用，持續嘗試其他供應者。
			continue
		}
		if joined == nil {
			joined = err
		} else {
			joined = errors.Join(joined, err)
		}
	}

	if joined != nil {
		// 若有累積其他錯誤，優先回傳詳細資訊。
		return nil, joined
	}

	return nil, ErrNoDriver
}

// DetectByName 僅嘗試與指定名稱相符的驅動供應者。
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
