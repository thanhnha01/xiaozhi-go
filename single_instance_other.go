//go:build !windows

package main

// Single-instance behavior is intended only for native Windows GUI releases.
func claimGUIInstance() (release func(), alreadyRunning bool, err error) {
    return func() {}, false, nil
}
