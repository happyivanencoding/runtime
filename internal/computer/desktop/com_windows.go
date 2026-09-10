//go:build windows && amd64

// Copyright 2026 runtime-core contributors.
// SPDX-License-Identifier: Apache-2.0
package desktop

import (
	"context"
	"errors"
	"fmt"
	"golang.org/x/sys/windows"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"time"
	"unsafe"
)

var ole32 = windows.NewLazySystemDLL("ole32.dll")
var oleaut32 = windows.NewLazySystemDLL("oleaut32.dll")
var coInitialize = ole32.NewProc("CoInitializeEx")
var coUninitialize = ole32.NewProc("CoUninitialize")
var coCreate = ole32.NewProc("CoCreateInstance")
var sysStringLen = oleaut32.NewProc("SysStringLen")
var sysFreeString = oleaut32.NewProc("SysFreeString")
var safeArrayDestroy = oleaut32.NewProc("SafeArrayDestroy")
var safeArrayLBound = oleaut32.NewProc("SafeArrayGetLBound")
var safeArrayUBound = oleaut32.NewProc("SafeArrayGetUBound")
var safeArrayAccess = oleaut32.NewProc("SafeArrayAccessData")
var safeArrayUnaccess = oleaut32.NewProc("SafeArrayUnaccessData")

// Vtable slots below follow the installed Windows SDK UIAutomationClient.h.
// Each interface is created, used and released on one locked COM thread.
type com struct{ vtable *[90]uintptr }

//go:uintptrescapes
func (p *com) call(index int, args ...uintptr) error {
	if p == nil {
		return errors.New("UI Automation interface is unavailable")
	}
	argv := append([]uintptr{uintptr(unsafe.Pointer(p))}, args...)
	result, _, _ := syscall.SyscallN(p.vtable[index], argv...)
	runtime.KeepAlive(p)
	if int32(result) < 0 {
		return fmt.Errorf("UI Automation HRESULT 0x%08X (method %d)", uint32(result), index)
	}
	return nil
}
func (p *com) release() {
	if p != nil {
		_ = p.call(2)
	}
}
func (p *com) intValue(index int) (int32, error) {
	var value int32
	err := p.call(index, uintptr(unsafe.Pointer(&value)))
	return value, err
}
func (p *com) stringValue(index int) (string, error) {
	var text *uint16
	err := p.call(index, uintptr(unsafe.Pointer(&text)))
	if err != nil {
		return "", err
	}
	if text == nil {
		return "", nil
	}
	defer sysFreeString.Call(uintptr(unsafe.Pointer(text)))
	n, _, _ := sysStringLen.Call(uintptr(unsafe.Pointer(text)))
	if n > 65536 {
		return "", errors.New("UI Automation string exceeds 64 Ki UTF-16 units")
	}
	return windows.UTF16ToString(unsafe.Slice(text, int(n))), nil
}
func (p *com) pattern(id int) (*com, error) {
	var value *com
	err := p.call(16, uintptr(id), uintptr(unsafe.Pointer(&value)))
	return value, err
}
func (p *com) runtimeID() (string, error) {
	var array uintptr
	if err := p.call(4, uintptr(unsafe.Pointer(&array))); err != nil {
		return "", err
	}
	if array == 0 {
		return "", nil
	}
	defer safeArrayDestroy.Call(array)
	var low, high int32
	r, _, _ := safeArrayLBound.Call(array, 1, uintptr(unsafe.Pointer(&low)))
	if int32(r) < 0 {
		return "", errors.New("invalid UIA runtime id array")
	}
	r, _, _ = safeArrayUBound.Call(array, 1, uintptr(unsafe.Pointer(&high)))
	if int32(r) < 0 || high < low || high-low > 128 {
		return "", errors.New("invalid UIA runtime id bounds")
	}
	var values *int32
	r, _, _ = safeArrayAccess.Call(array, uintptr(unsafe.Pointer(&values)))
	if int32(r) < 0 {
		return "", errors.New("cannot read UIA runtime id")
	}
	defer safeArrayUnaccess.Call(array)
	parts := []string{}
	for _, value := range unsafe.Slice(values, int(high-low+1)) {
		parts = append(parts, strconv.FormatInt(int64(value), 10))
	}
	return strings.Join(parts, "."), nil
}

type uia struct{ automation, walker *com }

func withUIA(ctx context.Context, fn func(context.Context, *uia) (map[string]any, error)) (map[string]any, error) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	hr, _, _ := coInitialize.Call(0, 0)
	if int32(hr) < 0 {
		return nil, fmt.Errorf("initialize COM: 0x%08X", uint32(hr))
	}
	defer coUninitialize.Call()
	class, _ := windows.GUIDFromString("{e22ad333-b25f-460c-83d0-0581107395c9}")
	iid, _ := windows.GUIDFromString("{34723aff-0c9d-49d0-9896-7ab52df8cd8a}")
	var automation *com
	hr, _, _ = coCreate.Call(uintptr(unsafe.Pointer(&class)), 0, 1, uintptr(unsafe.Pointer(&iid)), uintptr(unsafe.Pointer(&automation)))
	if int32(hr) < 0 {
		return nil, fmt.Errorf("create Windows UI Automation: 0x%08X", uint32(hr))
	}
	defer automation.release()
	if err := automation.call(61, 2000); err != nil {
		return nil, err
	}
	if err := automation.call(63, 2000); err != nil {
		return nil, err
	}
	var walker *com
	if err := automation.call(14, uintptr(unsafe.Pointer(&walker))); err != nil {
		return nil, err
	}
	defer walker.release()
	bounded, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	return fn(bounded, &uia{automation: automation, walker: walker})
}
func (u *uia) element(handle uint64) (*com, error) {
	if handle == 0 {
		return nil, errors.New("an explicit window_handle is required")
	}
	if ok, _, _ := isWindow.Call(uintptr(handle)); ok == 0 {
		return nil, errors.New("window_handle no longer exists; enumerate windows again")
	}
	var element *com
	err := u.automation.call(6, uintptr(handle), uintptr(unsafe.Pointer(&element)))
	return element, err
}
func (u *uia) related(element *com, method int) (*com, error) {
	var next *com
	err := u.walker.call(method, uintptr(unsafe.Pointer(element)), uintptr(unsafe.Pointer(&next)))
	return next, err
}
