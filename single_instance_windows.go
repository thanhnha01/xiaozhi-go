//go:build windows

package main

import (
    "fmt"
    "syscall"
    "time"
    "unsafe"
)

// Mutexes in the Local namespace are scoped to the current Windows logon
// session, so other signed-in users can run their own XiaoZhi instances.
const xiaoZhiGUIInstanceMutex = "Local\\XiaoZhiPC_GUI_SingleInstance_v1"
const xiaoZhiWebViewClass = "webview"

var (
    kernel32SingleInstance = syscall.NewLazyDLL("kernel32.dll")
    user32SingleInstance = syscall.NewLazyDLL("user32.dll")
    procCreateMutexW = kernel32SingleInstance.NewProc("CreateMutexW")
    procCloseHandle = kernel32SingleInstance.NewProc("CloseHandle")
    procFindWindowW = user32SingleInstance.NewProc("FindWindowW")
    procShowWindow = user32SingleInstance.NewProc("ShowWindow")
    procSetForegroundWindow = user32SingleInstance.NewProc("SetForegroundWindow")
)

// claimInstance is atomic across processes: unlike checking process names or
// using a lock file, the Windows kernel decides exactly one mutex owner.
// The returned handle is kept open for the entire application lifetime.
func claimInstance(name string) (release func(), alreadyRunning bool, err error) {
    ptr, err := syscall.UTF16PtrFromString(name)
    if err != nil { return nil, false, err }

    handle, _, callErr := procCreateMutexW.Call(0, 0, uintptr(unsafe.Pointer(ptr)))
    if handle == 0 {
        return nil, false, fmt.Errorf("không tạo được khóa phiên ứng dụng: %w", callErr)
    }
    if callErr == syscall.Errno(183) { // ERROR_ALREADY_EXISTS
        procCloseHandle.Call(handle)
        return nil, true, nil
    }
    return func() { procCloseHandle.Call(handle) }, false, nil
}

// bringExistingWindowToFront is best-effort. During initial startup the mutex
// exists before the native window does, so retry briefly rather than starting
// another application or changing the device's saved identity.
func bringExistingWindowToFront() bool {
    class, _ := syscall.UTF16PtrFromString(xiaoZhiWebViewClass)
    title, _ := syscall.UTF16PtrFromString(xiaoZhiWindowOptions().Title)
    for attempt := 0; attempt < 25; attempt++ {
        hwnd, _, _ := procFindWindowW.Call(
            uintptr(unsafe.Pointer(class)),
            uintptr(unsafe.Pointer(title)),
        )
        if hwnd != 0 {
            procShowWindow.Call(hwnd, 9) // SW_RESTORE, including minimized windows
            procSetForegroundWindow.Call(hwnd)
            return true
        }
        time.Sleep(120 * time.Millisecond)
    }
    return false
}

func claimGUIInstance() (release func(), alreadyRunning bool, err error) {
    release, alreadyRunning, err = claimInstance(xiaoZhiGUIInstanceMutex)
    if alreadyRunning {
        bringExistingWindowToFront()
    }
    return release, alreadyRunning, err
}
