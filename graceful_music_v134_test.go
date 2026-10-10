package main

import (
 "context"
 "errors"
 "testing"
 "time"
)

func TestActivationWaitCanceledImmediately(t *testing.T) {
 ctx,cancel:=context.WithCancel(context.Background())
 cancel()
 start:=time.Now()
 if err:=activationWait(ctx,5*time.Minute);!errors.Is(err,context.Canceled){t.Fatalf("want canceled got %v",err)}
 if time.Since(start)>time.Second{t.Fatal("shutdown blocked activation retry")}
}
func TestLyricsParseLRCAndPlain(t *testing.T){
 lines:=parseLyrics("[00:01.50]Dòng đầu\n[00:03.2]Dòng sau")
 if len(lines)!=2 || lines[0].Text!="Dòng đầu" || lines[0].TimeSeconds!=1.5 || lines[1].TimeSeconds!=3.2{t.Fatalf("bad timed lyrics: %#v",lines)}
 plain:=parseLyrics("Dòng một\nDòng hai")
 if len(plain)!=2 || plain[0].TimeSeconds!=-1{t.Fatalf("bad plain lyrics: %#v",plain)}
}
func TestMusicProxyLyricsRejectUntrustedURL(t *testing.T){
 base:="https://example.com"
 for _,raw:=range []string{"https://evil.com/proxy_lyric?id=1","//evil.com/proxy_lyric?id=1","/proxy_audio?id=1","/proxy_lyric"}{
  if _,err:=musicProxyLyricURL(base,raw);err==nil{t.Errorf("accepted %q",raw)}
 }
 if _,err:=musicProxyLyricURL(base,"/proxy_lyric?path=%2Fapi%2Fproxy_lyric%3Fid%3D1");err!=nil{t.Fatal(err)}
}
