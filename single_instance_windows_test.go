//go:build windows

package main

import (
    "fmt"
    "testing"
    "time"
)

func TestClaimSingleInstanceAndRelease(t *testing.T) {
    name := fmt.Sprintf("Local\\XiaoZhiPC_Test_%d", time.Now().UnixNano())
    release, existing, err := claimInstance(name)
    if err != nil { t.Fatal(err) }
    if existing || release == nil { t.Fatal("first launch must own mutex") }

    secondRelease, secondExisting, err := claimInstance(name)
    if err != nil { release(); t.Fatal(err) }
    if !secondExisting || secondRelease != nil {
        release()
        t.Fatal("second launch must not own mutex")
    }
    release()

    againRelease, againExisting, err := claimInstance(name)
    if err != nil { t.Fatal(err) }
    if againExisting || againRelease == nil { t.Fatal("mutex should be released") }
    againRelease()
}
