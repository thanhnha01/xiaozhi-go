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
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"sync"
	"sync/atomic"
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
 music *MusicPlayer
	http  *http.Client

	// 监听状态控制
	stateMu        sync.Mutex
	isRecording    bool
	listeningTimer *time.Timer

	quitChan chan struct{}
	quitOnce sync.Once
	dashboard *Dashboard
	micFramesSent atomic.Uint64
	micFramesDropped atomic.Uint64
}

func main() {
	log.SetFlags(log.LstdFlags | log.Lshortfile)

	var cfgPath string
	var otaURL string
	var directWS string
	var directToken string
	var directVersion int
	var consoleOnly bool
 var musicURL string
	flag.StringVar(&cfgPath, "config", "", "设备配置文件路径")
	flag.StringVar(&otaURL, "ota", "", "OTA 服务器地址（默认 https://api.tenclass.net/xiaozhi/ota/）")
	flag.StringVar(&directWS, "ws", "", "设备 WebSocket URL（不是 MCP Server URL；设置后跳过 OTA 激活）")
	flag.StringVar(&directToken, "ws-token", os.Getenv("XIAOZHI_WS_TOKEN"), "设备 WebSocket token（推荐通过环境变量 XIAOZHI_WS_TOKEN 设置）")
	flag.IntVar(&directVersion, "ws-version", 1, "直连设备 WebSocket 协议版本（1, 2 或 3）")
	flag.IntVar(&listenTimeout, "listen-timeout", 0, "自动停止监听的超时秒数（0 为不自动停止）")
	flag.StringVar(&musicURL, "music-url", "https://xiaozhi-master.nguyennha-020201.workers.dev", "HTTPS music Worker base URL")
 flag.BoolVar(&consoleOnly, "console", false, "Chạy giao diện dòng lệnh cũ")
	flag.Parse()
	if !consoleOnly {
        release, alreadyRunning, err := claimGUIInstance()
        if err != nil { log.Fatalf("Không thể kiểm tra phiên ứng dụng: %v", err) }
        if alreadyRunning { return }
        defer release()
    }
    configPath = resolveConfigPath(cfgPath)

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
	if !consoleOnly {
		dashboard, err := newDashboard(app)
		if err != nil { log.Fatalf("Không tạo được giao diện: %v", err) }
		app.dashboard = dashboard
		log.SetOutput(io.MultiWriter(os.Stderr, dashboard))
		if err := dashboard.start(); err != nil { log.Fatalf("Không thể mở giao diện: %v", err) }
		defer dashboard.close()
		fmt.Println("Giao diện XiaoZhi tiếng Việt:", dashboard.url)
	}

	// ---------- 2. 初始化音频 ----------
	audio, err := InitAudio()
	if err != nil {
		if app.dashboard != nil { app.dashboard.setError("Không khởi tạo được âm thanh: "+err.Error()); app.waitForQuit(); return }
		log.Fatalf("Không khởi tạo được âm thanh: %v",err)
	}
	app.audio = audio
 app.music=NewMusicPlayer(audio,musicURL)
 defer app.music.Stop()
	defer audio.Close()
	audio.SetStateCallback(func() State { return app.sm.Current() })

	// ---------- 3. 设备绑定检查（对应原版 ActivationTask） ----------
	app.sm.TransitionTo(StateActivating)
	if directWS != "" {
		u, err := url.Parse(directWS)
		if err != nil || (u.Scheme != "ws" && u.Scheme != "wss") || u.Host == "" || u.User != nil {
			log.Fatalf("设备 WebSocket URL 不合法（应为 ws:// 或 wss://，不要填 MCP endpoint）")
		}
		if directVersion < 1 || directVersion > 3 {
			log.Fatal("ws-version 必须为 1、2 或 3")
		}
		// 直连只修改本次运行的连接参数；不覆盖已保存的 OTA 设备配置。
		cfg.Websocket = &WebsocketConfig{URL: directWS, Token: directToken, Version: directVersion}
		log.Printf("直连设备 WebSocket: %s (v%d)；跳过 OTA 激活", u.Host, directVersion)
	} else {
		log.Println("正在检查设备绑定状态...")
		var onActivation func(string)
		if app.dashboard != nil { onActivation=app.dashboard.setActivation }
		if err := RunActivationWithCallback(app.http, cfg, onActivation); err != nil {
			if app.dashboard != nil { app.dashboard.setError("Không kích hoạt được thiết bị: "+err.Error()); app.waitForQuit(); return }; log.Fatalf("Kích hoạt thất bại: %v", err)
		}
		if err := cfg.saveConfig(); err != nil {
			log.Fatalf("保存配置失败: %v", err)
		}
	}
	app.sm.TransitionTo(StateIdle)

	// 启动麦克风采集流
	if err := audio.StartInput(); err != nil {
		if app.dashboard != nil { app.dashboard.setError("Không mở được microphone: "+err.Error()); app.waitForQuit(); return }
		log.Fatalf("Không mở được microphone: %v",err)
	}

	// ---------- 4. 建立 WebSocket 会话 ----------
	app.proto = NewProtocolClient(cfg)
	// Never block the real-time PortAudio callback on a WebSocket network write.
	frames := make(chan []byte, 16)
	audio.SetSendCallback(func(frame []byte) {
		select {
		case frames <- frame:
		default:
			app.micFramesDropped.Add(1)
		}
	})
	go func() {
		for {
			select {
			case <-app.quitChan:
				return
			case frame := <-frames:
				// Discard queued frames once the listen session has ended.
				if app.sm.Current() != StateListening { continue }
				if err := app.proto.SendAudioFrame(frame, uint32(time.Now().UnixMilli())); err != nil {
					app.micFramesDropped.Add(1)
					log.Printf("Lỗi gửi âm thanh microphone: %v", err)
				} else {
					app.micFramesSent.Add(1)
				}
			}
		}
	}()
	// 接线服务器消息与音频回调
	app.proto.onMessage = app.handleMessage
	app.proto.onAudio = func(payload []byte, _ uint32) {
		if pcm := app.audio.DecodeAudio(payload); pcm != nil {
			app.audio.PushPcm(pcm)
		}
	}
	if err := app.connect(); err != nil {
		if app.dashboard != nil { app.dashboard.setError("Kết nối máy chủ thất bại: "+err.Error()); app.waitForQuit(); return }
		log.Fatalf("Kết nối máy chủ thất bại: %v",err)
	}
	if app.dashboard != nil { app.dashboard.setPhase("ready");app.dashboard.event("system","Đã kết nối với máy chủ Xiaozhi. Bạn có thể bắt đầu nói.") }


	if consoleOnly { showCommandMenu() }

	// ---------- 5. 启动各协程 ----------
	go app.receiveLoop()
	if consoleOnly {go app.startKeyboardInput()}

	// ---------- 6. 等待退出 ----------
	interrupt := make(chan os.Signal, 1)
	signal.Notify(interrupt, os.Interrupt)
	select {
	case <-app.quitChan:
		log.Println("程序退出")
	case <-interrupt:
		log.Println("Đã nhận tín hiệu thoát, đang đóng cửa sổ và kết nối...")
		if app.dashboard!=nil {
			app.dashboard.requestQuit()
			<-app.quitChan
		}
	}
	// Gracefully release microphone, speaker, device WebSocket, and GUI HTTP
	// server before the process exits; no application process is left behind.
	app.stateMu.Lock()
	if app.isRecording { app.stopListeningLocked() }
	app.stateMu.Unlock()
	app.proto.Close()
	if app.dashboard!=nil { app.dashboard.close() }
}
func (app *App) waitForQuit(){
	interrupt := make(chan os.Signal, 1)
	signal.Notify(interrupt, os.Interrupt)
	select {case <-app.quitChan: case <-interrupt:}
}

// connect 建立 WebSocket 连接并完成 hello 握手（对应原版 OpenAudioChannel）。
func (app *App) connect() error {
	app.sm.TransitionTo(StateConnecting)
	if app.dashboard != nil {app.dashboard.setPhase("connecting")}
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
	if app.dashboard != nil {app.dashboard.setPhase("ready")}
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
			if app.dashboard != nil {app.dashboard.setPhase("connecting");app.dashboard.event("system","Mất kết nối, đang thử kết nối lại...")}
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
			// GUI uses explicit push-to-talk. Do not reopen microphone after
			// TTS stops unless the user presses "Bắt đầu nói" again.
			if app.dashboard == nil { app.startListening() }
		case "sentence_start":
			log.Printf("字幕: %s", msg.Text)
			if app.dashboard != nil {app.dashboard.event("tts",msg.Text)}
		}
	case "stt":
		log.Printf("识别结果: %s", msg.Text)
		if app.dashboard != nil {app.dashboard.event("stt",msg.Text)}
	case "llm":
		log.Printf("LLM: 情感=%s 文本=%s", msg.Emotion, msg.Text)
		if app.dashboard != nil {app.dashboard.setEmotion(msg.Emotion);app.dashboard.event("llm",msg.Text)}
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
	app.micFramesSent.Store(0)
	app.micFramesDropped.Store(0)
	app.resetListeningTimer()
	log.Println("Đang nghe qua microphone")
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
	if app.audio != nil { app.audio.micLevel.Store(0) }
	app.sm.TransitionTo(StateIdle)
	log.Println("Đã dừng microphone")
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
		func() { // 服务端 MCP 工具通过语音指令触发，不向设备 WS 发送伪造的 tools/call。
			log.Printf("设备 WebSocket: %s; 已连接: %t", app.proto.wsURL(), app.proto.IsConnected())
			log.Println("要测试 Cloudflare/Render MCP，请先在 xiaozhi.me 绑定服务器 MCP，再用麦克风提问触发工具；按 5 只显示诊断信息。")
		},
		func(delta float32) { app.audio.SetVolume(app.audio.Volume() + delta) },
		func() { app.quitOnce.Do(func() { close(app.quitChan) }) },
	)
}
