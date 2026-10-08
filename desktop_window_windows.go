//go:build windows

package main

import (
    "fmt"
    "os"
    "path/filepath"
    "runtime"

    webview2 "github.com/jchv/go-webview2"
)

// Native Win32 window with Microsoft's Edge WebView2. The localhost backend is
// embedded in this process; no command prompt or external browser is required.
func openDesktopWindow(d *Dashboard) error {
    // Keep Chromium's profile outside the portable EXE/DLL folder.
    profilePath, err := webViewProfilePath()
    if err != nil { return err }
    ready := make(chan error, 1)
    go func() {
        runtime.LockOSThread()
        defer runtime.UnlockOSThread()

        w := webview2.NewWithOptions(webview2.WebViewOptions{
            Debug: false,
            AutoFocus: true,
            DataPath: profilePath,
            WindowOptions: webview2.WindowOptions{
                Title: "XiaoZhi PC - Trợ lý giọng nói",
                Width: 1480, Height: 930,
                Center: true,
            },
        })
        if w == nil {
            ready <- fmt.Errorf("không thể mở WebView2; vui lòng cài Microsoft Edge WebView2 Runtime")
            return
        }
        w.SetSize(1160, 750, webview2.HintMin)
        w.Navigate(d.url)
        // WM_CLOSE must be posted to the window while its message loop is alive.
        // Calling Destroy() after Run() returns is too late.
        d.setWindowClose(func(){ w.Destroy() })
        ready <- nil
        w.Run()
        d.setWindowClose(nil)
        // The GUI thread has now left its message loop. Ask the Go backend
        // to stop; main() will close socket, PortAudio and HTTP server.
        d.app.quitOnce.Do(func(){ close(d.app.quitChan) })
    }()
    return <-ready
}

// webViewProfilePath moves WebView2 data to %LOCALAPPDATA% so updates or
// deletion of the portable app folder are never blocked by Chromium cache.
func webViewProfilePath() (string, error) {
    base, err := os.UserCacheDir()
    if err != nil || base == "" {
        return "", fmt.Errorf("không tìm được thư mục dữ liệu Windows: %v", err)
    }
    profile := filepath.Join(base, "XiaoZhiPC", "WebView2")
    if err := os.MkdirAll(profile, 0700); err != nil {
        return "", fmt.Errorf("không tạo được thư mục WebView2: %w", err)
    }
    return profile, nil
}
