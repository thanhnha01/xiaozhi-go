package main

// ota.go 对应原版 main/ota.cc：版本检查（CheckVersion）+ 设备激活（Activate）。
// 这是设备绑定的核心：服务器按 Device-Id（MAC）判断设备是否已绑定账号，
// 未绑定的设备在 OTA 响应中获得 6 位激活码并播报，用户在小智 App 中输入
// 验证码完成绑定后，设备通过轮询 /activate 感知绑定完成。

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os/exec"
	"runtime"
	"time"
)

// 默认 OTA 地址，对应原版 CONFIG_OTA_URL（Kconfig.projbuild）。
const defaultOtaURL = "https://api.tenclass.net/xiaozhi/ota/"

// ActivationInfo 对应 OTA 响应中的 "activation" 对象（ota.cc:124-144）。
type ActivationInfo struct {
	Message   string `json:"message"`
	Code      string `json:"code"`
	Challenge string `json:"challenge"`
	TimeoutMS int    `json:"timeout_ms"`
}

// OtaCheckResponse 对应 CheckVersion 的完整响应。
type OtaCheckResponse struct {
	Activation *ActivationInfo  `json:"activation"`
	Websocket  *WebsocketConfig `json:"websocket"`
	Mqtt       *MqttConfig      `json:"mqtt"`
	ServerTime *ServerTimeInfo  `json:"server_time"`
	Firmware   *FirmwareInfo    `json:"firmware"`
}

type ServerTimeInfo struct {
	Timestamp      int64 `json:"timestamp"`
	TimezoneOffset int   `json:"timezone_offset"`
}

type FirmwareInfo struct {
	Version string `json:"version"`
	URL     string `json:"url"`
	Force   int    `json:"force"`
}

// checkVersion 对应原版 Ota::CheckVersion()（ota.cc:77-245）。
// POST 系统信息到 OTA 接口，解析激活码 / 协议配置 / 服务器时间 / 固件版本。
func checkVersion(client *http.Client, cfg *DeviceConfig) (*OtaCheckResponse, error) {
	url := cfg.OtaURL
	if url == "" {
		url = defaultOtaURL
	}
	if len(url) < 10 {
		return nil, fmt.Errorf("OTA 地址未正确配置: %q", url)
	}

	body, err := getSystemInfoJson(cfg)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	// 与原版 SetupHttp()（ota.cc:55-72）一致的请求头
	req.Header.Set("Activation-Version", "1")
	req.Header.Set("Device-Id", cfg.MacAddress)
	req.Header.Set("Client-Id", cfg.UUID)
	req.Header.Set("User-Agent", getUserAgent())
	lang := cfg.Language
	if lang == "" {
		lang = "zh-CN"
	}
	req.Header.Set("Accept-Language", lang)
	req.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("检查版本失败，HTTP 状态码: %d", resp.StatusCode)
	}

	var result OtaCheckResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("解析 OTA 响应失败: %w", err)
	}

	// 服务器下发的 websocket 配置持久化（对应原版写入 NVS "websocket" 命名空间）
	if result.Websocket != nil {
		log.Printf("收到服务器下发的 WebSocket 配置: url=%s, version=%d", result.Websocket.URL, result.Websocket.Version)
		cfg.Websocket = result.Websocket
	}
	// MQTT 配置暂存（PC 版使用 WebSocket 协议）
	if result.Mqtt != nil {
		log.Printf("收到服务器下发的 MQTT 配置: endpoint=%s", result.Mqtt.Endpoint)
		cfg.Mqtt = result.Mqtt
	}
	// 服务器时间：PC 系统时钟已同步，仅记录
	if result.ServerTime != nil {
		log.Printf("服务器时间: %d (时区偏移 %d 分钟)",
			result.ServerTime.Timestamp, result.ServerTime.TimezoneOffset)
	}
	if result.Firmware != nil {
		log.Printf("服务器固件版本: %s", result.Firmware.Version)
	}
	return &result, nil
}

// activate 对应原版 Ota::Activate()（ota.cc:458-492）。
// 轮询 /activate，200=绑定成功；202=等待用户输入验证码；其他=失败。
func activate(client *http.Client, cfg *DeviceConfig) error {
	url := cfg.OtaURL
	if url == "" {
		url = defaultOtaURL
	}
	if url[len(url)-1] != '/' {
		url += "/activate"
	} else {
		url += "activate"
	}

	// v1 无序列号设备发送空对象（原版 GetActivationPayload()，ota.cc:421-456）
	payload := []byte("{}")
	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(payload))
	if err != nil {
		return err
	}
	req.Header.Set("Activation-Version", "1")
	req.Header.Set("Device-Id", cfg.MacAddress)
	req.Header.Set("Client-Id", cfg.UUID)
	req.Header.Set("User-Agent", getUserAgent())
	req.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	switch resp.StatusCode {
	case http.StatusOK:
		return nil // 激活成功
	case http.StatusAccepted:
		return errActivationTimeout // 仍在等待
	default:
		return fmt.Errorf("激活失败，HTTP 状态码: %d", resp.StatusCode)
	}
}

var errActivationTimeout = fmt.Errorf("等待激活")

// RunActivation 对应原版 CheckNewVersion()（application.cc:417-493）中的
// "检查绑定 + 获取验证码 + 轮询绑定" 全流程：
//
//	CheckVersion 失败 → 指数退避重试（最多 10 次，初始 10s，翻倍）
//	CheckVersion 成功且有激活码 → 播报验证码，轮询 /activate：
//	  202 → 3s 后重试；其他失败 → 10s 后重试；最多 10 次
//	绑定完成后重新 CheckVersion（服务器已绑定则不再返回 activation）
func RunActivation(client *http.Client, cfg *DeviceConfig) error {
	const maxRetry = 10
	retryDelay := 10 * time.Second

	for attempt := 0; ; attempt++ {
		resp, err := checkVersion(client, cfg)
		if err != nil {
			if attempt >= maxRetry {
				return fmt.Errorf("检查版本重试次数耗尽: %w", err)
			}
			log.Printf("检查版本失败（第 %d/%d 次），%.0f 秒后重试: %v",
				attempt+1, maxRetry, retryDelay.Seconds(), err)
			time.Sleep(retryDelay)
			retryDelay *= 2
			continue
		}
		retryDelay = 10 * time.Second // 成功则重置退避

		// 已绑定设备：响应无 activation 段，直接进入正常流程
		if resp.Activation == nil ||
			(resp.Activation.Code == "" && resp.Activation.Challenge == "") {
			log.Println("设备已绑定，无需激活")
			return nil
		}

		// 未绑定：播报验证码并轮询等待用户绑定
		if resp.Activation.Code != "" {
			announceActivationCode(resp.Activation.Code, resp.Activation.Message)
		}

		activated := false
		for i := 0; i < 10; i++ {
			log.Printf("正在等待绑定... %d/10", i+1)
			err := activate(client, cfg)
			if err == nil {
				log.Println("设备绑定成功")
				activated = true
				break
			} else if err == errActivationTimeout {
				time.Sleep(3 * time.Second)
			} else {
				log.Printf("激活请求失败: %v（10 秒后重试）", err)
				time.Sleep(10 * time.Second)
			}
		}
		if !activated {
			log.Println("绑定等待超时，重新检查版本")
			continue
		}
		// 绑定完成后重新检查，确认服务器已记录绑定关系
		resp, err = checkVersion(client, cfg)
		if err != nil {
			return err
		}
		if resp.Activation == nil || resp.Activation.Code == "" {
			return nil
		}
		log.Println("绑定尚未生效，重新进入等待流程")
	}
}

// announceActivationCode 播报 6 位激活码。
// 对应原版 ShowActivationCode()（application.cc:655-677）：播放激活提示音后
// 逐位播放数字音。PC 版实现：终端醒目显示 + macOS 系统语音逐位朗读。
func announceActivationCode(code, message string) {
	fmt.Println()
	fmt.Println("╔══════════════════════════════════════════════════════════════╗")
	fmt.Println("║                      ⚠  设备尚未绑定账号                     ║")
	fmt.Println("╠══════════════════════════════════════════════════════════════╣")
	fmt.Println("║  请在「小智」App 中输入以下 6 位激活码完成设备绑定:          ║")
	fmt.Printf("║                                                            ║\n")
	fmt.Printf("║                        %s                        ║\n", formatCodeForBanner(code))
	fmt.Println("║                                                            ║")
	fmt.Println("║  绑定成功后本程序将自动继续。                               ║")
	fmt.Println("║  等待期间可随时按 Ctrl+C 退出。                             ║")
	fmt.Println("╚══════════════════════════════════════════════════════════════╝")
	fmt.Println()

	// macOS 系统语音逐位播报（对应原版逐位播放数字音 0.ogg~9.ogg）
	if runtime.GOOS == "darwin" {
		go speakDigits(code)
	}
}

// formatCodeForBanner 把验证码按 "1 2 3 4 5 6" 分隔居中显示。
func formatCodeForBanner(code string) string {
	var spaced string
	for i, r := range code {
		if i > 0 {
			spaced += " "
		}
		spaced += string(r)
	}
	return spaced
}

// speakDigits 用 macOS 系统语音逐位朗读验证码，异步执行防止阻塞主流程。
func speakDigits(code string) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	text := ""
	for _, r := range code {
		text += string(r) + " "
	}
	cmd := exec.CommandContext(ctx, "say", text)
	if out, err := cmd.CombinedOutput(); err != nil {
		log.Printf("语音播报验证码失败（将仅以终端显示）: %v %s", err, out)
	}
}
