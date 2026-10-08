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
func TestDeviceMCPToolListEmpty(t *testing.T) {
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
