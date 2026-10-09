package main

import (
 "bytes"
 "encoding/json"
 "image/gif"
 "net/http"
 "net/http/httptest"
 "strings"
 "testing"
)

func TestNormalizeEmotion(t *testing.T){
 for input,want:=range map[string]string{" HAPPY ":"happy","LOVING":"loving","sad":"sad","../../evil":"neutral","":"neutral","invalid":"neutral"}{
  if got:=normalizeEmotion(input);got!=want{t.Errorf("%q=%q want %q",input,got,want)}
 }
}

func TestEmbeddedMascotAssets(t *testing.T){
 for name:=range mascotFiles{
  data,err:=mascotAssets.ReadFile("assets/mascot/"+name)
  if err!=nil{t.Fatalf("%s: %v",name,err)}
  if len(data)<100||(!bytes.HasPrefix(data,[]byte("GIF87a"))&&!bytes.HasPrefix(data,[]byte("GIF89a"))){t.Fatalf("%s is not a GIF file",name)}
  im,err:=gif.DecodeConfig(bytes.NewReader(data))
  if err!=nil{t.Fatalf("%s decode: %v",name,err)}
  if im.Width<16||im.Height<16{t.Errorf("%s invalid dimensions",name)}
 }
}
func TestMascotHTTPWhitelist(t *testing.T){
 d:=&Dashboard{}
 for _,tt:=range []struct{path string;status int}{
  {"/assets/emotions/idle.gif",200},
  {"/assets/emotions/speaking.gif",200},
  {"/assets/emotions/secret.txt",404},
  {"/assets/emotions/qoobee.gif",404},
  {"/assets/emotions/%2e%2e/idle.gif",404},
 }{
  req:=httptest.NewRequest(http.MethodGet,tt.path,nil)
  rec:=httptest.NewRecorder()
  d.serveMascot(rec,req)
  if rec.Code!=tt.status{t.Errorf("%s got %d expected %d",tt.path,rec.Code,tt.status)}
  if tt.status==200&&!strings.Contains(rec.Header().Get("Content-Type"),"image/gif"){t.Errorf("%s content type %q",tt.path,rec.Header().Get("Content-Type"))}
 }
 req:=httptest.NewRequest(http.MethodHead,"/assets/emotions/idle.gif",nil)
 rec:=httptest.NewRecorder();d.serveMascot(rec,req)
 if rec.Code!=200||rec.Body.Len()!=0{t.Fatalf("HEAD status=%d body=%d",rec.Code,rec.Body.Len())}
 req=httptest.NewRequest(http.MethodPost,"/assets/emotions/idle.gif",nil)
 rec=httptest.NewRecorder();d.serveMascot(rec,req)
 if rec.Code!=http.StatusMethodNotAllowed{t.Fatalf("POST status %d",rec.Code)}
}
func TestDashboardEmotionsInStatus(t *testing.T){
 app:=&App{sm:NewStateMachine(),cfg:&DeviceConfig{}}
 d:=&Dashboard{app:app,emotion:"happy",nonce:"secret"}
 req:=httptest.NewRequest(http.MethodGet,"/api/status",nil)
 rec:=httptest.NewRecorder();d.handler().ServeHTTP(rec,req)
 if rec.Code!=200{t.Fatalf("status %d, body %s",rec.Code,rec.Body.String())}
 var data map[string]any
 if err:=json.Unmarshal(rec.Body.Bytes(),&data);err!=nil{t.Fatal(err)}
 if data["emotion"]!="happy"{t.Errorf("emotion = %v",data["emotion"])}
 d.setEmotion("../bad")
 if d.emotion!="neutral"{t.Fatalf("unsupported emotion %s",d.emotion)}
}
