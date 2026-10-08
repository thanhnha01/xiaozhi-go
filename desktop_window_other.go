//go:build !windows

package main

// The application still runs on Linux/macOS as a local browser dashboard.
func openDesktopWindow(d *Dashboard) error {
    return openLocalBrowser(d.url)
}
