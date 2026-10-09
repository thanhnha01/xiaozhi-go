package main

import (
 "os"
 "path/filepath"
 "strings"
 "testing"
)

func TestReleaseVersionComparison(t *testing.T){
 cases:=[]struct{version string;want bool}{
  {"v1.3.3",true},{"v1.4.0",true},{"v2.0.0",true},
  {"v1.3.2",false},{"v1.2.99",false},{"bad",false},{"v2.1",false},
 }
 for _,c:=range cases{if got:=newerVersion(c.version);got!=c.want{t.Errorf("%s got %t want %t",c.version,got,c.want)}}
}
func TestTrustedReleaseURL(t *testing.T){
 valid:="https://github.com/thanhnha01/xiaozhi-go/releases/download/v1.4.0/XiaoZhi-PC-Setup-v1.4.0.exe"
 if !trustedReleaseURL(valid){t.Fatal("official release rejected")}
 for _,u:=range []string{"https://example.com/exe","http://github.com/thanhnha01/xiaozhi-go/releases/download/v1.4.0/setup.exe","https://github.com/thanhnha01/xiaozhi-go/releases/download/../evil.exe","https://github.com/thanhnha01/xiaozhi-go/releases/download/v1.4.0/setup.exe?bad=1"}{
  if trustedReleaseURL(u){t.Errorf("unsafe URL allowed %s",u)}
 }
}
func TestExplicitConfigPathIsStable(t *testing.T){
 custom:=filepath.Join(t.TempDir(),"device_config.json")
 if got:=resolveConfigPath(custom);got!=custom{t.Fatalf("explicit path changed %q",got)}
}
func TestGeneratedDeviceIDsPersist(t *testing.T){
 old:=configPath
 configPath=filepath.Join(t.TempDir(),"config.json")
 defer func(){configPath=old}()
 a,err:=loadConfig();if err!=nil{t.Fatal(err)}
 ensureDeviceIdentity(a)
 if a.MacAddress==""||a.UUID==""{t.Fatal("missing identity")}
 if err=a.saveConfig();err!=nil{t.Fatal(err)}
 b,err:=loadConfig();if err!=nil{t.Fatal(err)}
 ensureDeviceIdentity(b)
 if a.MacAddress!=b.MacAddress||a.UUID!=b.UUID{t.Fatal("device identity changed after restart")}
 c,err:=os.ReadFile(configPath);if err!=nil{t.Fatal(err)}
 if !strings.Contains(string(c),a.UUID){t.Fatal("identity not serialized")}
}
