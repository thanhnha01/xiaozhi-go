package main

import (
 "encoding/json"
 "strings"
 "testing"
)

func TestFormatServerAlertFromMessageField(t *testing.T){
 raw:=[]byte(`{"type":"alert","status":"error","message":"Detect is only for wake words"}`)
 var msg Message
 if err:=json.Unmarshal(raw,&msg);err!=nil{t.Fatal(err)}
 got:=formatServerAlert(&msg)
 if !strings.Contains(got,"Detect is only for wake words"){t.Fatalf("missing alert message: %q",got)}
 if !strings.Contains(got,"[error]"){t.Fatalf("missing status: %q",got)}
}
func TestFormatServerAlertLegacyTextAndFallback(t *testing.T){
 for _,tc:=range []struct{msg Message;contains string}{
  {Message{Text:"Old alert"},"Old alert"},
  {Message{Detail:"MCP call failed"},"MCP call failed"},
  {Message{Status:"failed"},"Máy chủ không cung cấp nội dung"},
 } {
  got:=formatServerAlert(&tc.msg)
  if !strings.Contains(got,tc.contains){t.Errorf("got %q, want %q",got,tc.contains)}
 }
}
