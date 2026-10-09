package main

import (
    "encoding/json"
    "strings"
    "testing"
)

func TestDeviceMCPInitialize(t *testing.T) {
    out, err := deviceMCPResponse([]byte(`{"jsonrpc":"2.0","id":42,"method":"initialize"}`))
    if err != nil { t.Fatal(err) }
    var res map[string]json.RawMessage
    if err := json.Unmarshal([]byte(out), &res); err != nil { t.Fatal(err) }
    if string(res["id"]) != "42" { t.Fatalf("wrong ID: %s", res["id"]) }
    if !strings.Contains(string(res["result"]), "protocolVersion") { t.Fatalf("missing version: %s", out) }
}
func TestDeviceMCPToolListEmptyWithoutPlayer(t *testing.T) {
    out, err := deviceMCPResponse([]byte(`{"jsonrpc":"2.0","id":"a","method":"tools/list"}`))
    if err != nil { t.Fatal(err) }
    if !strings.Contains(out, `"tools":[]`) { t.Fatalf("unexpected tools: %s", out) }
}
func TestDeviceMCPNotificationNoResponse(t *testing.T) {
    out, err := deviceMCPResponse([]byte(`{"jsonrpc":"2.0","method":"notifications/initialized"}`))
    if err != nil || out != "" { t.Fatalf("notification answered: %s, %v", out, err) }
}
func TestDeviceMCPUnknownMethodError(t *testing.T) {
    out, err := deviceMCPResponse([]byte(`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"not-present"}}`))
    if err != nil { t.Fatal(err) }
    if !strings.Contains(out, `"code":-32601`) { t.Fatalf("should not report false success: %s", out) }
}
func TestDeviceMCPBadJSON(t *testing.T) {
    if _, err := deviceMCPResponse([]byte("not json")); err == nil { t.Fatal("expected error") }
}

func TestDeviceMusicToolList(t *testing.T){
 p:=NewMusicPlayer(nil,defaultMusicURL)
 out,err:=deviceMCPResponseWithPlayer([]byte(`{"jsonrpc":"2.0","id":1,"method":"tools/list"}`),p)
 if err!=nil{t.Fatal(err)}
 for _,name:=range []string{"self.music.play_song","self.music.pause","self.music.resume","self.music.stop","self.music.get_status"}{
  if !strings.Contains(out,name){t.Errorf("missing %s",name)}
 }
}
func TestDeviceMusicStatus(t *testing.T){
 p:=NewMusicPlayer(nil,defaultMusicURL)
 out,err:=deviceMCPResponseWithPlayer([]byte(`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"self.music.get_status","arguments":{}}}`),p)
 if err!=nil{t.Fatal(err)}
 if !strings.Contains(out,"stopped"){t.Fatal(out)}
}
func TestDeviceMusicRejectUnknown(t *testing.T){
 p:=NewMusicPlayer(nil,defaultMusicURL)
 out,err:=deviceMCPResponseWithPlayer([]byte(`{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"self.music.bad","arguments":{}}}`),p)
 if err!=nil{t.Fatal(err)}
 if !strings.Contains(out,"-32601"){t.Fatal(out)}
}
