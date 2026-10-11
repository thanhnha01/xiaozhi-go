package main

import (
 "strings"
 "time"
)

// markChatSent records a local WS write, NOT proof that Xiaozhi accepted it.
func(d *Dashboard) markChatSent(text string){
 d.mu.Lock()
 d.chatMonitorID++
 id:=d.chatMonitorID
 d.chatLastTyped=text
 d.chatSentAt=time.Now()
 d.chatEchoSeen=false
 d.chatStatus="sent"
 d.events=append(d.events,dashboardEvent{Kind:"chat",Text:text,Time:time.Now().Format("15:04:05")})
 d.trimEventHistoryLocked()
 d.mu.Unlock()
 go func(){
  timer:=time.NewTimer(25*time.Second)
  defer timer.Stop()
  select{
  case <-timer.C:
  case <-d.app.quitChan:return
  }
  d.expireChatWait(id)
 }()
}

func(d *Dashboard) expireChatWait(id uint64){
 d.mu.Lock()
 defer d.mu.Unlock()
 if id!=d.chatMonitorID||d.chatStatus=="complete"||d.chatStatus=="error"||d.chatStatus=="timeout" {return}
 notice:="Chưa nhận được phản hồi STT/TTS từ máy chủ sau 25 giây. Kết nối WebSocket vẫn có thể đang mở nhưng server không xử lý listen/detect. Hãy thử hỏi cùng câu bằng micro."
 if d.chatStatus=="recognized" {notice="Máy chủ đã trả STT nhưng chưa bắt đầu TTS sau 25 giây; kiểm tra TTS, MCP hoặc nhật ký kết nối."}
 if d.chatStatus=="speaking" {return} // TTS may be reading a long answer.
 d.chatStatus="timeout"
 d.events=append(d.events,dashboardEvent{Kind:"chat_notice",Text:notice,Time:time.Now().Format("15:04:05")})
 d.trimEventHistoryLocked()
}

func(d *Dashboard) onChatServerEvent(kind string){
 d.mu.Lock()
 defer d.mu.Unlock()
 if d.chatMonitorID==0{return}
 switch kind{
 case "stt":
  if d.chatStatus=="sent"||d.chatStatus=="timeout"{d.chatStatus="recognized"}
 case "tts_start":
  if d.chatStatus!="complete"{d.chatStatus="speaking"}
 case "tts_stop":
  if d.chatStatus=="sent"||d.chatStatus=="recognized"||d.chatStatus=="speaking"||d.chatStatus=="timeout"{d.chatStatus="complete"}
 case "alert","goodbye":
  if d.chatStatus=="sent"||d.chatStatus=="recognized"{d.chatStatus="error"}
 }
}

// Suppress the second copy of typed text echoed as server STT, without hiding
// voice STT or STT with different text.
func(d *Dashboard) consumeChatEcho(text string)bool{
 d.mu.Lock()
 defer d.mu.Unlock()
 if d.chatEchoSeen||d.chatSentAt.IsZero()||time.Since(d.chatSentAt)>time.Minute{return false}
 if strings.TrimSpace(text)!=d.chatLastTyped{return false}
 d.chatEchoSeen=true
 return true
}
