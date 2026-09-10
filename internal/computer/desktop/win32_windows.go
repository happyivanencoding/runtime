//go:build windows && amd64

// Copyright 2026 runtime-core contributors.
// SPDX-License-Identifier: Apache-2.0
package desktop

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"golang.org/x/sys/windows"
	"image"
	"image/png"
	"path/filepath"
	"runtime"
	"unsafe"
)

var user32 = windows.NewLazySystemDLL("user32.dll")
var kernel32 = windows.NewLazySystemDLL("kernel32.dll")
var gdi32 = windows.NewLazySystemDLL("gdi32.dll")
var isWindow = user32.NewProc("IsWindow")
var enumWindows = user32.NewProc("EnumWindows")
var isVisible = user32.NewProc("IsWindowVisible")
var isIconic = user32.NewProc("IsIconic")
var getWindowText = user32.NewProc("GetWindowTextW")
var getClassName = user32.NewProc("GetClassNameW")
var getWindowRect = user32.NewProc("GetWindowRect")
var getWindowPID = user32.NewProc("GetWindowThreadProcessId")
var getForeground = user32.NewProc("GetForegroundWindow")
var setThreadDPI = user32.NewProc("SetThreadDpiAwarenessContext")
var openClipboard = user32.NewProc("OpenClipboard")
var closeClipboard = user32.NewProc("CloseClipboard")
var emptyClipboard = user32.NewProc("EmptyClipboard")
var getClipboard = user32.NewProc("GetClipboardData")
var setClipboard = user32.NewProc("SetClipboardData")
var clipboardAvailable = user32.NewProc("IsClipboardFormatAvailable")
var globalAlloc = kernel32.NewProc("GlobalAlloc")
var globalLock = kernel32.NewProc("GlobalLock")
var globalUnlock = kernel32.NewProc("GlobalUnlock")
var globalFree = kernel32.NewProc("GlobalFree")
var globalSize = kernel32.NewProc("GlobalSize")

type windowEnumeration struct {
	ctx        context.Context
	pid        uint32
	foreground uintptr
	items      []Window
}

// Go native callback slots are never released: create ONE callback for all calls.
var windowCallback = windows.NewCallback(enumerateWindowCallback)

func enumerateWindowCallback(hwnd, parameter uintptr) uintptr {
	state := (*windowEnumeration)(unsafe.Pointer(parameter))
	if state.ctx.Err() != nil {
		return 0
	}
	if visible, _, _ := isVisible.Call(hwnd); visible == 0 {
		return 1
	}
	var pid uint32
	getWindowPID.Call(hwnd, uintptr(unsafe.Pointer(&pid)))
	if state.pid != 0 && pid != state.pid {
		return 1
	}
	title, class := make([]uint16, 2048), make([]uint16, 256)
	n, _, _ := getWindowText.Call(hwnd, uintptr(unsafe.Pointer(&title[0])), uintptr(len(title)))
	if n == 0 {
		return 1
	}
	getClassName.Call(hwnd, uintptr(unsafe.Pointer(&class[0])), uintptr(len(class)))
	var bounds Rect
	getWindowRect.Call(hwnd, uintptr(unsafe.Pointer(&bounds)))
	application := ""
	if handle, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, pid); err == nil {
		buf := make([]uint16, 32768)
		size := uint32(len(buf))
		if windows.QueryFullProcessImageName(handle, 0, &buf[0], &size) == nil {
			application = filepath.Base(windows.UTF16ToString(buf[:size]))
		}
		_ = windows.CloseHandle(handle)
	}
	state.items = append(state.items, Window{Handle: uint64(hwnd), ProcessID: pid, Application: application, Title: windows.UTF16ToString(title), ClassName: windows.UTF16ToString(class), Bounds: bounds, Foreground: hwnd == state.foreground})
	return 1
}

func enumerateWindows(ctx context.Context, pidFilter uint32) ([]Window, error) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	previous, _, _ := setThreadDPI.Call(^uintptr(3))
	if previous != 0 {
		defer setThreadDPI.Call(previous)
	}
	foreground, _, _ := getForeground.Call()
	state := windowEnumeration{ctx: ctx, pid: pidFilter, foreground: foreground, items: []Window{}}
	ok, _, err := enumWindows.Call(windowCallback, uintptr(unsafe.Pointer(&state)))
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	if ok == 0 {
		return nil, fmt.Errorf("EnumWindows: %w", err)
	}
	return state.items, nil
}

func clipboardNative(ctx context.Context, action, text string) (map[string]any, error) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	owner := uintptr(0)
	if action == "write" {
		class, _ := windows.UTF16PtrFromString("STATIC")
		var createErr error
		owner, _, createErr = user32.NewProc("CreateWindowExW").Call(0, uintptr(unsafe.Pointer(class)), 0, 0, 0, 0, 0, 0, ^uintptr(2), 0, 0, 0)
		if owner == 0 {
			return nil, fmt.Errorf("create clipboard owner window: %w", createErr)
		}
		defer user32.NewProc("DestroyWindow").Call(owner)
	}
	ok, _, err := openClipboard.Call(owner)
	if ok == 0 {
		return nil, fmt.Errorf("clipboard is currently unavailable: %w", err)
	}
	defer closeClipboard.Call()
	if action == "read" {
		available, _, _ := clipboardAvailable.Call(13)
		if available == 0 {
			return map[string]any{"has_text": false, "text": ""}, nil
		}
		handle, _, err := getClipboard.Call(13)
		if handle == 0 {
			return nil, err
		}
		size, _, _ := globalSize.Call(handle)
		if size > 2<<20 {
			return nil, errors.New("clipboard text exceeds 1 Mi UTF-16 units")
		}
		ptr, _, err := globalLock.Call(handle)
		if ptr == 0 {
			return nil, err
		}
		defer globalUnlock.Call(handle)
		value := windows.UTF16ToString(unsafe.Slice((*uint16)(unsafe.Pointer(ptr)), int(size/2)))
		return map[string]any{"has_text": true, "text": value}, nil
	}
	data, err := windows.UTF16FromString(text)
	if err != nil {
		return nil, err
	}
	handle, _, err := globalAlloc.Call(2, uintptr(len(data)*2))
	if handle == 0 {
		return nil, err
	}
	owned := true
	defer func() {
		if owned {
			globalFree.Call(handle)
		}
	}()
	ptr, _, err := globalLock.Call(handle)
	if ptr == 0 {
		return nil, err
	}
	copy(unsafe.Slice((*uint16)(unsafe.Pointer(ptr)), len(data)), data)
	globalUnlock.Call(handle)
	if ok, _, err := emptyClipboard.Call(); ok == 0 {
		return nil, err
	}
	if result, _, err := setClipboard.Call(13, handle); result == 0 {
		return nil, err
	}
	owned = false
	return map[string]any{"written": true, "characters": len([]rune(text))}, nil
}

type bitmapHeader struct {
	Size         uint32
	Width        int32
	Height       int32
	Planes       uint16
	BitCount     uint16
	Compression  uint32
	SizeImage    uint32
	XPels        int32
	YPels        int32
	ClrUsed      uint32
	ClrImportant uint32
}

func screenNative(ctx context.Context, handle uint64) ([]byte, Rect, error) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	if err := ctx.Err(); err != nil {
		return nil, Rect{}, err
	}
	if valid, _, _ := isWindow.Call(uintptr(handle)); valid == 0 {
		return nil, Rect{}, errors.New("window no longer exists")
	}
	if minimized, _, _ := isIconic.Call(uintptr(handle)); minimized != 0 {
		return nil, Rect{}, errors.New("window is minimized; restore it before capturing")
	}
	previous, _, _ := setThreadDPI.Call(^uintptr(3))
	if previous != 0 {
		defer setThreadDPI.Call(previous)
	}
	var bounds Rect
	if ok, _, err := getWindowRect.Call(uintptr(handle), uintptr(unsafe.Pointer(&bounds))); ok == 0 {
		return nil, bounds, err
	}
	width, height := int(bounds.Right-bounds.Left), int(bounds.Bottom-bounds.Top)
	if width <= 0 || height <= 0 || int64(width)*int64(height) > 40_000_000 {
		return nil, bounds, errors.New("window capture dimensions are invalid or exceed 40 million pixels")
	}
	dc, _, err := user32.NewProc("GetDC").Call(0)
	if dc == 0 {
		return nil, bounds, err
	}
	defer user32.NewProc("ReleaseDC").Call(0, dc)
	mem, _, err := gdi32.NewProc("CreateCompatibleDC").Call(dc)
	if mem == 0 {
		return nil, bounds, err
	}
	defer gdi32.NewProc("DeleteDC").Call(mem)
	bitmap, _, err := gdi32.NewProc("CreateCompatibleBitmap").Call(dc, uintptr(width), uintptr(height))
	if bitmap == 0 {
		return nil, bounds, err
	}
	defer gdi32.NewProc("DeleteObject").Call(bitmap)
	original, _, _ := gdi32.NewProc("SelectObject").Call(mem, bitmap)
	ok, _, err := gdi32.NewProc("BitBlt").Call(mem, 0, 0, uintptr(width), uintptr(height), dc, uintptr(int64(bounds.Left)), uintptr(int64(bounds.Top)), 0x00CC0020|0x40000000)
	gdi32.NewProc("SelectObject").Call(mem, original)
	if ok == 0 {
		return nil, bounds, err
	}
	pixels := make([]byte, width*height*4)
	header := bitmapHeader{Size: 40, Width: int32(width), Height: -int32(height), Planes: 1, BitCount: 32}
	rows, _, err := gdi32.NewProc("GetDIBits").Call(dc, bitmap, 0, uintptr(height), uintptr(unsafe.Pointer(&pixels[0])), uintptr(unsafe.Pointer(&header)), 0)
	if rows != uintptr(height) {
		return nil, bounds, fmt.Errorf("capture read %d/%d rows: %v", rows, height, err)
	}
	for i := 0; i < len(pixels); i += 4 {
		pixels[i], pixels[i+2] = pixels[i+2], pixels[i]
		pixels[i+3] = 255
	}
	picture := &image.RGBA{Pix: pixels, Stride: width * 4, Rect: image.Rect(0, 0, width, height)}
	var output bytes.Buffer
	if err = png.Encode(&output, picture); err != nil {
		return nil, bounds, err
	}
	return output.Bytes(), bounds, nil
}
