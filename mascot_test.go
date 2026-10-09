package main

import (
 "encoding/json"
 "net/http"
 "net/http/httptest"
 "os"
 "path/filepath"
 "strings"
 "testing"
)

func TestNormalizeEmotion(t *testing.T){
 for input,want:=range map[string]string{" HAPPY ":"happy","LOVING":"loving","sad":"sad","../../evil":"neutral","":"neutral","not-real":"neutral"}{
  if got:=normalizeEmotion(input);got!=want{t.Errorf("%q = %q, want %q",input,got,want)}
 }
}

func TestMascotAssetWhitelist(t *testing.T){
 dir:=t.TempDir()
 if err:=os.WriteFile(filepath.Join(dir,"bom-dia.gif"),[]byte("GIF89a-example"),0600);err!=nil{t.Fatal(err)}
 if err:=os.WriteFile(filepath.Join(dir,"secret.txt"),[]byte("secret"),0600);err!=nil{t.Fatal(err)}
 d:=&Dashboard{mascotDir:dir}
 for _,tt:=range []struct{path string;status int}{
  {"/assets/emotions/bom-dia.gif",200},
  {"/assets/emotions/secret.txt",404},
  {"/assets/emotions/unknown.gif",404},
  {"/assets/emotions/%2e%2e/secret.txt",404},
 }{
  req:=httptest.NewRequest(http.MethodGet,tt.path,nil);rec:=httptest.NewRecorder()
  d.serveMascot(rec,req)
  if rec.Code!=tt.status{t.Errorf("%s = %d, want %d",tt.path,rec.Code,tt.status)}
  if tt.status==200&&!strings.Contains(rec.Header().Get("Content-Type"),"image/gif"){t.Errorf("%s returned content-type %q",tt.path,rec.Header().Get("Content-Type"))}
 }
 req:=httptest.NewRequest(http.MethodPost,"/assets/emotions/bom-dia.gif",nil)
 rec:=httptest.NewRecorder();d.serveMascot(rec,req)
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
 if d.emotion!="neutral"{t.Fatalf("unsupported emotion should fallback to neutral: %s",d.emotion)}
}
