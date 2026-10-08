//go:build windows

package main

import (
    "fmt"
    "runtime"

    webview2 "github.com/jchv/go-webview2"
)

// Native Win32 window with Microsoft's Edge WebView2. The localhost backend is
// embedded in this process; no command prompt or external browser is required.
func openDesktopWindow(d *Dashboard) error {
    ready := make(chan error, 1)
    go func() {
        runtime.LockOSThread()
        defer runtime.UnlockOSThread()

        w := webview2.NewWithOptions(webview2.WebViewOptions{
            Debug: false,
            AutoFocus: true,
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
        ready <- nil
        w.Run()
        w.Destroy()
        // Closing the GUI also shuts down the background audio / socket process.
        d.app.quitOnce.Do(func(){ close(d.app.quitChan) })
    }()
    return <-ready
}
