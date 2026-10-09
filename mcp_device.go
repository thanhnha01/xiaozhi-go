package main

// Device-side MCP JSON-RPC responses. Cloud MCP tools registered in xiaozhi.me
// execute on the Xiaozhi backend, not inside this PC device simulator.
import (
    "encoding/json"
    "fmt"
    "log"
 "strings"
)

type deviceMCPRequest struct {
    JSONRPC string          `json:"jsonrpc"`
    ID      json.RawMessage `json:"id"`
    Method  string          `json:"method"`
 Params json.RawMessage `json:"params"`
}

func deviceMCPResponse(payload []byte) (string, error) {
 return deviceMCPResponseWithPlayer(payload,nil)
}

func deviceMCPResponseWithPlayer(payload []byte,player *MusicPlayer) (string, error) {
    var request deviceMCPRequest
    if err := json.Unmarshal(payload, &request); err != nil {
        return "", fmt.Errorf("invalid device MCP JSON: %w", err)
    }
    if request.Method == "" {
        return "", fmt.Errorf("MCP request has no method")
    }
    // JSON-RPC notifications do not have an id and MUST NOT be replied to.
    if len(request.ID) == 0 || string(request.ID) == "null" {
        return "", nil
    }
    response := map[string]any{"jsonrpc": "2.0", "id": request.ID}
    switch request.Method {
    case "initialize":
        response["result"] = map[string]any{
            "protocolVersion": "2024-11-05",
            "capabilities": map[string]any{"tools": map[string]any{}},
            "serverInfo": map[string]any{"name": appName, "version": appVersion},
        }
    case "tools/list":
        // Do not advertise cloud MCP tools as local IoT tools.
        response["result"] = map[string]any{"tools": musicDeviceTools(player)}
    case "ping":
        response["result"] = map[string]any{}
    case "tools/call":
        if player==nil{response["error"]=map[string]any{"code":-32601,"message":"No device music player"};break}
        var call struct{Name string `json:"name"`;Arguments map[string]any `json:"arguments"`}
        if err:=json.Unmarshal(request.Params,&call);err!=nil{response["error"]=map[string]any{"code":-32602,"message":"Invalid tool parameters"};break}
        var err error
        switch call.Name {
        case "self.music.play_song":
            song,_:=call.Arguments["song_name"].(string);artist,_:=call.Arguments["artist_name"].(string)
            err=player.Play(song,artist)
        case "self.music.pause":err=player.Pause()
        case "self.music.resume":err=player.Resume()
        case "self.music.stop":err=player.Stop()
        case "self.music.get_status":
        default:response["error"]=map[string]any{"code":-32601,"message":"Unknown local music tool"};break
        }
        if _,unknown:=response["error"];unknown{break}
        value:=map[string]any{"ok":err==nil,"status":player.Status()}
        if err!=nil{value["error"]=err.Error()}
        bb,_:=json.Marshal(value)
        response["result"]=map[string]any{"content":[]any{map[string]string{"type":"text","text":string(bb)}},"isError":err!=nil}
    default:
        response["error"] = map[string]any{
            "code": -32601,
            "message": "Local device MCP method not available: " + request.Method,
        }
    }
    data, err := json.Marshal(response)
    if err != nil {
        return "", err
    }
    return string(data), nil
}

func (app *App) handleMcp(payload []byte) {
    response, err := deviceMCPResponseWithPlayer(payload,app.music)
    if err != nil {
        log.Printf("Không thể xử lý device MCP: %v", err)
        return
    }
    if response == "" {
        return
    }
    if err := app.proto.SendMcpMessage(response); err != nil {
        log.Printf("Không thể gửi phản hồi device MCP: %v", err)
    }
}

func musicDeviceTools(p *MusicPlayer)[]any{
 if p==nil{return []any{}}
 defs:=[]struct{name,description string;properties map[string]any;required []string}{
  {"self.music.play_song","Phát nhạc online trên loa PC theo tên bài hát và ca sĩ. Tải trực tiếp từ Cloudflare Worker, không dùng cho nhạc SD.",map[string]any{"song_name":map[string]string{"type":"string"},"artist_name":map[string]string{"type":"string"}},[]string{"song_name"}},
  {"self.music.pause","Tạm dừng phát nhạc online.",map[string]any{},nil},
  {"self.music.resume","Tiếp tục phát nhạc online.",map[string]any{},nil},
  {"self.music.stop","Dừng phát nhạc online.",map[string]any{},nil},
  {"self.music.get_status","Kiểm tra trạng thái nhạc trên loa PC.",map[string]any{},nil},
 }
 tools:=make([]any,0,len(defs))
 for _,d:=range defs{
  if strings.TrimSpace(d.name)==""{continue}
  tools=append(tools,map[string]any{"name":d.name,"description":d.description,"inputSchema":map[string]any{"type":"object","properties":d.properties,"required":d.required,"additionalProperties":false}})
 }
 return tools
}
