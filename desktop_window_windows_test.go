//go:build windows

package main
import (
 "os"
 "path/filepath"
 "strings"
 "testing"
)

func TestWebViewProfileOutsidePortableDirectory(t *testing.T){
 p,err:=webViewProfilePath()
 if err!=nil{t.Fatal(err)}
 if !filepath.IsAbs(p){t.Fatalf("profile not absolute: %s",p)}
 if !strings.Contains(strings.ToLower(p),strings.ToLower(filepath.Join("XiaoZhiPC","WebView2"))) {
  t.Fatalf("unexpected profile: %s",p)
 }
 if fi,err:=os.Stat(p);err!=nil||!fi.IsDir(){t.Fatalf("profile not writable directory: %v",err)}
}

func TestWebViewWindowHasBrandedIcon(t *testing.T) {
 opts := xiaoZhiWindowOptions()
 if opts.IconId != 1 { t.Fatalf("window icon resource ID = %d; want 1",opts.IconId) }
 if opts.Title == "" { t.Fatal("missing window title") }
}
