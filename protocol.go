package main

// protocol.go WebSocket 协议客户端。
// 忠实移植原版 main/protocols/websocket_protocol.cc + protocol.cc：
// - 握手 headers：Authorization / Protocol-Version / Device-Id / Client-Id
// - hello 能力协商，等待服务器 hello（10 秒超时）
// - 二进制音频：v1 裸 Opus；v2/v3 带包头（BinaryProtocol2/3，大端序）
// - 消息发送：listen(start/stop/detect)、abort、mcp

import (
	"encoding/binary"
	"encoding/json"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

// 默认 WebSocket 地址（服务器 OTA 响应未下发时使用，对应原版 CONFIG_WS_URL 场景）。
const defaultWsURL = "wss://api.tenclass.net/xiaozhi/v1/"

const (
	audioSampleRate   = 16000 // 音频采样率（Hz）
	audioChannels     = 1     // 单声道
	frameDurationMs   = 60    // 帧时长（ms）
	serverHelloTimout = 10 * time.Second
)

// AudioParams 对应 hello 消息的 audio_params 字段。
type AudioParams struct {
	Format        string `json:"format"`
	SampleRate    int    `json:"sample_rate"`
	Channels      int    `json:"channels"`
	FrameDuration int    `json:"frame_duration"`
}

// Message 对应协议 JSON 消息（覆盖 hello/listen/abort/mcp/tts/stt/llm/system/alert/goodbye）。
type Message struct {
	Type        string           `json:"type"`
	Version     int              `json:"version,omitempty"`
	Transport   string           `json:"transport,omitempty"`
	AudioParams *AudioParams     `json:"audio_params,omitempty"`
	Features    *MessageFeatures `json:"features,omitempty"`
	SessionID   string           `json:"session_id,omitempty"`
	State       string           `json:"state,omitempty"`
	Mode        string           `json:"mode,omitempty"`
	Text        string           `json:"text,omitempty"`
	Reason      string           `json:"reason,omitempty"`
	Payload     json.RawMessage  `json:"payload,omitempty"`
	Emotion     string           `json:"emotion,omitempty"`
	Status      string           `json:"status,omitempty"`
	Command     string           `json:"command,omitempty"`
}

// MessageFeatures 对应 hello 的 features 字段（能力协商）。
// PC 版无 AEC/字体渲染硬件，只声明 mcp。
type MessageFeatures struct {
	AEC       bool `json:"aec"`
	MCP       bool `json:"mcp"`
	GlyphPush bool `json:"glyph_push"`
}

// ServerHello 服务器 hello 的解析结果。
type ServerHello struct {
	Transport   string
	SessionID   string
	AudioParams *AudioParams
}

// ProtocolClient 管理 WebSocket 连接与消息收发。
type ProtocolClient struct {
	mu        sync.Mutex // 保护 conn 与写入（gorilla/websocket 不支持并发写）
	conn      *websocket.Conn
	cfg       *DeviceConfig
	sessionID string
	// serverSampleRate / serverFrameDuration 来自服务器 hello（原版 ParseServerHello）
	serverSampleRate    int
	serverFrameDuration int
	// 接收回调
	onAudio   func(payload []byte, timestamp uint32)
	onMessage func(*Message)
}

func NewProtocolClient(cfg *DeviceConfig) *ProtocolClient {
	return &ProtocolClient{
		cfg:                 cfg,
		serverSampleRate:    audioSampleRate,
		serverFrameDuration: frameDurationMs,
	}
}

// version 返回协议版本（来自 OTA 下发的 websocket.version，默认 1）。
func (p *ProtocolClient) version() int {
	if p.cfg.Websocket != nil && p.cfg.Websocket.Version > 0 {
		return p.cfg.Websocket.Version
	}
	return 1
}

// wsURL 返回服务器 WebSocket 地址。
func (p *ProtocolClient) wsURL() string {
	if p.cfg.Websocket != nil && p.cfg.Websocket.URL != "" {
		return p.cfg.Websocket.URL
	}
	return defaultWsURL
}

// Connect 对应原版 OpenAudioChannel()（websocket_protocol.cc:79-190）：
// 建立连接 → 发送 hello → 等待服务器 hello（10 秒超时）。
func (p *ProtocolClient) Connect() (*ServerHello, error) {
	header := map[string][]string{
		"Authorization":    {normalizeToken(p.wsToken())},
		"Protocol-Version": {fmt.Sprintf("%d", p.version())},
		"Device-Id":        {p.cfg.MacAddress},
		"Client-Id":        {p.cfg.UUID},
	}

	conn, _, err := websocket.DefaultDialer.Dial(p.wsURL(), header)
	if err != nil {
		return nil, fmt.Errorf("WebSocket 连接失败: %w", err)
	}

	p.mu.Lock()
	p.conn = conn
	p.mu.Unlock()

	// 发送 hello 描述客户端能力
	if err := p.writeJSON(Message{
		Type:      "hello",
		Version:   p.version(),
		Transport: "websocket",
		Features:  &MessageFeatures{MCP: true},
		AudioParams: &AudioParams{
			Format:        "opus",
			SampleRate:    audioSampleRate,
			Channels:      audioChannels,
			FrameDuration: frameDurationMs,
		},
	}); err != nil {
		return nil, fmt.Errorf("发送 hello 失败: %w", err)
	}
	log.Printf("已发送 hello（协议版本 v%d）", p.version())

	// 等待服务器 hello
	conn.SetReadDeadline(time.Now().Add(serverHelloTimout))
	for {
		msgType, data, err := conn.ReadMessage()
		if err != nil {
			return nil, fmt.Errorf("等待服务器 hello 超时: %w", err)
		}
		if msgType != websocket.TextMessage {
			continue
		}
		var msg Message
		if err := json.Unmarshal(data, &msg); err != nil {
			continue
		}
		if msg.Type != "hello" {
			continue
		}
		hello := &ServerHello{Transport: msg.Transport, SessionID: msg.SessionID, AudioParams: msg.AudioParams}
		if hello.Transport != "websocket" {
			return nil, fmt.Errorf("服务器协议传输方式不匹配: %s", hello.Transport)
		}
		if hello.AudioParams != nil {
			if hello.AudioParams.SampleRate > 0 {
				p.serverSampleRate = hello.AudioParams.SampleRate
			}
			if hello.AudioParams.FrameDuration > 0 {
				p.serverFrameDuration = hello.AudioParams.FrameDuration
			}
		}
		p.sessionID = hello.SessionID
		conn.SetReadDeadline(time.Time{})
		log.Printf("服务器握手成功: session_id=%s, sample_rate=%d, frame_duration=%dms",
			hello.SessionID, p.serverSampleRate, p.serverFrameDuration)
		return hello, nil
	}
}

// wsToken 返回鉴权 token。
func (p *ProtocolClient) wsToken() string {
	if p.cfg.Websocket != nil {
		return p.cfg.Websocket.Token
	}
	return ""
}

// IsConnected 判断连接是否可用（对应原版 IsAudioChannelOpened）。
func (p *ProtocolClient) IsConnected() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.conn != nil
}

// Close 关闭连接（对应原版 CloseAudioChannel：WebSocket 不需要发送 goodbye）。
func (p *ProtocolClient) Close() {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.conn != nil {
		p.conn.Close()
		p.conn = nil
	}
}

// writeJSON 线程安全地发送 JSON 消息。
func (p *ProtocolClient) writeJSON(msg Message) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.conn == nil {
		return fmt.Errorf("连接未建立")
	}
	if err := p.conn.WriteJSON(msg); err != nil {
		return err
	}
	return nil
}

// writeBinary 线程安全地发送二进制音频帧。
func (p *ProtocolClient) writeBinary(data []byte) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.conn == nil {
		return fmt.Errorf("连接未建立")
	}
	return p.conn.WriteMessage(websocket.BinaryMessage, data)
}

// ---- 客户端消息（对应原版 protocol.cc 的发送方法）----

// SendStartListening 对应 SendStartListening()：mode 为 auto/manual/realtime。
func (p *ProtocolClient) SendStartListening(mode string) error {
	return p.writeJSON(Message{
		SessionID: p.sessionID,
		Type:      "listen",
		State:     "start",
		Mode:      mode,
	})
}

// SendStopListening 对应 SendStopListening()。
func (p *ProtocolClient) SendStopListening() error {
	return p.writeJSON(Message{
		SessionID: p.sessionID,
		Type:      "listen",
		State:     "stop",
	})
}

// SendWakeWordDetected 对应 SendWakeWordDetected()：上报唤醒词。
func (p *ProtocolClient) SendWakeWordDetected(wakeWord string) error {
	return p.writeJSON(Message{
		SessionID: p.sessionID,
		Type:      "listen",
		State:     "detect",
		Text:      wakeWord,
	})
}

// SendAbortSpeaking 对应 SendAbortSpeaking()：中止当前对话。
func (p *ProtocolClient) SendAbortSpeaking() error {
	return p.writeJSON(Message{
		SessionID: p.sessionID,
		Type:      "abort",
		Reason:    "wake_word_detected",
	})
}

// SendMcpMessage 对应 SendMcpMessage()：发送 MCP JSON-RPC 消息。
func (p *ProtocolClient) SendMcpMessage(payload string) error {
	return p.writeJSON(Message{
		SessionID: p.sessionID,
		Type:      "mcp",
		Payload:   json.RawMessage(payload),
	})
}

// SendAudioFrame 对应原版 SendAudio()（websocket_protocol.cc:24-56）：
// v1 裸 Opus 帧；v2/v3 按协议包头封装（大端序）。
func (p *ProtocolClient) SendAudioFrame(opusFrame []byte, timestamp uint32) error {
	version := p.version()
	var data []byte
	switch version {
	case 2:
		data = make([]byte, 16+len(opusFrame)) // BinaryProtocol2 头 16 字节
		binary.BigEndian.PutUint16(data[0:2], uint16(version))
		binary.BigEndian.PutUint16(data[2:4], 0) // type: 0 = OPUS
		binary.BigEndian.PutUint32(data[4:8], 0) // reserved
		binary.BigEndian.PutUint32(data[8:12], timestamp)
		binary.BigEndian.PutUint32(data[12:16], uint32(len(opusFrame)))
		copy(data[16:], opusFrame)
	case 3:
		data = make([]byte, 4+len(opusFrame)) // BinaryProtocol3 头 4 字节
		data[0] = 0                           // type: 0 = OPUS
		data[1] = 0                           // reserved
		binary.BigEndian.PutUint16(data[2:4], uint16(len(opusFrame)))
		copy(data[4:], opusFrame)
	default:
		data = opusFrame // v1 裸流
	}
	return p.writeBinary(data)
}

// ---- 接收循环 ----

// ReadLoop 阻塞读取服务器消息，对应原版 OnData 回调分发。
// 二进制消息解析 v1/v2/v3 三种格式（websocket_protocol.cc:108-140）。
func (p *ProtocolClient) ReadLoop() error {
	for {
		p.mu.Lock()
		conn := p.conn
		p.mu.Unlock()
		if conn == nil {
			return fmt.Errorf("连接已关闭")
		}

		msgType, data, err := conn.ReadMessage()
		if err != nil {
			return err
		}
		switch msgType {
		case websocket.TextMessage:
			var msg Message
			if err := json.Unmarshal(data, &msg); err != nil {
				log.Printf("解析 JSON 失败: %v, 数据: %s", err, data)
				continue
			}
			if p.onMessage != nil {
				p.onMessage(&msg)
			}
		case websocket.BinaryMessage:
			payload, timestamp := parseAudioFrame(data, p.version())
			if p.onAudio != nil && payload != nil {
				p.onAudio(payload, timestamp)
			}
		case websocket.CloseMessage:
			return fmt.Errorf("服务器关闭连接: %s", data)
		}
	}
}

// parseAudioFrame 按协议版本解析下行音频帧，返回 Opus 载荷与时间戳。
func parseAudioFrame(data []byte, version int) ([]byte, uint32) {
	if len(data) == 0 {
		return nil, 0
	}
	switch version {
	case 2: // BinaryProtocol2: version(2) type(2) reserved(4) timestamp(4) payload_size(4)
		if len(data) < 16 {
			return nil, 0
		}
		payloadSize := binary.BigEndian.Uint32(data[12:16])
		if len(data) < 16+int(payloadSize) {
			return nil, 0
		}
		return data[16 : 16+payloadSize], binary.BigEndian.Uint32(data[8:12])
	case 3: // BinaryProtocol3: type(1) reserved(1) payload_size(2)
		if len(data) < 4 {
			return nil, 0
		}
		payloadSize := binary.BigEndian.Uint16(data[2:4])
		if len(data) < 4+int(payloadSize) {
			return nil, 0
		}
		return data[4 : 4+payloadSize], 0
	default: // v1 裸 Opus 流
		return data, 0
	}
}
