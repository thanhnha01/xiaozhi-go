// xiaozhi-go 小智对话助手（PC 版）
//
// 基于 xiaozhi-esp32（github.com/78/xiaozhi-esp32）协议用 Go 实现：
//   - 设备绑定：首次启动生成真实随机 MAC + UUID 并持久化，通过 OTA 接口
//     检查绑定状态；未绑定设备播报 6 位激活码并轮询等待用户在
//     小智 App 中输入验证码完成绑定。
//   - 语音对话：麦克风 Opus 编码上行，服务器 TTS 音频解码播放下行。
//   - 完整状态机与消息协议（hello/listen/abort/mcp/tts/stt/llm/system/alert）。
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"time"
)

// listenTimeout 监听超时秒数（0 = 不自动停止），对应原版 VAD 静音检测的作用。
var listenTimeout int

// App 应用主体，串联设备配置、状态机、协议客户端与音频管线。
type App struct {
	cfg   *DeviceConfig
	sm    *StateMachine
	proto *ProtocolClient
	audio *AudioManager
	http  *http.Client

	// 监听状态控制
	stateMu        sync.Mutex
	isRecording    bool
	listeningTimer *time.Timer

	quitChan chan struct{}
	quitOnce sync.Once
}

func main() {
	log.SetFlags(log.LstdFlags | log.Lshortfile)

	var cfgPath string
	var otaURL string
	flag.StringVar(&cfgPath, "config", "device_config.json", "设备配置文件路径")
	flag.StringVar(&otaURL, "ota", "", "OTA 服务器地址（默认 https://api.tenclass.net/xiaozhi/ota/）")
	flag.IntVar(&listenTimeout, "listen-timeout", 10, "自动停止监听的超时秒数（0 为不自动停止）")
	flag.Parse()
	configPath = cfgPath

	// ---------- 1. 加载/生成本设备身份（MAC + UUID） ----------
	cfg, err := loadConfig()
	if err != nil {
		log.Fatalf("加载配置失败: %v", err)
	}
	if otaURL != "" {
		cfg.OtaURL = otaURL
	}
	ensureDeviceIdentity(cfg)
	if err := cfg.saveConfig(); err != nil {
		log.Fatalf("保存配置失败: %v", err)
	}
	log.Printf("设备身份: MAC=%s, UUID=%s", cfg.MacAddress, cfg.UUID)

	app := &App{
		cfg:      cfg,
		sm:       NewStateMachine(),
		http:     &http.Client{Timeout: 15 * time.Second},
		quitChan: make(chan struct{}),
	}
	app.sm.TransitionTo(StateStarting)

	// ---------- 2. 初始化音频 ----------
	audio, err := InitAudio()
	if err != nil {
		log.Fatalf("初始化音频失败: %v", err)
	}
	app.audio = audio
	defer audio.Close()
	audio.SetStateCallback(func() State { return app.sm.Current() })

	// ---------- 3. 设备绑定检查（对应原版 ActivationTask） ----------
	app.sm.TransitionTo(StateActivating)
	log.Println("正在检查设备绑定状态...")
	if err := RunActivation(app.http, cfg); err != nil {
		log.Fatalf("激活流程失败: %v", err)
	}
	if err := cfg.saveConfig(); err != nil {
		log.Fatalf("保存配置失败: %v", err)
	}
	app.sm.TransitionTo(StateIdle)

	// 启动麦克风采集流
	if err := audio.StartInput(); err != nil {
		log.Fatalf("初始化音频输入失败: %v", err)
	}

	// ---------- 4. 建立 WebSocket 会话 ----------
	app.proto = NewProtocolClient(cfg)
	audio.SetSendCallback(func(frame []byte) {
		// 发送音频帧（毫秒时间戳，对应原版 AudioStreamPacket.timestamp）
		if err := app.proto.SendAudioFrame(frame, uint32(time.Now().UnixNano()/1e6)); err != nil {
			log.Printf("发送音频失败: %v", err)
		}
	})
	// 接线服务器消息与音频回调
	app.proto.onMessage = app.handleMessage
	app.proto.onAudio = func(payload []byte, _ uint32) {
		if pcm := app.audio.DecodeAudio(payload); pcm != nil {
			app.audio.PushPcm(pcm)
		}
	}
	if err := app.connect(); err != nil {
		log.Fatalf("连接服务器失败: %v", err)
	}

	showCommandMenu()

	// ---------- 5. 启动各协程 ----------
	go app.receiveLoop()
	go app.startKeyboardInput()

	// ---------- 6. 等待退出 ----------
	interrupt := make(chan os.Signal, 1)
	signal.Notify(interrupt, os.Interrupt)
	select {
	case <-app.quitChan:
		log.Println("程序退出")
	case <-interrupt:
		log.Println("收到中断信号，退出...")
	}
	app.proto.Close()
}

// connect 建立 WebSocket 连接并完成 hello 握手（对应原版 OpenAudioChannel）。
func (app *App) connect() error {
	app.sm.TransitionTo(StateConnecting)
	hello, err := app.proto.Connect()
	if err != nil {
		app.sm.TransitionTo(StateIdle)
		return err
	}
	// 按服务器下发的采样率初始化下行音频解码/播放
	rate := 0
	if hello.AudioParams != nil {
		rate = hello.AudioParams.SampleRate
	}
	if err := app.audio.SetupDecoder(rate); err != nil {
		app.sm.TransitionTo(StateIdle)
		return fmt.Errorf("初始化音频输出失败: %w", err)
	}
	app.sm.TransitionTo(StateIdle)
	log.Println("连接就绪，可以开始对话")
	return nil
}

// receiveLoop 接收服务器消息；断线后指数退避自动重连。
func (app *App) receiveLoop() {
	backoff := 1 * time.Second
	for {
		err := app.proto.ReadLoop()
		if err != nil {
			log.Printf("连接断开: %v", err)
		}
		// 网络断开时关闭当前音频通道（对应原版 HandleNetworkDisconnectedEvent）
		app.stateMu.Lock()
		app.stopListeningLocked()
		app.stateMu.Unlock()
		app.sm.TransitionTo(StateIdle)

		select {
		case <-app.quitChan:
			return
		case <-time.After(backoff):
		}
		log.Printf("%.0f 秒后重连...", backoff.Seconds())
		app.proto.Close()
		if err := app.connect(); err == nil {
			backoff = 1 * time.Second
			continue
		}
		if backoff < 30*time.Second {
			backoff *= 2
		}
	}
}

// handleMessage 处理服务器 JSON 消息（对应原版 OnIncomingJson，application.cc:543-650）。
func (app *App) handleMessage(msg *Message) {
	switch msg.Type {
	case "tts":
		switch msg.State {
		case "start":
			app.sm.TransitionTo(StateSpeaking)
			log.Println("开始播放 TTS")
			app.audio.ClearOutput()
		case "stop":
			app.sm.TransitionTo(StateIdle)
			log.Println("TTS 播放结束")
			// 回复结束后自动进入监听（对应原版 auto 模式）
			app.startListening()
		case "sentence_start":
			log.Printf("字幕: %s", msg.Text)
		}
	case "stt":
		log.Printf("识别结果: %s", msg.Text)
	case "llm":
		log.Printf("LLM: 情感=%s 文本=%s", msg.Emotion, msg.Text)
	case "mcp":
		app.handleMcp(msg.Payload)
	case "system":
		log.Printf("系统命令: %s（PC 版忽略重启，保持会话）", msg.Command)
	case "alert":
		log.Printf("告警: [%s] %s", msg.Status, msg.Text)
	case "goodbye":
		log.Println("服务器结束会话")
		app.stateMu.Lock()
		app.stopListeningLocked()
		app.stateMu.Unlock()
		app.sm.TransitionTo(StateIdle)
	default:
		log.Printf("未知消息类型: %s", msg.Type)
	}
}

// handleMcp 处理 MCP JSON-RPC 消息（原版 McpServer::ParseMessage）。
// PC 版无 IoT 硬件，记录并回显结果，避免服务器等待超时。
func (app *App) handleMcp(payload []byte) {
	if len(payload) == 0 {
		log.Println("收到空的 MCP 消息")
		return
	}
	log.Printf("MCP 消息: %s", string(payload))

	// 解析 JSON-RPC 请求，回显其 id
	var req struct {
		ID json.RawMessage `json:"id"`
	}
	if err := json.Unmarshal(payload, &req); err != nil || len(req.ID) == 0 {
		return
	}
	resp := fmt.Sprintf(`{"jsonrpc":"2.0","id":%s,"result":{"ok":true,"message":"xiaozhi-go: 工具不受支持"}}`,
		req.ID)
	if err := app.proto.SendMcpMessage(resp); err != nil {
		log.Printf("发送 MCP 响应失败: %v", err)
	}
}

// startListening 开始监听（对应原版 SendStartListening，manual 模式）。
func (app *App) startListening() {
	app.stateMu.Lock()
	defer app.stateMu.Unlock()
	app.startListeningLocked()
}

func (app *App) startListeningLocked() {
	if app.sm.Current() != StateIdle {
		return
	}
	if !app.proto.IsConnected() {
		log.Println("未连接服务器")
		return
	}
	if err := app.proto.SendStartListening("manual"); err != nil {
		log.Printf("发送 listen start 失败: %v", err)
		return
	}
	app.sm.TransitionTo(StateListening)
	app.isRecording = true
	app.resetListeningTimer()
	log.Println("开始监听（按空格停止）")
}

// stopListening 停止监听。
func (app *App) stopListening() {
	app.stateMu.Lock()
	defer app.stateMu.Unlock()
	app.stopListeningLocked()
}

func (app *App) stopListeningLocked() {
	if !app.isRecording {
		return
	}
	if app.listeningTimer != nil {
		app.listeningTimer.Stop()
		app.listeningTimer = nil
	}
	if app.proto.IsConnected() {
		if err := app.proto.SendStopListening(); err != nil {
			log.Printf("发送 listen stop 失败: %v", err)
		}
	}
	app.isRecording = false
	app.sm.TransitionTo(StateIdle)
	log.Println("停止监听")
}

// resetListeningTimer 设置监听超时自动停止（替代原版 VAD 静音检测）。
func (app *App) resetListeningTimer() {
	if listenTimeout <= 0 {
		return
	}
	if app.listeningTimer != nil {
		app.listeningTimer.Stop()
	}
	app.listeningTimer = time.AfterFunc(time.Duration(listenTimeout)*time.Second, func() {
		app.stateMu.Lock()
		defer app.stateMu.Unlock()
		if app.isRecording {
			log.Printf("%d 秒无操作，自动停止监听", listenTimeout)
			app.stopListeningLocked()
		}
	})
}

// startKeyboardInput 绑定键盘操作。
func (app *App) startKeyboardInput() {
	startKeyboardInput(
		func() { // 空格：空闲时开始监听，监听时停止，播放时打断
			app.stateMu.Lock()
			defer app.stateMu.Unlock()
			switch app.sm.Current() {
			case StateListening:
				app.stopListeningLocked()
			case StateIdle:
				app.startListeningLocked()
			case StateSpeaking:
				if app.proto.IsConnected() {
					app.proto.SendAbortSpeaking()
				}
				app.audio.ClearOutput()
				app.sm.TransitionTo(StateIdle)
				log.Println("已打断对话")
			}
		},
		func() { app.startListening() },
		func() { app.stopListening() },
		func() { // 发送唤醒词
			if app.sm.Current() != StateListening {
				log.Println("请先开始监听")
				return
			}
			if err := app.proto.SendWakeWordDetected("你好小智"); err != nil {
				log.Printf("发送唤醒词失败: %v", err)
			}
		},
		func() { // 中止对话
			if err := app.proto.SendAbortSpeaking(); err != nil {
				log.Printf("发送 abort 失败: %v", err)
			}
			app.audio.ClearOutput()
			app.sm.TransitionTo(StateIdle)
			log.Println("已中止对话")
		},
		func() { // 发送 MCP 测试消息
			payload := `{"jsonrpc":"2.0","method":"tools/call","params":{"name":"ping","arguments":{}},"id":1}`
			if err := app.proto.SendMcpMessage(payload); err != nil {
				log.Printf("发送 MCP 消息失败: %v", err)
			} else {
				log.Printf("已发送 MCP 消息: %s", payload)
			}
		},
		func(delta float32) { app.audio.SetVolume(app.audio.Volume() + delta) },
		func() { app.quitOnce.Do(func() { close(app.quitChan) }) },
	)
}
