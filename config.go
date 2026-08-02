package main

// config.go 负责设备配置的持久化管理。
// 对应原版 xiaozhi-esp32 的 NVS 存储（main/settings.cc + ota.cc 中
// "board"、"websocket"、"mqtt" 等命名空间），这里用单个 JSON 文件实现。

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// WebsocketConfig 对应原版 NVS "websocket" 命名空间，由 OTA CheckVersion
// 响应中的 "websocket" 对象写入（main/ota.cc:167-186），WebSocket 协议
// 初始化时读取（main/protocols/websocket_protocol.cc:80-83）。
type WebsocketConfig struct {
	URL     string `json:"url"`
	Token   string `json:"token"`
	Version int    `json:"version"`
}

// MqttConfig 对应原版 NVS "mqtt" 命名空间，OTA 响应可能同时下发 mqtt 配置。
// PC 版暂不使用 MQTT 协议，仅保存（原版协议选择逻辑：有 websocket 优先 websocket）。
type MqttConfig struct {
	Endpoint       string `json:"endpoint"`
	ClientID       string `json:"client_id"`
	Username       string `json:"username"`
	Password       string `json:"password"`
	Keepalive      int    `json:"keepalive"`
	PublishTopic   string `json:"publish_topic"`
	SubscribeTopic string `json:"subscribe_topic"`
}

// DeviceConfig 设备全局配置，替代 NVS。
type DeviceConfig struct {
	// MacAddress 对应原版 SystemInfo::GetMacAddress()（esp_read_mac）。
	// 首次启动生成真实随机 MAC 后持久化，是服务器识别设备身份的唯一标识。
	MacAddress string `json:"mac_address"`
	// UUID 对应原版 Board::GenerateUuid()，首次开机生成存入 NVS "board/uuid"
	// （main/boards/common/board.cc:15-23），作为 Client-Id。
	UUID string `json:"uuid"`
	// Websocket 服务器下发的 WebSocket 配置。
	Websocket *WebsocketConfig `json:"websocket,omitempty"`
	// Mqtt 服务器下发的 MQTT 配置（暂存）。
	Mqtt *MqttConfig `json:"mqtt,omitempty"`
	// 用户配置：服务器地址 / OTA 地址 / 语言等，可被命令行覆盖。
	OtaURL   string `json:"ota_url,omitempty"`
	Language string `json:"language,omitempty"`
}

// 默认配置文件路径：可执行文件同目录下的 device_config.json。
var configPath = "device_config.json"

// loadConfig 读取配置文件；文件不存在或损坏时返回一个空配置（由调用方决定是否重建）。
func loadConfig() (*DeviceConfig, error) {
	data, err := os.ReadFile(configPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return &DeviceConfig{}, nil
		}
		return nil, err
	}
	cfg := &DeviceConfig{}
	if err := json.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("解析配置文件 %s 失败: %w", configPath, err)
	}
	return cfg, nil
}

// saveConfig 原子写入配置（先写临时文件再重命名，避免断电/崩溃损坏）。
func (c *DeviceConfig) saveConfig() error {
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	dir := filepath.Dir(configPath)
	if dir != "." && dir != "" {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
	}
	tmp := configPath + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, configPath)
}
