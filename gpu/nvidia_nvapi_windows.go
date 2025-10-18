//go:build windows

package gpu

import (
	"bytes"
	"errors"
	"fmt"
	"strings"
	"sync"
	"syscall"
	"unsafe"
)

const (
	nvapiStatusOK             = 0x00000000
	nvapiStatusEndEnumeration = 0xFFFFFFF9
	nvapiDpAuxTimeout         = 0x000000FF

	qiInit                         = 0x000000000150E828
	qiEnumPhysicalGPUs             = 0x00000000E5AC921F
	qiEnumNvidiaDisplayHandle      = 0x000000009ABDD40D
	qiGetAssociatedDisplayOutputID = 0x00000000D995937E
	qiGetDisplayPortInfo           = 0x00000000C64FF367
	qiGetErrorMessage              = 0x000000006C2D048C
	qiDispDpAuxChannelControl      = 0x000000008EB56969
)

const (
	nvDPInfoV1Size    = 44
	nvDPInfoV1Version = 0x10000 | nvDPInfoV1Size

	nvDpAuxParamsV1Version = 0x00010028
)

const (
	dpAuxOpWriteDPCD = 0
	dpAuxOpReadDPCD  = 1
	dpAuxMaxPayload  = 16
)

var (
	errNoActiveDisplayPort = errors.New("nvapi: no active displayport output")
	errNoPhysicalGPU       = errors.New("nvapi: no physical gpu detected")
)

type nvapiProcs struct {
	dll    *syscall.LazyDLL
	query  *syscall.LazyProc
	init   uintptr
	enumGP uintptr
	enumDH uintptr
	getOut uintptr
	getDP  uintptr
	getErr uintptr
	dpAux  uintptr
}

type nvDPInfoV1 struct {
	Version   uint32
	Reserved0 [36]byte
	Flags     byte
	Pad       [3]byte
}

type nvDpAuxParamsV1 struct {
	Version   uint32
	OutputID  uint32
	Op        uint32
	Address   uint32
	Buf       [16]byte
	LenMinus1 uint32
	Status    int32
	DataLo    uint64
	DataHi    uint64
	Reserved1 [48]byte
}

type nvapiDriver struct {
	procs         *nvapiProcs
	displayHandle uintptr
	outputID      uint32
	mu            sync.Mutex
}

func init() {
	registerProviderNamed("nvidia", newNVAPIDriver)
}

func newNVAPIDriver() (Driver, error) {
	procs, err := loadNvapiProcs()
	if err != nil {
		switch {
		case errors.Is(err, ErrNoDriver):
			return nil, ErrNoDriver
		case errors.Is(err, syscall.ERROR_MOD_NOT_FOUND), errors.Is(err, syscall.ERROR_PROC_NOT_FOUND):
			return nil, ErrNoDriver
		case errors.Is(err, errNoPhysicalGPU), errors.Is(err, errNoActiveDisplayPort):
			return nil, ErrNoDriver
		default:
			return nil, err
		}
	}

	if _, err := procs.enumPhysicalGPUs(); err != nil {
		if errors.Is(err, errNoPhysicalGPU) {
			return nil, ErrNoDriver
		}
		return nil, err
	}

	handle, outputID, err := procs.findActiveDisplayPort()
	if err != nil {
		if errors.Is(err, errNoActiveDisplayPort) {
			return nil, ErrNoDriver
		}
		return nil, err
	}

	return &nvapiDriver{procs: procs, displayHandle: handle, outputID: outputID}, nil
}

func loadNvapiProcs() (*nvapiProcs, error) {
	dll := syscall.NewLazyDLL("nvapi64.dll")
	if err := dll.Load(); err != nil {
		return nil, fmt.Errorf("nvapi: failed to load library: %w", err)
	}

	query := dll.NewProc("nvapi_QueryInterface")
	get := func(id uintptr) uintptr {
		// 透過 QueryInterface 取得各功能的函式指標。
		r, _, _ := query.Call(id)
		return r
	}

	procs := &nvapiProcs{
		dll:    dll,
		query:  query,
		init:   get(qiInit),
		enumGP: get(qiEnumPhysicalGPUs),
		enumDH: get(qiEnumNvidiaDisplayHandle),
		getOut: get(qiGetAssociatedDisplayOutputID),
		getDP:  get(qiGetDisplayPortInfo),
		getErr: get(qiGetErrorMessage),
		dpAux:  get(qiDispDpAuxChannelControl),
	}

	if procs.init == 0 || procs.enumGP == 0 || procs.enumDH == 0 ||
		procs.getOut == 0 || procs.getDP == 0 || procs.dpAux == 0 {
		return nil, ErrNoDriver
	}

	if status, _ := call0(procs.init); uint32(status) != nvapiStatusOK {
		return nil, procs.statusError(uint32(status), "NvAPI_Initialize")
	}

	return procs, nil
}

func (p *nvapiProcs) enumPhysicalGPUs() ([]uintptr, error) {
	const maxGPUs = 64
	handles := make([]uintptr, maxGPUs)
	var count int32
	status, _ := call2(p.enumGP, uintptr(unsafe.Pointer(&handles[0])), uintptr(unsafe.Pointer(&count)))
	if uint32(status) != nvapiStatusOK {
		return nil, p.statusError(uint32(status), "NvAPI_EnumPhysicalGPUs")
	}
	if count <= 0 {
		return nil, errNoPhysicalGPU
	}
	return handles[:count], nil
}

func (p *nvapiProcs) findActiveDisplayPort() (uintptr, uint32, error) {
	handles, err := p.enumDisplayHandles()
	if err != nil {
		return 0, 0, err
	}
	for _, handle := range handles {
		outID, err := p.associatedOutputID(handle)
		if err != nil {
			continue
		}
		info, err := p.displayPortInfo(handle, outID)
		if err != nil {
			continue
		}
		if info.Flags&1 != 0 {
			// Flags 的最低位代表輸出埠是否處於啟用狀態。
			return handle, outID, nil
		}
	}
	return 0, 0, errNoActiveDisplayPort
}

func (p *nvapiProcs) enumDisplayHandles() ([]uintptr, error) {
	handles := make([]uintptr, 0, 8)
	for index := uint32(0); ; index++ {
		var handle uintptr
		status, _ := call2(p.enumDH, uintptr(index), uintptr(unsafe.Pointer(&handle)))
		switch uint32(status) {
		case nvapiStatusOK:
			// 成功取得顯示器控制代碼，加入清單。
			handles = append(handles, handle)
		case nvapiStatusEndEnumeration:
			// 到達列舉結尾時中斷迴圈。
			return handles, nil
		default:
			return nil, p.statusError(uint32(status), "NvAPI_EnumNvidiaDisplayHandle")
		}
	}
}

func (p *nvapiProcs) associatedOutputID(handle uintptr) (uint32, error) {
	var outID uint32
	status, _ := call2(p.getOut, handle, uintptr(unsafe.Pointer(&outID)))
	if uint32(status) != nvapiStatusOK {
		return 0, p.statusError(uint32(status), "NvAPI_GetAssociatedDisplayOutputId")
	}
	return outID, nil
}

func (p *nvapiProcs) displayPortInfo(handle uintptr, outputID uint32) (*nvDPInfoV1, error) {
	info := nvDPInfoV1{Version: nvDPInfoV1Version}
	status, _ := call3(p.getDP, handle, uintptr(outputID), uintptr(unsafe.Pointer(&info)))
	if uint32(status) != nvapiStatusOK {
		return nil, p.statusError(uint32(status), "NvAPI_GetDisplayPortInfo")
	}
	return &info, nil
}

func (d *nvapiDriver) Name() string {
	return "NVIDIA NVAPI"
}

func (d *nvapiDriver) ReadDPCD(addr uint32, length uint32) ([]byte, error) {
	if length == 0 {
		return nil, fmt.Errorf("dpcd read length must be greater than zero")
	}
	if length > dpAuxMaxPayload {
		return nil, fmt.Errorf("dpcd read length %d exceeds 16-byte limit", length)
	}

	d.mu.Lock()
	defer d.mu.Unlock()

	// 準備 NVAPI 所需的參數結構，指定操作型態與目標位址。
	params := nvDpAuxParamsV1{
		Version:   nvDpAuxParamsV1Version,
		OutputID:  d.outputID,
		Op:        dpAuxOpReadDPCD,
		Address:   addr,
		LenMinus1: length - 1,
	}

	status, _ := call3(d.procs.dpAux, d.displayHandle, uintptr(unsafe.Pointer(&params)), uintptr(unsafe.Sizeof(params)))
	if uint32(status) != nvapiStatusOK {
		if params.Status == nvapiDpAuxTimeout {
			return nil, fmt.Errorf("nvapi: dp aux transaction timed out")
		}
		return nil, d.procs.statusError(uint32(status), "NvAPI_Disp_DpAuxChannelControl")
	}
	if params.Status == nvapiDpAuxTimeout {
		return nil, fmt.Errorf("nvapi: dp aux transaction timed out")
	}
	if params.Status != 0 {
		return nil, fmt.Errorf("nvapi: dp aux error status 0x%X", uint32(params.Status))
	}

	// LenMinus1 回報實際讀取的位元組數，需再加 1 才是真實長度。
	actual := int(params.LenMinus1 + 1)
	if actual < 0 {
		actual = 0
	}
	if actual > int(length) {
		actual = int(length)
	}

	data := make([]byte, actual)
	copy(data, params.Buf[:actual])
	return data, nil
}

func (d *nvapiDriver) WriteDPCD(addr uint32, data []byte) error {
	return ErrNotImplemented
}

func (d *nvapiDriver) ReadI2C(addr uint32, length uint32) ([]byte, error) {
	return nil, ErrNotImplemented
}

func (d *nvapiDriver) WriteI2C(addr uint32, data []byte) error {
	return ErrNotImplemented
}

func (p *nvapiProcs) statusError(status uint32, context string) error {
	if status == nvapiStatusOK {
		return nil
	}
	message := fmt.Sprintf("status 0x%08X", status)
	if p.getErr != 0 {
		// 呼叫 NVAPI 取得更具體的錯誤訊息。
		buf := make([]byte, 256)
		call2(p.getErr, uintptr(status), uintptr(unsafe.Pointer(&buf[0])))
		if idx := bytes.IndexByte(buf, 0); idx >= 0 {
			buf = buf[:idx]
		}
		if trimmed := strings.TrimSpace(string(buf)); trimmed != "" {
			message = trimmed
		}
	}
	if context != "" {
		// 若提供 context，將其加入錯誤訊息中便於追蹤。
		return fmt.Errorf("%s: %s (0x%08X)", context, message, status)
	}
	return fmt.Errorf("nvapi error: %s (0x%08X)", message, status)
}

func call0(fn uintptr) (uintptr, syscall.Errno) {
	r1, _, e1 := syscall.SyscallN(fn)
	return r1, e1
}

func call2(fn, a1, a2 uintptr) (uintptr, syscall.Errno) {
	r1, _, e1 := syscall.SyscallN(fn, a1, a2)
	return r1, e1
}

func call3(fn, a1, a2, a3 uintptr) (uintptr, syscall.Errno) {
	r1, _, e1 := syscall.SyscallN(fn, a1, a2, a3)
	return r1, e1
}
