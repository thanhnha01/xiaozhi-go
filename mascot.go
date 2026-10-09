package main

import (
 "bytes"
 "embed"
 "net/http"
 "strings"
 "time"
)

// Mascot files are packaged in the Windows binary, not loaded from beside the EXE.
// Only the six user-facing emotions are embedded. Remaining legacy files in assets/
// are ignored and can be cleaned up separately.
//go:embed assets/mascot/angry.gif assets/mascot/idle.gif assets/mascot/loving.gif assets/mascot/sad.gif assets/mascot/speaking.gif assets/mascot/thinking.gif
var mascotAssets embed.FS

var mascotFiles = map[string]bool{
 "idle.gif":true,
 "speaking.gif":true,
 "loving.gif":true,
 "sad.gif":true,
 "angry.gif":true,
 "thinking.gif":true,
}

var allowedEmotions = map[string]bool{
 "neutral":true, "happy":true, "laughing":true, "funny":true,
 "loving":true, "sad":true, "crying":true, "angry":true,
 "surprised":true, "shocked":true, "thinking":true,
 "confused":true, "embarrassed":true, "confident":true,
 "sleepy":true, "relaxed":true, "winking":true, "silly":true,
}

func normalizeEmotion(value string)string{
 emotion:=strings.ToLower(strings.TrimSpace(value))
 if !allowedEmotions[emotion]{return "neutral"}
 return emotion
}

func (d *Dashboard) setEmotion(value string){
 d.mu.Lock()
 d.emotion=normalizeEmotion(value)
 d.mu.Unlock()
}

func (d *Dashboard) serveMascot(w http.ResponseWriter,r *http.Request){
 if r.Method!=http.MethodGet && r.Method!=http.MethodHead{
  http.Error(w,"Sai phương thức",http.StatusMethodNotAllowed)
  return
 }
 const prefix="/assets/emotions/"
 if !strings.HasPrefix(r.URL.Path,prefix){http.NotFound(w,r);return}
 name:=strings.TrimPrefix(r.URL.Path,prefix)
 if !mascotFiles[name]||strings.ContainsAny(name,"/\\"){http.NotFound(w,r);return}
 data,err:=mascotAssets.ReadFile("assets/mascot/"+name)
 if err!=nil{http.NotFound(w,r);return}
 // Content sniffing supports old mislabeled files but tests reject non-GIF
 // for release builds, ensuring animation and native browser display.
 w.Header().Set("Content-Type","image/gif")
 http.ServeContent(w,r,name,time.Time{},bytes.NewReader(data))
}
