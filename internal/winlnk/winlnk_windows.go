//go:build windows

// Package winlnk cria atalhos (.lnk) do Windows pela interface COM IShellLinkW,
// sem chamar PowerShell (antivírus desconfiam de PowerShell escondido com
// -ExecutionPolicy Bypass).
package winlnk

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"syscall"
	"unsafe"
)

var (
	ole32            = syscall.NewLazyDLL("ole32.dll")
	pCoInitializeEx  = ole32.NewProc("CoInitializeEx")
	pCoUninitialize  = ole32.NewProc("CoUninitialize")
	pCoCreateInstance = ole32.NewProc("CoCreateInstance")
)

type guid struct {
	d1     uint32
	d2, d3 uint16
	d4     [8]byte
}

var (
	clsidShellLink  = guid{0x00021401, 0, 0, [8]byte{0xC0, 0, 0, 0, 0, 0, 0, 0x46}}
	iidIShellLinkW  = guid{0x000214F9, 0, 0, [8]byte{0xC0, 0, 0, 0, 0, 0, 0, 0x46}}
	iidIPersistFile = guid{0x0000010B, 0, 0, [8]byte{0xC0, 0, 0, 0, 0, 0, 0, 0x46}}
)

const (
	coinitApartmentThreaded = 0x2
	clsctxInprocServer      = 0x1
	rpcEChangedMode         = 0x80010106
)

// índices na tabela de métodos (vtable)
const (
	vRelease          = 2
	vQueryInterface   = 0
	vSetDescription   = 7
	vSetWorkingDir    = 9
	vSetArguments     = 11
	vSetIconLocation  = 17
	vSetPath          = 20
	vPersistFileSave  = 6
)

type comObj struct{ vtbl *[32]uintptr }

func (o *comObj) call(i int, args ...uintptr) uintptr {
	a := append([]uintptr{uintptr(unsafe.Pointer(o))}, args...)
	r, _, _ := syscall.SyscallN(o.vtbl[i], a...)
	return r
}

func failed(hr uintptr) bool { return int32(hr) < 0 }

func wstr(s string) *uint16 {
	p, _ := syscall.UTF16PtrFromString(s)
	return p
}

func up(p *uint16) uintptr { return uintptr(unsafe.Pointer(p)) }

// Create grava o atalho lnk apontando para target.
func Create(lnk, target, args, workdir, desc string) error {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	hr, _, _ := pCoInitializeEx.Call(0, coinitApartmentThreaded)
	if !failed(hr) {
		defer pCoUninitialize.Call()
	} else if uint32(hr) != rpcEChangedMode {
		return fmt.Errorf("CoInitializeEx: 0x%08X", uint32(hr))
	}
	var sl *comObj
	hr, _, _ = pCoCreateInstance.Call(uintptr(unsafe.Pointer(&clsidShellLink)), 0, clsctxInprocServer,
		uintptr(unsafe.Pointer(&iidIShellLinkW)), uintptr(unsafe.Pointer(&sl)))
	if failed(hr) || sl == nil { return fmt.Errorf("CoCreateInstance(ShellLink): 0x%08X", uint32(hr)) }
	defer sl.call(vRelease)

	pTarget, pArgs, pDir, pDesc, pLnk := wstr(target), wstr(args), wstr(workdir), wstr(desc), wstr(lnk)
	defer runtime.KeepAlive([]*uint16{pTarget, pArgs, pDir, pDesc, pLnk})
	steps := []struct {
		name string
		hr   uintptr
	}{
		{"SetPath", sl.call(vSetPath, up(pTarget))},
		{"SetArguments", sl.call(vSetArguments, up(pArgs))},
		{"SetWorkingDirectory", sl.call(vSetWorkingDir, up(pDir))},
		{"SetDescription", sl.call(vSetDescription, up(pDesc))},
		{"SetIconLocation", sl.call(vSetIconLocation, up(pTarget), 0)},
	}
	for _, s := range steps { if failed(s.hr) { return fmt.Errorf("%s: 0x%08X", s.name, uint32(s.hr)) } }

	var pf *comObj
	hr = sl.call(vQueryInterface, uintptr(unsafe.Pointer(&iidIPersistFile)), uintptr(unsafe.Pointer(&pf)))
	if failed(hr) || pf == nil { return fmt.Errorf("QueryInterface(IPersistFile): 0x%08X", uint32(hr)) }
	defer pf.call(vRelease)
	_ = os.MkdirAll(filepath.Dir(lnk), 0o755)
	if hr = pf.call(vPersistFileSave, up(pLnk), 1); failed(hr) { return fmt.Errorf("Save: 0x%08X", uint32(hr)) }
	return nil
}
