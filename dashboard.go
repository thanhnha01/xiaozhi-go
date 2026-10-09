package main

// Vietnamese local-only dashboard. Device WSS != cloud MCP endpoint.
import (
 "crypto/rand"
 "embed"
 "encoding/hex"
 "encoding/json"
 "errors"
 "fmt"
 "io"
 "net"
 "net/http"
 "os/exec"
 "runtime"
 "strings"
 "sync"
 "time"
 "context"
)

//go:embed web/dashboard.html
var dashboardAssets embed.FS

type dashboardEvent struct {
 Kind string `json:"kind"`
 Text string `json:"text"`
 Time string `json:"time"`
}
type Dashboard struct {
 mu sync.RWMutex
 app *App
 listener net.Listener
 server *http.Server
 url, nonce, phase, errorMessage, activation string
 emotion string
 windowClose func()
 events []dashboardEvent
 ttsInProgress bool
 ttsEventIndex int
 logs []string
 update appUpdate
 updateBusy bool
}
func newDashboard(app *App) (*Dashboard,error) {
 secret:=make([]byte,24)
 if _,err:=rand.Read(secret);err!=nil{return nil,err}
 return &Dashboard{app:app,nonce:hex.EncodeToString(secret),phase:"starting",emotion:"neutral",events:[]dashboardEvent{},logs:[]string{}},nil
}
func (d *Dashboard) start() error {
 ln,err:=net.Listen("tcp","127.0.0.1:0")
 if err!=nil{return fmt.Errorf("không mở được giao diện cục bộ: %w",err)}
 d.listener=ln
 d.url="http://"+ln.Addr().String()
 d.server=&http.Server{Handler:d.handler(),ReadHeaderTimeout:5*time.Second}
 go func(){if err:=d.server.Serve(ln);err!=nil&&!errors.Is(err,http.ErrServerClosed){d.setError("Giao diện gặp lỗi: "+err.Error())}}()
 go d.checkUpdates()
 if err:=openDesktopWindow(d);err!=nil{d.event("system","Không mở được cửa sổ ứng dụng; chuyển sang trình duyệt: "+err.Error()); if fallbackErr:=openLocalBrowser(d.url);fallbackErr!=nil{d.event("system","Truy cập giao diện tại: "+d.url)}}
 return nil
}
func (d *Dashboard) close(){if d.server!=nil{_ = d.server.Close()}}
func (d *Dashboard) setWindowClose(fn func()) {
 d.mu.Lock()
 d.windowClose=fn
 d.mu.Unlock()
}
// requestQuit closes the native window first, then the window message loop
// signals quitChan. Browser fallback has no native window and quits directly.
func (d *Dashboard) requestQuit() {
 d.mu.RLock()
 fn:=d.windowClose
 d.mu.RUnlock()
 if fn!=nil {fn();return}
 d.app.quitOnce.Do(func(){close(d.app.quitChan)})
}
func openLocalBrowser(address string) error {
 var cmd *exec.Cmd
 switch runtime.GOOS {
 case "windows":cmd=exec.Command("rundll32","url.dll,FileProtocolHandler",address)
 case "darwin":cmd=exec.Command("open",address)
 default:cmd=exec.Command("xdg-open",address)
 }
 return cmd.Start()
}
func (d *Dashboard) setPhase(phase string){
 d.mu.Lock();d.phase=phase
 if phase=="ready"{d.activation="";d.errorMessage=""}
 d.mu.Unlock()
}
func (d *Dashboard) setActivation(code string){
 d.mu.Lock();d.activation=code;d.phase="activating";d.mu.Unlock()
 d.event("system","Thiết bị chưa được liên kết. Nhập mã kích hoạt tại xiaozhi.me.")
}
func (d *Dashboard) setError(message string){
 d.mu.Lock();d.errorMessage=message;d.phase="error";d.mu.Unlock()
 d.event("system","Có lỗi: "+message)
}
func (d *Dashboard) event(kind,message string){
 if message==""{return}
 d.mu.Lock()
 d.events=append(d.events,dashboardEvent{Kind:kind,Text:message,Time:time.Now().Format("15:04:05")})
 d.trimEventHistoryLocked()
 d.mu.Unlock()
}
// Write captures technical logs; the primary screen displays localized events.
func (d *Dashboard) Write(p []byte)(int,error){
 d.mu.Lock()
 for _,line:=range strings.Split(strings.TrimSpace(string(p)),"\n"){if line!=""{d.logs=append(d.logs,line)}}
 if len(d.logs)>150{d.logs=d.logs[len(d.logs)-150:]}
 d.mu.Unlock()
 return len(p),nil
}
func (d *Dashboard) handler() http.Handler{
 mux:=http.NewServeMux()
 mux.HandleFunc("/assets/emotions/",d.serveMascot)
 mux.HandleFunc("/",func(w http.ResponseWriter,r *http.Request){
  if r.URL.Path!="/"||r.Method!=http.MethodGet{http.NotFound(w,r);return}
  body,err:=dashboardAssets.ReadFile("web/dashboard.html")
  if err!=nil{http.Error(w,"Không tìm thấy giao diện",500);return}
  w.Header().Set("Content-Type","text/html; charset=utf-8")
  _,_=w.Write([]byte(strings.ReplaceAll(string(body),"{{LOCAL_TOKEN}}",d.nonce)))
 })
 mux.HandleFunc("/api/status",func(w http.ResponseWriter,r *http.Request){
  if r.Method!=http.MethodGet{http.Error(w,"Sai phương thức",405);return}
  d.mu.RLock()
  phase,failure,activation,emotion:=d.phase,d.errorMessage,d.activation,d.emotion
  events:=append([]dashboardEvent{},d.events...)
  logs:=append([]string{},d.logs...)
  update,busy:=d.update,d.updateBusy
  d.mu.RUnlock()
  state:=d.app.sm.Current().String()
  volume:=float32(0.7)
  micLevel:=uint32(0)
  if d.app.audio!=nil {
   volume=d.app.audio.Volume()
   if state=="listening" {micLevel=d.app.audio.MicLevel()}
  }
  w.Header().Set("Content-Type","application/json; charset=utf-8")
  _=json.NewEncoder(w).Encode(map[string]any{
   "phase":phase,"state":state,"emotion":emotion,"recording":state=="listening","activation":activation,
   "error":failure,"update":update,"update_busy":busy,"app_version":appVersion,"mac":d.app.cfg.MacAddress,"volume":volume,"events":events,"logs":logs,
   "mic_level":micLevel,"music":func()MusicStatus{if d.app.music!=nil{return d.app.music.Status()};return MusicStatus{State:"stopped"}}(),"mic_frames_sent":d.app.micFramesSent.Load(),
   "mic_frames_dropped":d.app.micFramesDropped.Load(),
  })
 })
 mux.HandleFunc("/api/action",func(w http.ResponseWriter,r *http.Request){
  if r.Method!=http.MethodPost{http.Error(w,"Sai phương thức",405);return}
  if r.Header.Get("X-Xiaozhi-Token")!=d.nonce||(r.Header.Get("Origin")!=""&&r.Header.Get("Origin")!=d.url){http.Error(w,"Không được phép",403);return}
  var req struct {Action string `json:"action"`;Value float32 `json:"value"`;Song string `json:"song"`;Artist string `json:"artist"`}
  if err:=json.NewDecoder(io.LimitReader(r.Body,2048)).Decode(&req);err!=nil{http.Error(w,"Dữ liệu không hợp lệ",400);return}
  err:=d.performAction(req.Action,req.Value)
  if req.Action=="install_update"{err=d.startUpdate()}
  if req.Action=="music_play" && d.app.music!=nil {err=d.app.music.Play(req.Song,req.Artist)}
  w.Header().Set("Content-Type","application/json; charset=utf-8")
  if err!=nil{
   w.WriteHeader(http.StatusConflict)
   _=json.NewEncoder(w).Encode(map[string]string{"error":err.Error()})
   return
  }
  _=json.NewEncoder(w).Encode(map[string]bool{"ok":true})
 })
 return http.HandlerFunc(func(w http.ResponseWriter,r *http.Request){
  if d.listener!=nil&&r.Host!=d.listener.Addr().String(){http.Error(w,"Địa chỉ không hợp lệ",403);return}
  w.Header().Set("Cache-Control","no-store")
  w.Header().Set("X-Content-Type-Options","nosniff")
  w.Header().Set("X-Frame-Options","DENY")
  w.Header().Set("Content-Security-Policy","default-src 'none'; connect-src 'self'; script-src 'unsafe-inline'; style-src 'unsafe-inline'; img-src 'self' data:; base-uri 'none'; frame-ancestors 'none'")
  mux.ServeHTTP(w,r)
 })
}
func (d *Dashboard) performAction(action string,value float32)error{
 if action=="quit"{d.requestQuit();return nil}
 if d.app.music!=nil {
 switch action {
 case "music_stop":return d.app.music.Stop()
 case "music_pause":return d.app.music.Pause()
 case "music_resume":return d.app.music.Resume()
 }
 }
 if action=="volume"{
  if d.app.audio==nil{return fmt.Errorf("âm thanh chưa sẵn sàng")}
  if value<0||value>1{return fmt.Errorf("âm lượng phải từ 0 đến 100%%")}
  d.app.audio.SetVolume(value);return nil
 }
 d.mu.RLock();ready:=d.phase=="ready";d.mu.RUnlock()
 if !ready||d.app.proto==nil||!d.app.proto.IsConnected(){return fmt.Errorf("thiết bị chưa kết nối, vui lòng chờ")}
 switch action {
 case "start":d.app.startListening();d.event("system","Đã yêu cầu bật micro.")
 case "stop":d.app.stopListening();d.event("system","Đã yêu cầu tắt micro.")
 case "abort":
  d.app.stopListening()
  if err:=d.app.proto.SendAbortSpeaking();err!=nil{return err}
  d.app.audio.ClearOutput();d.app.sm.TransitionTo(StateIdle)
  d.event("system","Đã yêu cầu ngắt câu trả lời.")
 default:return fmt.Errorf("thao tác không được hỗ trợ")
 }
 return nil
}

func(d *Dashboard) checkUpdates(){
 ctx,cancel:=context.WithTimeout(context.Background(),15*time.Second)
 defer cancel()
 update,err:=checkAppUpdate(ctx)
 d.mu.Lock()
 if err==nil{d.update=update}
 d.mu.Unlock()
}
func(d *Dashboard) startUpdate()error{
 d.mu.Lock()
 if d.updateBusy{d.mu.Unlock();return fmt.Errorf("đang tải bản cập nhật")}
 if !d.update.Available{d.mu.Unlock();return fmt.Errorf("không có bản cập nhật")}
 u:=d.update;d.updateBusy=true
 d.mu.Unlock()
 go func(){
  ctx,cancel:=context.WithTimeout(context.Background(),4*time.Minute)
  defer cancel()
  path,err:=downloadVerifiedUpdate(ctx,u)
  if err==nil{err=launchUpdateInstaller(path)}
  if err!=nil{
   d.mu.Lock();d.updateBusy=false;d.mu.Unlock()
   d.event("system","Cập nhật thất bại: "+err.Error())
   return
  }
  d.app.quitOnce.Do(func(){close(d.app.quitChan)})
 }()
 return nil
}
