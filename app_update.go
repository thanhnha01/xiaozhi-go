package main

import (
 "context"
 "crypto/sha256"
 "encoding/hex"
 "encoding/json"
 "errors"
 "fmt"
 "io"
 "net/http"
 "os"
 "os/exec"
 "path/filepath"
 "runtime"
 "strings"
 "time"
)

const releaseAPI = "https://api.github.com/repos/thanhnha01/xiaozhi-go/releases/latest"
const setupFilename = "XiaoZhi-PC-Setup-v1.3.0.exe"

type appUpdate struct { Version string `json:"version"`; URL string `json:"-"`; SHAURL string `json:"-"`; Available bool `json:"available"`; Error string `json:"error,omitempty"` }
type githubRelease struct { Tag string `json:"tag_name"`; Draft bool `json:"draft"`; Prerelease bool `json:"prerelease"`; Assets []struct{Name string `json:"name"`; URL string `json:"browser_download_url"`} `json:"assets"` }

// Local version is intentionally pinned to the installer build. Only HTTPS GitHub release URLs are trusted.
func newerVersion(tag string) bool {
 tag=strings.TrimPrefix(strings.TrimSpace(tag),"v")
 a,b:=strings.Split(tag,"."),strings.Split(appVersion,".")
 if len(a)!=3||len(b)!=3{return false}
 for i:=0;i<3;i++{
  var x,y int
  if _,err:=fmt.Sscanf(a[i],"%d",&x);err!=nil{return false}
  _,_=fmt.Sscanf(b[i],"%d",&y)
  if x>y{return true};if x<y{return false}
 }
 return false
}
func trustedReleaseURL(raw string) bool {
 const prefix="https://github.com/thanhnha01/xiaozhi-go/releases/download/"
 return strings.HasPrefix(raw,prefix)&& !strings.ContainsAny(raw,"?#\\") && !strings.Contains(raw,"..")
}
func checkAppUpdate(ctx context.Context)(appUpdate,error){
 c:=&http.Client{Timeout:12*time.Second}
 req,err:=http.NewRequestWithContext(ctx,http.MethodGet,releaseAPI,nil)
 if err!=nil{return appUpdate{},err}
 req.Header.Set("Accept","application/vnd.github+json")
 req.Header.Set("User-Agent","XiaoZhi-PC/"+appVersion)
 resp,err:=c.Do(req)
 if err!=nil{return appUpdate{},err}
 defer resp.Body.Close()
 if resp.StatusCode!=200{return appUpdate{},fmt.Errorf("GitHub: HTTP %d",resp.StatusCode)}
 var release githubRelease
 if err=json.NewDecoder(io.LimitReader(resp.Body,1<<20)).Decode(&release);err!=nil{return appUpdate{},err}
 u:=appUpdate{Version:strings.TrimPrefix(release.Tag,"v")}
 if release.Draft||release.Prerelease||!newerVersion(release.Tag){return u,nil}
 for _,asset:=range release.Assets{
  if !trustedReleaseURL(asset.URL){continue}
  if asset.Name=="XiaoZhi-PC-Setup-"+release.Tag+".exe"{u.URL=asset.URL}
  if asset.Name=="XiaoZhi-PC-Setup-"+release.Tag+".exe.sha256"{u.SHAURL=asset.URL}
 }
 u.Available=u.URL!=""&&u.SHAURL!=""
 return u,nil
}
func downloadVerifiedUpdate(ctx context.Context,u appUpdate)(string,error){
 if runtime.GOOS!="windows"{return "",errors.New("cập nhật cài đặt chỉ dành cho Windows")}
 if !u.Available||!trustedReleaseURL(u.URL)||!trustedReleaseURL(u.SHAURL){return "",errors.New("bản cập nhật chưa hợp lệ")}
 c:=&http.Client{Timeout:3*time.Minute}
 download:=func(url string,max int64)([]byte,error){
  req,err:=http.NewRequestWithContext(ctx,http.MethodGet,url,nil);if err!=nil{return nil,err}
  resp,err:=c.Do(req);if err!=nil{return nil,err};defer resp.Body.Close()
  if resp.StatusCode!=200{return nil,fmt.Errorf("không tải được bản cập nhật: HTTP %d",resp.StatusCode)}
  return io.ReadAll(io.LimitReader(resp.Body,max+1))
 }
 checksum,err:=download(u.SHAURL,512);if err!=nil{return "",err}
 if len(checksum)>512{return "",errors.New("checksum quá lớn")}
 fields:=strings.Fields(string(checksum));if len(fields)<1||len(fields[0])!=64{return "",errors.New("checksum không hợp lệ")}
 want,err:=hex.DecodeString(fields[0]);if err!=nil{return "",err}
 data,err:=download(u.URL,150<<20);if err!=nil{return "",err}
 if len(data)>150<<20{return "",errors.New("bản cập nhật quá lớn")}
 sum:=sha256.Sum256(data)
 if !equalHash(sum[:],want){return "",errors.New("tệp cập nhật không khớp SHA-256")}
 dest:=filepath.Join(os.TempDir(),"XiaoZhi-PC-Setup-"+u.Version+".exe")
 if err=os.WriteFile(dest,data,0700);err!=nil{return "",err}
 return dest,nil
}
func equalHash(a,b []byte)bool{if len(a)!=len(b){return false};var x byte;for i:=range a{x|=a[i]^b[i]};return x==0}
func launchUpdateInstaller(path string)error{
 if runtime.GOOS!="windows"{return errors.New("unsupported OS")}
 // The installer runs independently. Inno Setup closes/replaces the existing application safely.
 cmd:=exec.Command(path,"/CLOSEAPPLICATIONS","/RESTARTAPPLICATIONS")
 return cmd.Start()
}
