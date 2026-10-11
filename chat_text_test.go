package main

import (
 "net/http"
 "net/http/httptest"
 "strings"
 "testing"

 "github.com/gorilla/websocket"
)

func TestValidateChatText(t *testing.T) {
 cases:=[]struct{input string;valid bool;expected string}{
  {"  Xin chào  ",true,"Xin chào"},
  {"",false,""},
  {"   ",false,""},
  {strings.Repeat("á",80),true,strings.Repeat("á",80)},
  {strings.Repeat("á",81),false,""},
  {"abc\x00def",false,""},
 }
 for _,tc:=range cases {
  got,err:=validateChatText(tc.input)
  if (err==nil)!=tc.valid {t.Errorf("input=%q valid=%v err=%v",tc.input,tc.valid,err)}
  if tc.valid&&got!=tc.expected {t.Errorf("got %q, expected %q",got,tc.expected)}
 }
}

func TestSendTextQueryWebSocket(t *testing.T) {
 messages:=make(chan Message,1)
 server:=httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter,r *http.Request){
  c,err:=(&websocket.Upgrader{CheckOrigin:func(r *http.Request) bool{return true}}).Upgrade(w,r,nil)
  if err!=nil{return}
  defer c.Close()
  var msg Message
  if err:=c.ReadJSON(&msg);err==nil { messages<-msg }
 }))
 defer server.Close()
 wsURL:="ws"+strings.TrimPrefix(server.URL,"http")
 conn,_,err:=websocket.DefaultDialer.Dial(wsURL,nil)
 if err!=nil{t.Fatal(err)}
 p:=&ProtocolClient{conn:conn,sessionID:"test-session"}
 defer p.Close()
 if err:=p.SendTextQuery("Xin chào");err!=nil{t.Fatal(err)}
 msg:=<-messages
 if msg.Type!="listen"||msg.State!="detect"||msg.Text!="Xin chào"||msg.SessionID!="test-session"{
  t.Fatalf("unexpected payload: %+v",msg)
 }
}
