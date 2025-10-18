//go:build windows && amd64

package gpu

import (
	"errors"
	"fmt"
	"runtime"
	"sync"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

// intelIGCLDriver exposes the Intel Control Library (IGCL) AUX and I2C access
// through the generic Driver interface.
type intelIGCLDriver struct {
	ctx *igclContext
	mu  sync.Mutex
}

var (
	errIGCLUnavailable = errors.New("intel igcl: interface not available")
	errIGCLNoDisplay   = errors.New("intel igcl: no display outputs on the adapter")
)

func init() {
	registerProvider(newIntelPreferredDriver)
	registerProviderNamed("intel-igcl", newIntelIGCLDriver)
}

func newIntelPreferredDriver() (Driver, error) {
	var igfxErr error
	if intelIGFXAvailable() {
		// 優先使用 igfx 介面，若成功可直接回傳。
		driver, err := newIntelIGFXDriver()
		if err == nil {
			return driver, nil
		}
		if !errors.Is(err, ErrNoDriver) {
			igfxErr = err
		}
	}

	// 若 igfx 失敗，改用 IGCL 介面嘗試建立驅動。
	driver, err := newIntelIGCLDriver()
	if err == nil {
		return driver, nil
	}
	if errors.Is(err, ErrNoDriver) {
		if igfxErr != nil {
			return nil, igfxErr
		}
		return nil, ErrNoDriver
	}
	if igfxErr != nil {
		return nil, errors.Join(igfxErr, err)
	}
	return nil, err
}

func newIntelIGCLDriver() (Driver, error) {
	ctx, err := newIGCLContext()
	if err != nil {
		if errors.Is(err, errIGCLUnavailable) || errors.Is(err, errIGCLNoDisplay) {
			return nil, ErrNoDriver
		}
		return nil, err
	}

	d := &intelIGCLDriver{ctx: ctx}
	runtime.SetFinalizer(d, func(driver *intelIGCLDriver) {
		// 釋放底層資源避免記憶體洩漏。
		driver.ctx.Close()
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

	const maxChunk = uint32(auxI2CDataCap)
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
		// 單次只能讀取有限長度，因此分批與硬體通訊。
		data, err := d.ctx.ReadDPCD(offset, int(chunk))
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

	const maxChunk = auxI2CDataCap
	offset := addr
	remaining := data

	d.mu.Lock()
	defer d.mu.Unlock()

	for len(remaining) > 0 {
		chunk := remaining
		if len(chunk) > maxChunk {
			chunk = chunk[:maxChunk]
		}
		// 對應讀取的方式，寫入同樣以分段處理。
		if err := d.ctx.WriteDPCD(offset, chunk); err != nil {
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

	const maxChunk = auxI2CDataCap
	slave, reg := decodeI2CAddress(addr)
	remaining := length
	offset := uint32(reg)
	result := make([]byte, 0, length)

	d.mu.Lock()
	defer d.mu.Unlock()

	for remaining > 0 {
		chunk := remaining
		if chunk > uint32(maxChunk) {
			chunk = uint32(maxChunk)
		}
		// I2C 讀取也需遵守資料長度限制。
		data, err := d.ctx.ReadI2C(slave, offset, int(chunk))
		if err != nil {
			return nil, err
		}
		result = append(result, data...)
		remaining -= chunk
		offset += chunk
	}
	return result, nil
}

func (d *intelIGCLDriver) WriteI2C(addr uint32, data []byte) error {
	if len(data) == 0 {
		return nil
	}

	const maxChunk = auxI2CDataCap
	slave, reg := decodeI2CAddress(addr)
	offset := uint32(reg)
	remaining := data

	d.mu.Lock()
	defer d.mu.Unlock()

	for len(remaining) > 0 {
		chunk := remaining
		if len(chunk) > maxChunk {
			chunk = chunk[:maxChunk]
		}
		// 逐段寫入指定的 I2C 裝置。
		if err := d.ctx.WriteI2C(slave, offset, chunk); err != nil {
			return err
		}
		offset += uint32(len(chunk))
		remaining = remaining[len(chunk):]
	}
	return nil
}

/*
========================
IGCL Control context
========================
*/

// CTL API return codes.
const (
	ctlResultSuccess = 0
)

// Operation types for ctl AUX/I2C requests.
const (
	ctlOperationTypeRead  = 0
	ctlOperationTypeWrite = 1
)

// AUX access flags.
const (
	ctlAuxFlagNativeAUX    = 1 << 0
	ctlAuxFlagI2CAUX       = 1 << 1
	ctlAuxFlagI2CAUXMOT    = 1 << 2
	ctlAuxFlagReservedMask = 0
)

// I2C access flags.
const (
	ctlI2CFlag1ByteIndex = 1 << 0
)

// ctl libraries typically support payloads up to 256/512 bytes depending on
// the version. 512 works for recent builds but adjust if necessary.
const auxI2CDataCap = 512

type (
	ctlAPIHandle           = unsafe.Pointer
	ctlDeviceAdapterHandle = unsafe.Pointer
	ctlDisplayOutputHandle = unsafe.Pointer
)

type ctlInitArgs struct {
	Size             uint32
	Version          uint8
	_                [3]byte
	AppVersion       uint32
	Flags            ctlInitFlags
	SupportedVersion uint32
	ApplicationUID   ctlApplicationID
}

type ctlInitFlags uint32

const (
	ctlInitFlagNone ctlInitFlags = 0
)

const (
	ctlInitAppVersion  uint32 = 0x00010001
	ctlInitArgsMinSize        = unsafe.Sizeof(struct {
		Size             uint32
		Version          uint8
		_                [3]byte
		AppVersion       uint32
		Flags            uint32
		SupportedVersion uint32
		ApplicationUID   [16]byte
	}{})
)

type ctlApplicationID [16]byte

type ctlI2CAccessArgs struct {
	Size     uint32
	Version  uint32
	OpType   uint32
	Flags    uint32
	Address  uint32
	Offset   uint32
	DataSize uint32
	_        uint32
	Data     [auxI2CDataCap]byte
}

type ctlAuxAccessArgs struct {
	Size     uint32
	Version  uint8
	_        [3]byte
	OpType   uint32
	Flags    uint32
	Address  uint32
	RAD      uint64
	PortID   uint32
	DataSize uint32
	Data     [auxI2CDataCap]byte
}

var (
	controlLibOnce sync.Once
	controlLibErr  error

	modControlLib *windows.DLL

	procCtlInit                    *windows.Proc
	procCtlClose                   *windows.Proc
	procCtlEnumerateDevices        *windows.Proc
	procCtlEnumerateDisplayOutputs *windows.Proc
	procCtlAUXAccess               *windows.Proc
	procCtlI2CAccess               *windows.Proc
)

func ensureControlLibLoaded() error {
	controlLibOnce.Do(func() {
		controlLibErr = loadControlLibFromSystem32()
	})
	return controlLibErr
}

func loadControlLibFromSystem32() error {
	if modControlLib != nil {
		return nil
	}

	const dllPath = `C:\\Windows\\System32\\ControlLib.dll`

	dll, err := windows.LoadDLL(dllPath)
	if err != nil {
		if errors.Is(err, syscall.ERROR_MOD_NOT_FOUND) || errors.Is(err, syscall.ERROR_FILE_NOT_FOUND) {
			return errIGCLUnavailable
		}
		return fmt.Errorf("loaddll %s: %w", dllPath, err)
	}

	type procEntry struct {
		name string
		dst  **windows.Proc
	}

	procs := []procEntry{
		{"ctlInit", &procCtlInit},
		{"ctlClose", &procCtlClose},
		{"ctlEnumerateDevices", &procCtlEnumerateDevices},
		{"ctlEnumerateDisplayOutputs", &procCtlEnumerateDisplayOutputs},
		{"ctlAUXAccess", &procCtlAUXAccess},
		{"ctlI2CAccess", &procCtlI2CAccess},
	}

	for _, entry := range procs {
		proc, findErr := dll.FindProc(entry.name)
		if findErr != nil {
			dll.Release()
			return findErr
		}
		*entry.dst = proc
	}

	modControlLib = dll
	return nil
}

func ctlInit(args *ctlInitArgs, api *ctlAPIHandle) uint32 {
	r1, _, _ := procCtlInit.Call(
		uintptr(unsafe.Pointer(args)),
		uintptr(unsafe.Pointer(api)),
	)
	return uint32(r1)
}

func ctlClose(api ctlAPIHandle) uint32 {
	r1, _, _ := procCtlClose.Call(uintptr(api))
	return uint32(r1)
}

func ctlEnumerateDevices(api ctlAPIHandle, count *uint32, handles *ctlDeviceAdapterHandle) uint32 {
	r1, _, _ := procCtlEnumerateDevices.Call(
		uintptr(api),
		uintptr(unsafe.Pointer(count)),
		uintptr(unsafe.Pointer(handles)),
	)
	return uint32(r1)
}

func ctlEnumerateDisplayOutputs(dev ctlDeviceAdapterHandle, count *uint32, handles *ctlDisplayOutputHandle) uint32 {
	r1, _, _ := procCtlEnumerateDisplayOutputs.Call(
		uintptr(dev),
		uintptr(unsafe.Pointer(count)),
		uintptr(unsafe.Pointer(handles)),
	)
	return uint32(r1)
}

func ctlAUXAccess(out ctlDisplayOutputHandle, auxArgs *ctlAuxAccessArgs) uint32 {
	r1, _, _ := procCtlAUXAccess.Call(
		uintptr(out),
		uintptr(unsafe.Pointer(auxArgs)),
	)
	return uint32(r1)
}

func ctlI2CAccess(out ctlDisplayOutputHandle, i2cArgs *ctlI2CAccessArgs) uint32 {
	r1, _, _ := procCtlI2CAccess.Call(
		uintptr(out),
		uintptr(unsafe.Pointer(i2cArgs)),
	)
	return uint32(r1)
}

type igclContext struct {
	api    ctlAPIHandle
	device ctlDeviceAdapterHandle
	output ctlDisplayOutputHandle
}

func newIGCLContext() (*igclContext, error) {
	if err := ensureControlLibLoaded(); err != nil {
		return nil, err
	}

	if size := unsafe.Sizeof(ctlInitArgs{}); size < ctlInitArgsMinSize {
		return nil, fmt.Errorf("ctlInitArgs too small; adjust struct definition (size=%d)", size)
	}
	if unsafe.Sizeof(ctlI2CAccessArgs{}) < 64 {
		return nil, errors.New("ctlI2CAccessArgs too small; adjust struct definition or auxI2CDataCap")
	}
	if unsafe.Sizeof(ctlAuxAccessArgs{}) < 64 {
		return nil, errors.New("ctlAuxAccessArgs too small; adjust struct definition or auxI2CDataCap")
	}

	var api ctlAPIHandle
	initArgs := ctlInitArgs{
		Size:       uint32(unsafe.Sizeof(ctlInitArgs{})),
		Version:    0,
		AppVersion: ctlInitAppVersion,
		Flags:      ctlInitFlagNone,
	}
	if r := ctlInit(&initArgs, &api); r != ctlResultSuccess {
		return nil, fmt.Errorf("ctlInit failed: 0x%08x", r)
	}

	ctx := &igclContext{api: api}

	var devCount uint32
	if r := ctlEnumerateDevices(ctx.api, &devCount, nil); r != ctlResultSuccess {
		ctx.Close()
		return nil, fmt.Errorf("ctlEnumerateDevices(count) failed: 0x%08x", r)
	}
	if devCount == 0 {
		ctx.Close()
		return nil, errIGCLUnavailable
	}

	devs := make([]ctlDeviceAdapterHandle, devCount)
	if r := ctlEnumerateDevices(ctx.api, &devCount, &devs[0]); r != ctlResultSuccess {
		ctx.Close()
		return nil, fmt.Errorf("ctlEnumerateDevices(get) failed: 0x%08x", r)
	}
	ctx.device = devs[0]

	var outCount uint32
	if r := ctlEnumerateDisplayOutputs(ctx.device, &outCount, nil); r != ctlResultSuccess {
		ctx.Close()
		return nil, fmt.Errorf("ctlEnumerateDisplayOutputs(count) failed: 0x%08x", r)
	}
	if outCount == 0 {
		ctx.Close()
		return nil, errIGCLNoDisplay
	}

	outs := make([]ctlDisplayOutputHandle, outCount)
	if r := ctlEnumerateDisplayOutputs(ctx.device, &outCount, &outs[0]); r != ctlResultSuccess {
		ctx.Close()
		return nil, fmt.Errorf("ctlEnumerateDisplayOutputs(get) failed: 0x%08x", r)
	}
	ctx.output = outs[0]
	return ctx, nil
}

func (c *igclContext) Close() {
	if c == nil || c.api == nil {
		return
	}
	_ = ctlClose(c.api)
	c.api = nil
}

func (c *igclContext) ReadDPCD(addr uint32, n int) ([]byte, error) {
	if n <= 0 || n > auxI2CDataCap {
		return nil, fmt.Errorf("invalid dpcd length %d (1..%d)", n, auxI2CDataCap)
	}

	var args ctlAuxAccessArgs
	args.Size = uint32(unsafe.Sizeof(args))
	args.Version = 1
	args.OpType = ctlOperationTypeRead
	args.Flags = ctlAuxFlagNativeAUX
	args.Address = addr
	args.DataSize = uint32(n)

	if r := ctlAUXAccess(c.output, &args); r != ctlResultSuccess {
		return nil, fmt.Errorf("ctlAUXAccess(read) failed: 0x%08x", r)
	}
	out := make([]byte, n)
	copy(out, args.Data[:n])
	return out, nil
}

func (c *igclContext) WriteDPCD(addr uint32, data []byte) error {
	if len(data) == 0 || len(data) > auxI2CDataCap {
		return fmt.Errorf("invalid dpcd payload %d (1..%d)", len(data), auxI2CDataCap)
	}

	var args ctlAuxAccessArgs
	args.Size = uint32(unsafe.Sizeof(args))
	args.Version = 1
	args.OpType = ctlOperationTypeWrite
	args.Flags = ctlAuxFlagNativeAUX
	args.Address = addr
	args.DataSize = uint32(len(data))
	copy(args.Data[:], data)

	if r := ctlAUXAccess(c.output, &args); r != ctlResultSuccess {
		return fmt.Errorf("ctlAUXAccess(write) failed: 0x%08x", r)
	}
	return nil
}

func (c *igclContext) ReadI2C(slave7bit byte, offset uint32, n int) ([]byte, error) {
	if n <= 0 || n > auxI2CDataCap {
		return nil, fmt.Errorf("invalid i2c length %d (1..%d)", n, auxI2CDataCap)
	}

	var args ctlI2CAccessArgs
	args.Size = uint32(unsafe.Sizeof(args))
	args.Version = 1
	args.OpType = ctlOperationTypeRead
	args.Flags = ctlI2CFlag1ByteIndex
	args.Address = uint32(slave7bit)
	args.Offset = offset
	args.DataSize = uint32(n)

	if r := ctlI2CAccess(c.output, &args); r != ctlResultSuccess {
		return nil, fmt.Errorf("ctlI2CAccess(read) failed: 0x%08x", r)
	}
	out := make([]byte, n)
	copy(out, args.Data[:n])
	return out, nil
}

func (c *igclContext) WriteI2C(slave7bit byte, offset uint32, data []byte) error {
	if len(data) == 0 || len(data) > auxI2CDataCap {
		return fmt.Errorf("invalid i2c payload %d (1..%d)", len(data), auxI2CDataCap)
	}

	var args ctlI2CAccessArgs
	args.Size = uint32(unsafe.Sizeof(args))
	args.Version = 1
	args.OpType = ctlOperationTypeWrite
	args.Flags = ctlI2CFlag1ByteIndex
	args.Address = uint32(slave7bit)
	args.Offset = offset
	args.DataSize = uint32(len(data))
	copy(args.Data[:], data)

	if r := ctlI2CAccess(c.output, &args); r != ctlResultSuccess {
		return fmt.Errorf("ctlI2CAccess(write) failed: 0x%08x", r)
	}
	return nil
}
