package main

import (
 "testing"
)

func TestDashboardQuitClosesNativeWindowFirst(t *testing.T) {
 app := &App{quitChan:make(chan struct{})}
 d,err := newDashboard(app)
 if err!=nil{t.Fatal(err)}
 closed:=0
 d.setWindowClose(func(){closed++})
 if err:=d.performAction("quit",0);err!=nil{t.Fatal(err)}
 if closed!=1{t.Fatalf("expected native window close request, got %d",closed)}
 select {
 case <-app.quitChan: t.Fatal("backend terminated before window message loop exited")
 default:
 }
 d.setWindowClose(nil)
 d.requestQuit()
 select {
 case <-app.quitChan:
 default: t.Fatal("backend quit channel left open")
 }
 // Repeated exit signals must not panic.
 d.requestQuit()
}
