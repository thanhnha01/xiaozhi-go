package main

// Online music plays directly from the configured Cloudflare Worker to the
// PC's existing PortAudio output. Render/MCP carries commands, never MP3 bytes.
import (
 "context"
 "encoding/binary"
 "encoding/json"
 "errors"
 "fmt"
 "io"
 "net/http"
 "net/url"
 "regexp"
 "strconv"
 "sort"
 "math"
 "strings"
 "sync"
 "time"

 "github.com/hajimehoshi/go-mp3"
)

const defaultMusicURL="https://xiaozhi-master.nguyennha-020201.workers.dev"

type MusicStatus struct {
 State string `json:"state"`
 Title string `json:"title"`
 Artist string `json:"artist"`
 Error string `json:"error,omitempty"`
 PositionSeconds int64 `json:"position_seconds"`
 DurationSeconds int64 `json:"duration_seconds"`
 Lyrics []LyricLine `json:"lyrics,omitempty"`
}

type LyricLine struct { TimeSeconds float64 `json:"time_seconds"`; Text string `json:"text"` }
var lrcTag = regexp.MustCompile(`\[(\d{1,3}):(\d{2})(?:\.(\d{1,3}))?\]`)
func parseLyrics(body string) []LyricLine {
 var lines []LyricLine
 for _,raw:=range strings.Split(strings.ReplaceAll(body,"\r",""),"\n") {
  matches:=lrcTag.FindAllStringSubmatch(raw,-1)
  text:=strings.TrimSpace(lrcTag.ReplaceAllString(raw,""))
  if text=="" || strings.HasPrefix(text,"[") {continue}
  if len(matches)==0 {lines=append(lines,LyricLine{TimeSeconds:-1,Text:text});continue}
  for _,m:=range matches {mi,_:=strconv.Atoi(m[1]);sec,_:=strconv.Atoi(m[2]);frac:=0.0;if len(m[3])>0{v,_:=strconv.Atoi(m[3]);frac=float64(v)/math.Pow10(len(m[3]))};lines=append(lines,LyricLine{TimeSeconds:float64(mi*60+sec)+frac,Text:text})}
 }
 sort.SliceStable(lines,func(i,j int)bool{return lines[i].TimeSeconds<lines[j].TimeSeconds})
 if len(lines)>400 {lines=lines[:400]}
 return lines
}

type MusicPlayer struct {
 mu sync.Mutex
 audio *AudioManager
 client *http.Client
 baseURL string
 cancel context.CancelFunc
 generation uint64
 status MusicStatus
 streams sync.WaitGroup
 closed bool
}

func NewMusicPlayer(audio *AudioManager,baseURL string)*MusicPlayer{
 return &MusicPlayer{
  audio:audio,baseURL:strings.TrimRight(baseURL,"/"),
  client:&http.Client{Timeout:15*time.Second,CheckRedirect:func(req *http.Request,via []*http.Request)error{
   if len(via)>=3{return errors.New("quá nhiều lần chuyển hướng")}
   if req.URL.Scheme!="https"{return errors.New("chỉ chấp nhận chuyển hướng HTTPS")}
   if len(via)>0&&req.URL.Host!=via[0].URL.Host{return errors.New("chuyển hướng sang domain ngoài")}
   return nil
  }},
  status:MusicStatus{State:"stopped"},
 }
}

func musicEndpoint(base,endpoint string)(*url.URL,error){
 u,err:=url.Parse(base)
 if err!=nil||u.Scheme!="https"||u.Host==""||u.User!=nil||u.RawQuery!=""||u.Fragment!=""{return nil,errors.New("music server phải là HTTPS URL hợp lệ")}
 if endpoint!="/stream_pcm"&&endpoint!="/proxy_audio"&&endpoint!="/proxy_lyric"{return nil,errors.New("music endpoint không hợp lệ")}
 u.Path=strings.TrimRight(u.Path,"/")+endpoint
 u.RawPath=""
 return u,nil
}

// Resolve only the worker's proxy audio route, never a third-party URL from
// an untrusted music search result. Workers generate the upstream auth.
func musicAudioURL(base,raw string)(string,error){
 root,err:=musicEndpoint(base,"/proxy_audio");if err!=nil{return "",err}
 ref,err:=url.Parse(strings.TrimSpace(raw))
 if err!=nil||ref==nil||ref.User!=nil||ref.Fragment!=""{return "",errors.New("đường dẫn MP3 không hợp lệ")}
 // Absolute URLs and scheme-relative redirects are deliberately not accepted.
 if ref.IsAbs()||ref.Host!=""||!strings.HasPrefix(ref.Path,"/") {return "",errors.New("MP3 phải dùng đường dẫn nội bộ Worker")}
 if ref.Path!=root.Path{return "",errors.New("MP3 phải đi qua /proxy_audio của Worker")}
 if ref.Query().Get("path")==""&&ref.Query().Get("id")==""{return "",errors.New("thiếu tham số nguồn nhạc")}
 root.RawQuery=ref.RawQuery
 return root.String(),nil
}

type musicMetadata struct{
 Title string `json:"title"`
 Artist string `json:"artist"`
 AudioURL string `json:"audio_url"`
 LyricURL string `json:"lyric_url"`
 Duration float64 `json:"duration"`
}
func (m *MusicPlayer) lookup(ctx context.Context,song,artist string)(musicMetadata,string,error){
 var meta musicMetadata
 u,err:=musicEndpoint(m.baseURL,"/stream_pcm");if err!=nil{return meta,"",err}
 q:=u.Query();q.Set("song",song);q.Set("artist",artist);u.RawQuery=q.Encode()
 req,err:=http.NewRequestWithContext(ctx,http.MethodGet,u.String(),nil);if err!=nil{return meta,"",err}
 req.Header.Set("Accept","application/json")
 resp,err:=m.client.Do(req);if err!=nil{return meta,"",fmt.Errorf("không thể tìm nhạc: %w",err)}
 defer resp.Body.Close()
 if resp.StatusCode!=200{return meta,"",fmt.Errorf("music server trả HTTP %d",resp.StatusCode)}
 body:=io.LimitReader(resp.Body,512*1024)
 if err:=json.NewDecoder(body).Decode(&meta);err!=nil{return meta,"",fmt.Errorf("JSON nhạc không hợp lệ: %w",err)}
 if meta.AudioURL==""{return meta,"",errors.New("không tìm thấy bài hát hoặc đường dẫn MP3")}
 audioURL,err:=musicAudioURL(m.baseURL,meta.AudioURL)
 return meta,audioURL,err
}

func (m *MusicPlayer) Play(song,artist string)error{
 m.mu.Lock();if m.closed {m.mu.Unlock();return errors.New("trình phát đã đóng")};m.mu.Unlock()
 song=strings.TrimSpace(song);artist=strings.TrimSpace(artist)
 if song==""||len([]rune(song))>160||len([]rune(artist))>160{return errors.New("tên bài hát/ca sĩ không hợp lệ")}
 if m.audio==nil{return errors.New("âm thanh chưa sẵn sàng")}
 ctx,cancel:=context.WithTimeout(context.Background(),15*time.Second)
 defer cancel()
 meta,audioURL,err:=m.lookup(ctx,song,artist)
 if err!=nil{return err}
 m.mu.Lock()
 if m.closed{m.mu.Unlock();return errors.New("trình phát đã đóng")}
 if m.cancel!=nil{m.cancel()}
 playbackCtx,playbackCancel:=context.WithCancel(context.Background())
 m.cancel=playbackCancel;m.generation++
 gen:=m.generation
 title:=strings.TrimSpace(meta.Title);if title==""{title=song}
 who:=strings.TrimSpace(meta.Artist);if who==""{who=artist}
 duration:=int64(math.Round(meta.Duration));if duration<0||duration>86400{duration=0}
 m.status=MusicStatus{State:"buffering",Title:title,Artist:who,DurationSeconds:duration}
 m.audio.BeginMusic(gen)
 m.streams.Add(1)
 if meta.LyricURL!="" {m.streams.Add(1)}
 m.mu.Unlock()
 go func(){defer m.streams.Done();m.stream(playbackCtx,gen,audioURL)}()
 if meta.LyricURL!="" {go func(){defer m.streams.Done();m.fetchLyrics(playbackCtx,gen,meta.LyricURL)}()}
 return nil
}

func (m *MusicPlayer) update(gen uint64,fn func(*MusicStatus)){
 m.mu.Lock();defer m.mu.Unlock()
 if m.generation==gen{fn(&m.status)}
}
func (m *MusicPlayer) stream(ctx context.Context,gen uint64,audioURL string){
 req,err:=http.NewRequestWithContext(ctx,http.MethodGet,audioURL,nil)
 if err!=nil{m.fail(gen,err);return}
 req.Header.Set("Accept","audio/mpeg,application/octet-stream")
 // Stream requests need no global deadline: context cancellation stops them.
 client:=*m.client;client.Timeout=0
 resp,err:=client.Do(req)
 if err!=nil{if ctx.Err()==nil{m.fail(gen,err)};return}
 defer resp.Body.Close()
 if resp.StatusCode!=200&&resp.StatusCode!=206{m.fail(gen,fmt.Errorf("MP3 trả HTTP %d",resp.StatusCode));return}
 typ:=strings.ToLower(resp.Header.Get("Content-Type"))
 if strings.Contains(typ,"json")||strings.Contains(typ,"text/html"){m.fail(gen,errors.New("music proxy không trả MP3"));return}
 dec,err:=mp3.NewDecoder(resp.Body)
 if err!=nil{m.fail(gen,fmt.Errorf("không giải mã MP3: %w",err));return}
 rate:=m.audio.OutputRate();if rate<=0{m.fail(gen,errors.New("loa chưa khởi tạo"));return}
 resampler:=newMusicResampler(dec.SampleRate(),rate)
 frameSize:=rate*frameDurationMs/1000
 if frameSize<=0{m.fail(gen,errors.New("sample rate không hợp lệ"));return}
 buf:=make([]byte,16384)
 var tail []byte
 var frames []int16
 var played int64
 for{
  if ctx.Err()!=nil{return}
  n,readErr:=dec.Read(buf)
  if n>0{
   bytes:=append(tail,buf[:n]...)
   aligned:=(len(bytes)/4)*4
   mono:=make([]int16,0,aligned/4)
   for i:=0;i<aligned;i+=4{
    left:=int16(binary.LittleEndian.Uint16(bytes[i:i+2]))
    right:=int16(binary.LittleEndian.Uint16(bytes[i+2:i+4]))
    mono=append(mono,int16((int32(left)+int32(right))/2))
   }
   tail=append(tail[:0],bytes[aligned:]...)
   frames=append(frames,resampler.Convert(mono)...)
   for len(frames)>=frameSize{
    chunk:=append([]int16(nil),frames[:frameSize]...)
    if !m.audio.PushMusicFrame(ctx,gen,chunk){return}
    played+=int64(frameDurationMs)
    m.update(gen,func(s *MusicStatus){if s.State=="buffering"{s.State="playing"};s.PositionSeconds=m.audio.MusicElapsedSeconds()})
    frames=frames[frameSize:]
   }
  }
  if readErr!=nil{
   if errors.Is(readErr,io.EOF){
    if len(frames)>0{
     padded:=make([]int16,frameSize);copy(padded,frames)
     if !m.audio.PushMusicFrame(ctx,gen,padded){return}
    }
    m.update(gen,func(s *MusicStatus){s.State="finished"})
   }else if ctx.Err()==nil{m.fail(gen,readErr)}
   return
  }
 }
}
func (m *MusicPlayer) fail(gen uint64,err error){
 m.update(gen,func(s *MusicStatus){s.State="error";s.Error=err.Error()})
}
func (m *MusicPlayer) fetchLyrics(ctx context.Context,gen uint64,raw string) {
 u,err:=musicProxyLyricURL(m.baseURL,raw);if err!=nil{return}
 req,err:=http.NewRequestWithContext(ctx,http.MethodGet,u,nil);if err!=nil{return}
 client:=*m.client;client.Timeout=12*time.Second
 resp,err:=client.Do(req);if err!=nil{return};defer resp.Body.Close()
 if resp.StatusCode!=200{return}
 b,err:=io.ReadAll(io.LimitReader(resp.Body,256*1024));if err!=nil{return}
 body:=string(b)
 if strings.Contains(resp.Header.Get("Content-Type"),"json") {
  var v struct {Lyric string `json:"lyric"`; Lyrics string `json:"lyrics"`; Data struct{Lyric string `json:"lyric"`} `json:"data"`}
  if json.Unmarshal(b,&v)!=nil{return};body=v.Lyric;if body==""{body=v.Lyrics};if body==""{body=v.Data.Lyric}
 }
 lines:=parseLyrics(body)
 m.update(gen,func(s *MusicStatus){s.Lyrics=lines})
}
func musicProxyLyricURL(base,raw string)(string,error){
 root,err:=musicEndpoint(base,"/proxy_lyric");if err!=nil{return "",err}
 ref,err:=url.Parse(strings.TrimSpace(raw));if err!=nil||ref==nil||ref.IsAbs()||ref.Host!=""||ref.User!=nil||ref.Fragment!=""||ref.Path!=root.Path{return "",errors.New("lyric URL không hợp lệ")}
 if ref.Query().Get("path")==""&&ref.Query().Get("id")==""{return "",errors.New("lyric URL thiếu tham số")}
 root.RawQuery=ref.RawQuery;return root.String(),nil
}
func (m *MusicPlayer) Close() {
 m.mu.Lock();m.closed=true;if m.cancel!=nil{m.cancel();m.cancel=nil};m.generation++;m.audio.StopMusic(m.generation);m.mu.Unlock()
 m.streams.Wait()
}
func (m *MusicPlayer) Pause()error{
 m.mu.Lock();defer m.mu.Unlock()
 if m.status.State!="playing"&&m.status.State!="buffering"{return errors.New("không có nhạc đang phát")}
 m.status.State="paused";m.audio.SetMusicPaused(true);return nil
}
func (m *MusicPlayer) Resume()error{
 m.mu.Lock();defer m.mu.Unlock()
 if m.status.State!="paused"{return errors.New("nhạc chưa tạm dừng")}
 m.status.State="playing";m.audio.SetMusicPaused(false);return nil
}
func (m *MusicPlayer) Stop()error{
 m.mu.Lock();defer m.mu.Unlock()
 if m.cancel!=nil{m.cancel();m.cancel=nil}
 m.generation++;m.audio.StopMusic(m.generation)
 m.status=MusicStatus{State:"stopped"}
 return nil
}
func (m *MusicPlayer) Status()MusicStatus{
 m.mu.Lock();defer m.mu.Unlock()
 if m.audio!=nil && (m.status.State=="playing"||m.status.State=="paused"||m.status.State=="buffering"){m.status.PositionSeconds=m.audio.MusicElapsedSeconds()}
 result:=m.status;result.Lyrics=append([]LyricLine(nil),m.status.Lyrics...);return result
}

// Linear mono PCM resampler preserves fractional phase between MP3 chunks.
type musicResampler struct{
 ratio,position float64
 last int16
 primed bool
}
func newMusicResampler(src,dst int)*musicResampler{return &musicResampler{ratio:float64(src)/float64(dst)}}
func (r *musicResampler) Convert(src []int16)[]int16{
 if len(src)==0||r.ratio<=0{return nil}
 if !r.primed{r.last=src[0];r.primed=true;src=src[1:]}
 n:=len(src)
 if n==0{return nil}
 buf:=make([]int16,0,int(float64(n)/r.ratio)+2)
 for r.position<float64(n){
  i:=int(r.position);alpha:=r.position-float64(i)
  lo:=r.last;if i>0{lo=src[i-1]}
  hi:=src[i]
  buf=append(buf,int16(float64(lo)*(1-alpha)+float64(hi)*alpha))
  r.position+=r.ratio
 }
 r.position-=float64(n)
 r.last=src[n-1]
 return buf
}
