//go:build windows

package gpu

import (
	"errors"
	"fmt"
	"runtime"
	"sync"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

const (
	clsctxInprocServer  = 0x1
	clsctxInprocHandler = 0x2
	clsctxLocalServer   = 0x4
	clsctxRemoteServer  = 0x10
	clsctxAll           = clsctxInprocServer | clsctxInprocHandler | clsctxLocalServer | clsctxRemoteServer
)

const (
	displayDeviceActive = 0x1
)

var (
	errIntelUnavailable = errors.New("intel igfx: interface not available")
	errIntelNoDisplay   = errors.New("intel igfx: no active display")
)

var (
	modOle32  = windows.NewLazySystemDLL("ole32.dll")
	modOleAut = windows.NewLazySystemDLL("oleaut32.dll")

	procCoInitialize     = modOle32.NewProc("CoInitialize")
	procCLSIDFromProgID  = modOle32.NewProc("CLSIDFromProgID")
	procCoCreateInstance = modOle32.NewProc("CoCreateInstance")

	procSysAllocString = modOleAut.NewProc("SysAllocString")
	procSysFreeString  = modOleAut.NewProc("SysFreeString")
)

var (
	loadOnce sync.Once
	loadErr  error
)

var (
	user32                  = syscall.NewLazyDLL("user32.dll")
	procEnumDisplayDevicesW = user32.NewProc("EnumDisplayDevicesW")
)

// IID for ICUIExternalX: {F932C038-6484-45CA-8FA1-7C8C279F7AEE}.
var iidICUIExternalX = windows.GUID{
	Data1: 0xF932C038, Data2: 0x6484, Data3: 0x45CA,
	Data4: [8]byte{0x8F, 0xA1, 0x7C, 0x8C, 0x27, 0x9F, 0x7A, 0xEE},
}

// Static blob extracted from igfx. Replace with a complete copy if available.
var igfxAuxBlob = [0x94]byte{
	0x6C, 0x81, 0xB9, 0xBF, 0xB0, 0xAE, 0x4B, 0x43,
	0x99, 0xF3, 0x0F, 0x94, 0xE6, 0xBE, 0xBF, 0x0D,
}

type intelDriver struct {
	cui *IntelCUI
	mu  sync.Mutex
}

func init() {
	registerProviderNamed("intel", newIntelDriver)
}

func newIntelDriver() (Driver, error) {
	cui, err := NewIntelCUI()
	if err != nil {
		if errors.Is(err, errIntelUnavailable) {
			return nil, ErrNoDriver
		}
		return nil, err
	}

	_, _, err := findIntelDisplay(cui)
	if err != nil {
		cui.Close()
		if errors.Is(err, errIntelNoDisplay) {
			return nil, ErrNoDriver
		}
		return nil, err
	}

	d := &intelDriver{cui: cui}
	runtime.SetFinalizer(d, func(driver *intelDriver) {
		driver.cui.Close()
	})
	return d, nil
}

func (d *intelDriver) Name() string {
	return "Intel Graphics Command Center"
}

func (d *intelDriver) ReadDPCD(addr uint32, length uint32) ([]byte, error) {
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
		data, err := d.cui.ReadDPCD(offset, chunk)
		if err != nil {
			return nil, err
		}
		result = append(result, data...)
		offset += chunk
		remaining -= chunk
	}
	return result, nil
}

func (d *intelDriver) WriteDPCD(addr uint32, data []byte) error {
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
		if err := d.cui.WriteDPCD(offset, chunk); err != nil {
			return err
		}
		offset += uint32(len(chunk))
		remaining = remaining[len(chunk):]
	}
	return nil
}

func (d *intelDriver) ReadI2C(addr uint32, length uint32) ([]byte, error) {
	if length == 0 {
		return []byte{}, nil
	}

	slave, reg := decodeI2CAddress(addr)

	d.mu.Lock()
	defer d.mu.Unlock()

	return d.cui.I2CRead(slave, reg, int(length))
}

func (d *intelDriver) WriteI2C(addr uint32, data []byte) error {
	if len(data) == 0 {
		return nil
	}

	if len(data) > 1 {
		return ErrNotImplemented
	}

	slave, _ := decodeI2CAddress(addr)

	d.mu.Lock()
	defer d.mu.Unlock()

	return d.cui.I2CWrite(slave, data[0])
}

// ===== IntelCUI implementation =====

type IUnknown struct{}

type IntelCUI struct {
	obj     *IUnknown
	display int32
	delayMS uint16
}

func NewIntelCUI() (*IntelCUI, error) {
	if err := ensureLoaded(); err != nil {
		return nil, err
	}
	_ = CoInitialize()

	var clsid windows.GUID
	if hr := CLSIDFromProgID(utf16Ptr("Igfxext.CUIExternal"), &clsid); FAILED(hr) {
		if uint32(hr) == 0x80040154 {
			return nil, errIntelUnavailable
		}
		return nil, fmt.Errorf("CLSIDFromProgID failed: 0x%08X", uint32(hr))
	}

	var ifPtr unsafe.Pointer
	if hr := CoCreateInstance(&clsid, nil, clsctxAll, &iidICUIExternalX, &ifPtr); FAILED(hr) || ifPtr == nil {
		if uint32(hr) == 0x80040154 {
			return nil, errIntelUnavailable
		}
		return nil, fmt.Errorf("CoCreateInstance failed: 0x%08X", uint32(hr))
	}
	return &IntelCUI{obj: (*IUnknown)(ifPtr), delayMS: 20}, nil
}

func (c *IntelCUI) Close() {
	if c == nil || c.obj == nil {
		return
	}
	vtbl := *(**uintptr)(unsafe.Pointer(c.obj))
	if vtbl != nil {
		release := *(*uintptr)(unsafe.Add(unsafe.Pointer(vtbl), 2*unsafe.Sizeof(uintptr(0))))
		_, _, _ = syscall.SyscallN(release, uintptr(unsafe.Pointer(c.obj)))
	}
	c.obj = nil
}

func (c *IntelCUI) AcquireDisplay(name string, outputIndex uint32) error {
	if c.obj == nil {
		return fmt.Errorf("intel igfx: COM object is nil")
	}
	bstr := SysAllocString(utf16Ptr(name))
	defer SysFreeString(bstr)

	fp, err := c.getSlot(12)
	if err != nil {
		return err
	}

	var disp int32
	status := int32(0x20000000)
	var code int32

	r1, _, _ := syscall.SyscallN(
		fp,
		uintptr(unsafe.Pointer(c.obj)),
		bstr,
		uintptr(outputIndex),
		uintptr(unsafe.Pointer(&disp)),
		uintptr(unsafe.Pointer(&status)),
		uintptr(unsafe.Pointer(&code)),
	)
	hr := int32(r1)
	if FAILED(hr) {
		if uint32(hr) == 0x80070002 {
			return errIntelNoDisplay
		}
		return fmt.Errorf("AcquireDisplay failed: 0x%08X", uint32(hr))
	}
	c.display = disp
	return nil
}

func (c *IntelCUI) SetDelay(ms uint16) {
	c.delayMS = ms
}

func (c *IntelCUI) ReadDPCD(offset uint32, length uint32) ([]byte, error) {
	if c.display == 0 {
		return nil, fmt.Errorf("intel igfx: display not acquired")
	}
	if length == 0 || length > 16 {
		return nil, fmt.Errorf("intel igfx: invalid length %d", length)
	}

	type ioRead struct {
		Display    int32
		StatusByte int32
		ReqLength  int32
		ReqOffset  int32
		Buf        [0x84]byte
	}

	io := ioRead{
		Display:    c.display,
		StatusByte: 9,
		ReqLength:  int32(length),
		ReqOffset:  int32(offset),
	}

	fp, err := c.getSlot(43)
	if err != nil {
		return nil, err
	}

	var devErr int32
	r1, _, _ := syscall.SyscallN(
		fp,
		uintptr(unsafe.Pointer(c.obj)),
		uintptr(unsafe.Pointer(&igfxAuxBlob[0])),
		uintptr(len(igfxAuxBlob)),
		uintptr(unsafe.Pointer(&io)),
		uintptr(unsafe.Pointer(&devErr)),
	)
	hr := int32(r1)
	if FAILED(hr) || devErr != 0 {
		return nil, c.auxErr("ReadDPCD", hr, devErr)
	}
	if io.StatusByte != 9 {
		return nil, fmt.Errorf("intel igfx: unexpected status byte %d", io.StatusByte)
	}

	buf := make([]byte, length)
	copy(buf, io.Buf[:length])
	return buf, nil
}

func (c *IntelCUI) WriteDPCD(offset uint32, data []byte) error {
	if c.display == 0 {
		return fmt.Errorf("intel igfx: display not acquired")
	}
	if len(data) == 0 || len(data) > 16 {
		return fmt.Errorf("intel igfx: invalid payload size %d", len(data))
	}

	type ioWrite struct {
		Display int32
		Op      int32
		Len     int32
		Addr    int32
		Buf     [0x84]byte
	}

	var io ioWrite
	io.Display = c.display
	io.Op = 8
	io.Len = int32(len(data))
	io.Addr = int32(offset)
	copy(io.Buf[:], data)

	fp, err := c.getSlot(44)
	if err != nil {
		return err
	}

	var devErr int32
	r1, _, _ := syscall.SyscallN(
		fp,
		uintptr(unsafe.Pointer(c.obj)),
		uintptr(unsafe.Pointer(&igfxAuxBlob[0])),
		uintptr(len(igfxAuxBlob)),
		uintptr(unsafe.Pointer(&io)),
		uintptr(unsafe.Pointer(&devErr)),
	)
	hr := int32(r1)
	if FAILED(hr) || devErr != 0 {
		return c.auxErr("WriteDPCD", hr, devErr)
	}
	return nil
}

func (c *IntelCUI) I2CRead(slave7bit byte, reg int, length int) ([]byte, error) {
	if c.display == 0 {
		return nil, fmt.Errorf("intel igfx: display not acquired")
	}
	if length <= 0 {
		return []byte{}, nil
	}

	const maxChunk = 16
	remaining := length
	start := reg
	result := make([]byte, 0, length)

	for remaining > 0 {
		chunk := remaining
		if chunk > maxChunk {
			chunk = maxChunk
		}
		if err := c.i2cWriteSetup(slave7bit, byte(start)); err != nil {
			return nil, err
		}
		remaining -= chunk
		start += chunk
		chunkData, err := c.i2cReadChunk(slave7bit, chunk, remaining == 0)
		if err != nil {
			return nil, err
		}
		result = append(result, chunkData...)
	}
	return result, nil
}

func (c *IntelCUI) I2CWrite(slave7bit byte, value byte) error {
	if c.display == 0 {
		return fmt.Errorf("intel igfx: display not acquired")
	}

	type ioWrite struct {
		Display int32
		Op      int32
		Len     int32
		Addr    int32
		Buf     [0x84]byte
	}

	var io ioWrite
	io.Display = c.display
	io.Op = 0
	io.Len = 1
	io.Addr = int32(2 * uint32(slave7bit))
	io.Buf[0] = value

	fp, err := c.getSlot(44)
	if err != nil {
		return err
	}

	var devErr int32
	r1, _, _ := syscall.SyscallN(
		fp,
		uintptr(unsafe.Pointer(c.obj)),
		uintptr(unsafe.Pointer(&igfxAuxBlob[0])),
		uintptr(len(igfxAuxBlob)),
		uintptr(unsafe.Pointer(&io)),
		uintptr(unsafe.Pointer(&devErr)),
	)
	hr := int32(r1)
	if FAILED(hr) || devErr != 0 {
		return c.auxErr("I2CWrite", hr, devErr)
	}
	if c.delayMS != 0 {
		time.Sleep(time.Duration(c.delayMS) * time.Millisecond)
	}
	return nil
}

func (c *IntelCUI) getSlot(slot int) (uintptr, error) {
	if c.obj == nil {
		return 0, fmt.Errorf("intel igfx: nil COM object")
	}
	vtbl := *(**uintptr)(unsafe.Pointer(c.obj))
	if vtbl == nil {
		return 0, fmt.Errorf("intel igfx: nil vtable")
	}
	fn := *(*uintptr)(unsafe.Add(unsafe.Pointer(vtbl), uintptr(slot)*unsafe.Sizeof(uintptr(0))))
	if fn == 0 {
		return 0, fmt.Errorf("intel igfx: vtable slot %d is null", slot)
	}
	return fn, nil
}

func (c *IntelCUI) auxErr(op string, hr int32, code int32) error {
	var msg string
	switch code {
	case 67:
		msg = "Invalid AUX device"
	case 68:
		msg = "Invalid AUX address"
	case 69:
		msg = "Invalid AUX data size"
	case 70:
		msg = "AUX defer"
	case 71:
		msg = "AUX timeout"
	case 0:
		// leave empty
	default:
		msg = fmt.Sprintf("AUX unknown error (%d)", code)
	}
	if FAILED(hr) {
		if msg == "" {
			msg = "AUX call failed"
		}
		msg = fmt.Sprintf("%s; hr=0x%08X", msg, uint32(hr))
	}
	if op != "" {
		msg = op + ": " + msg
	}
	if msg == "" {
		msg = "intel igfx: unexpected AUX error"
	}
	return fmt.Errorf(msg)
}

func (c *IntelCUI) i2cWriteSetup(slave7bit byte, reg byte) error {
	type ioWrite struct {
		Display int32
		Op      int32
		Len     int32
		Addr    int32
		Buf     [0x84]byte
	}

	var io ioWrite
	io.Display = c.display
	io.Op = 0
	io.Len = 1
	io.Addr = int32(2 * uint32(slave7bit))
	io.Buf[0] = reg

	fp, err := c.getSlot(44)
	if err != nil {
		return err
	}

	var devErr int32
	r1, _, _ := syscall.SyscallN(
		fp,
		uintptr(unsafe.Pointer(c.obj)),
		uintptr(unsafe.Pointer(&igfxAuxBlob[0])),
		uintptr(len(igfxAuxBlob)),
		uintptr(unsafe.Pointer(&io)),
		uintptr(unsafe.Pointer(&devErr)),
	)
	hr := int32(r1)
	if FAILED(hr) || devErr != 0 {
		return c.auxErr("I2C setup", hr, devErr)
	}
	return nil
}

func (c *IntelCUI) i2cReadChunk(slave7bit byte, size int, last bool) ([]byte, error) {
	if size <= 0 || size > 16 {
		return nil, fmt.Errorf("intel igfx: invalid chunk size %d", size)
	}

	type ioRead struct {
		Display int32
		Op      int32
		Len     int32
		Addr    int32
		Buf     [0x84]byte
	}

	var io ioRead
	io.Display = c.display
	if last {
		io.Op = 1
	} else {
		io.Op = 5
	}
	io.Len = int32(size)
	io.Addr = int32(2 * uint32(slave7bit))

	fp, err := c.getSlot(43)
	if err != nil {
		return nil, err
	}

	var devErr int32
	r1, _, _ := syscall.SyscallN(
		fp,
		uintptr(unsafe.Pointer(c.obj)),
		uintptr(unsafe.Pointer(&igfxAuxBlob[0])),
		uintptr(len(igfxAuxBlob)),
		uintptr(unsafe.Pointer(&io)),
		uintptr(unsafe.Pointer(&devErr)),
	)
	hr := int32(r1)
	if FAILED(hr) || devErr != 0 {
		return nil, c.auxErr("I2C read", hr, devErr)
	}

	buf := make([]byte, size)
	copy(buf, io.Buf[:size])
	return buf, nil
}

// ===== Win32 helpers =====

type displayDevice struct {
	cb           uint32
	DeviceName   [32]uint16
	DeviceString [128]uint16
	StateFlags   uint32
	DeviceID     [128]uint16
	DeviceKey    [128]uint16
}

func findIntelDisplay(cui *IntelCUI) (string, uint32, error) {
	for adapterIndex := uint32(0); ; adapterIndex++ {
		adapter, ok := enumDisplayDevices("", adapterIndex)
		if !ok {
			break
		}
		if adapter.StateFlags&displayDeviceActive == 0 {
			continue
		}
		adapterName := syscall.UTF16ToString(adapter.DeviceName[:])
		if adapterName == "" {
			continue
		}

		for outputIndex := uint32(0); ; outputIndex++ {
			monitor, ok := enumDisplayDevices(adapterName, outputIndex)
			if !ok {
				if outputIndex == 0 {
					// If no monitors were returned, still attempt the default output index.
					if err := cui.AcquireDisplay(adapterName, 0); err == nil {
						return adapterName, 0, nil
					}
				}
				break
			}
			if monitor.StateFlags&displayDeviceActive == 0 {
				continue
			}
			if err := cui.AcquireDisplay(adapterName, outputIndex); err == nil {
				return adapterName, outputIndex, nil
			}
		}
	}
	return "", 0, errIntelNoDisplay
}

func enumDisplayDevices(device string, devNum uint32) (*displayDevice, bool) {
	var dd displayDevice
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

func decodeI2CAddress(addr uint32) (byte, int) {
	slave := byte(addr & 0x7F)
	reg := int(addr >> 8)
	return slave, reg
}

func ensureLoaded() error {
	loadOnce.Do(func() {
		if err := modOle32.Load(); err != nil {
			loadErr = fmt.Errorf("load ole32.dll: %w", err)
			return
		}
		if err := modOleAut.Load(); err != nil {
			loadErr = fmt.Errorf("load oleaut32.dll: %w", err)
			return
		}
		procs := []*windows.LazyProc{
			procCoInitialize, procCLSIDFromProgID, procCoCreateInstance,
			procSysAllocString, procSysFreeString,
		}
		for _, p := range procs {
			if err := p.Find(); err != nil {
				loadErr = err
				return
			}
		}
	})
	return loadErr
}

func CoInitialize() int32 {
	r1, _, _ := procCoInitialize.Call(0)
	return int32(r1)
}

func CLSIDFromProgID(pw *uint16, clsid *windows.GUID) int32 {
	r1, _, _ := procCLSIDFromProgID.Call(uintptr(unsafe.Pointer(pw)), uintptr(unsafe.Pointer(clsid)))
	return int32(r1)
}

func CoCreateInstance(clsid *windows.GUID, outer unsafe.Pointer, ctx uint32, riid *windows.GUID, ppv *unsafe.Pointer) int32 {
	r1, _, _ := procCoCreateInstance.Call(
		uintptr(unsafe.Pointer(clsid)),
		uintptr(outer),
		uintptr(ctx),
		uintptr(unsafe.Pointer(riid)),
		uintptr(unsafe.Pointer(ppv)),
	)
	return int32(r1)
}

func SysAllocString(pw *uint16) uintptr {
	r1, _, _ := procSysAllocString.Call(uintptr(unsafe.Pointer(pw)))
	return r1
}

func SysFreeString(bstr uintptr) {
	_, _, _ = procSysFreeString.Call(bstr)
}

func utf16Ptr(s string) *uint16 {
	p, _ := windows.UTF16PtrFromString(s)
	return p
}

func SUCCEEDED(hr int32) bool { return hr >= 0 }
func FAILED(hr int32) bool    { return hr < 0 }
