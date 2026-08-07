//go:build windows

package main

import (
	"os"
	"syscall"

	"github.com/wailsapp/wails/v3/pkg/w32"
)

// 标题栏背景与应用内 --mac-bg 一致（浅 #f5f6fa / 深 #12141a），COLORREF 为 0x00BBGGRR。
const (
	titleBarColourLight uint32 = 0x00FAF6F5
	titleBarColourDark  uint32 = 0x001A1412
)

// Wails alpha.38 的窗口主题只在创建时应用一次，公开 API 没有运行时切换入口，
// 因此按进程枚举顶层窗口，直接通过 DWM 设置标题栏深浅色与背景色。
var enumWindowsThemeCallback = syscall.NewCallback(func(hwnd uintptr, lparam uintptr) uintptr {
	_, windowPid := w32.GetWindowThreadProcessId(w32.HWND(hwnd))
	if windowPid == os.Getpid() {
		dark := lparam == 1
		w32.SetTheme(hwnd, dark)
		if dark {
			w32.SetTitleBarColour(hwnd, titleBarColourDark)
		} else {
			w32.SetTitleBarColour(hwnd, titleBarColourLight)
		}
	}
	return 1
})

func applyNativeWindowTheme(dark bool) {
	var flag uintptr
	if dark {
		flag = 1
	}
	w32.EnumWindows(enumWindowsThemeCallback, flag)
}
