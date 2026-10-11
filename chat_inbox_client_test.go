package main

import (
 "context"
 "encoding/json"
 "net/http"
 "net/http/httptest"
 "strings"
 "testing"
 "time"
)
func TestInboxSubmitAndCheck(t *testing.T){
 seen:=0
 server:=httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter,r *http.Request){
  if r.Header.Get("Authorization")!="Bearer test-secret" {t.Error("missing auth")}
  switch r.URL.Path {
  case "/api/chat/inbox":
   if r.Method!="POST"{t.Errorf("method %s",r.Method)}
   var body map[string]string
   if json.NewDecoder(r.Body).Decode(&body)!=nil||body["text"]!="Câu hỏi thử nghiệm nhiều dòng" {t.Error("invalid body")}
   seen++
   _,_=w.Write([]byte(`{"ok":true,"code":"A7B9K2"}`))
  case "/api/chat/status":
   if r.URL.Query().Get("code")!="A7B9K2"{t.Error("bad code")}
   _,_=w.Write([]byte(`{"ok":true,"status":"fetched"}`))
  default: http.NotFound(w,r)
  }
 }))
 defer server.Close()
 c:=&ChatInboxClient{BaseURL:server.URL,Token:"test-secret",HTTP:server.Client()}
 code,err:=c.Submit(context.Background(),"Câu hỏi thử nghiệm nhiều dòng")
 if err!=nil||code!="A7B9K2"{t.Fatalf("submit %q %v",code,err)}
 got,err:=c.Check(context.Background(),code)
 if err!=nil||got!="fetched"{t.Fatalf("check %q %v",got,err)}
 if seen!=1{t.Fatalf("sent %d times",seen)}
}
func TestInboxValidation(t *testing.T){
 cases:=[]ChatInboxClient{
  {},
  {BaseURL:"http://example.com",Token:"abc"},
  {BaseURL:"https://example.com/?token=bad",Token:"abc"},
  {BaseURL:"https://me:password@example.com",Token:"abc"},
 }
 for _,c:=range cases{if c.Validate()==nil{t.Errorf("expected invalid: %+v",c)}}
 good:=ChatInboxClient{BaseURL:"https://example.workers.dev",Token:"a"}
 if err:=good.Validate();err!=nil{t.Fatal(err)}
}
func TestInboxRequestTimeout(t *testing.T){
 server:=httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter,r *http.Request){time.Sleep(100*time.Millisecond)}))
 defer server.Close()
 c:=&ChatInboxClient{BaseURL:server.URL,Token:"x",HTTP:server.Client()}
 ctx,cancel:=context.WithTimeout(context.Background(),10*time.Millisecond)
 defer cancel()
 _,err:=c.Submit(ctx,strings.Repeat("v",50))
 if err==nil{t.Fatal("expected timeout")}
}
