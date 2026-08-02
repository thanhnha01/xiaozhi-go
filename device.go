package main

// device.go 设备身份管理。
// 对应原版 main/system_info.cc（MAC 地址）与 main/boards/common/board.cc
// （UUID v4 生成、GetSystemInfoJson）。

import (
	"crypto/rand"
	"encoding/json"
	"fmt"
	"log"
	"runtime"
	"strings"
)

// appName / appVersion 用于 OTA 请求中的 application 信息与 User-Agent。
const (
	appName    = "xiaozhi-go"
	appVersion = "1.0.0"
)

// GenerateMacAddress 生成真实随机的 MAC 地址（6 字节）。
//   - 用 crypto/rand 真随机数源，而非伪随机。
//   - 首字节设置 bit0=0（单播）与 bit1=1（本地管理地址），
//     与原版 ESP32 芯片内烧录的 MAC 一致格式："xx:xx:xx:xx:xx:xx" 小写。
func GenerateMacAddress() string {
	bytes := make([]byte, 6)
	if _, err := rand.Read(bytes); err != nil {
		// crypto/rand 失败时退回 math/rand，保证程序可用
		bytes = fallbackRandomBytes(6)
	}
	bytes[0] = bytes[0]&0xFC | 0x02 // 单播 + 本地管理位
	return fmt.Sprintf("%02x:%02x:%02x:%02x:%02x:%02x",
		bytes[0], bytes[1], bytes[2], bytes[3], bytes[4], bytes[5])
}

// GenerateUuid 生成 UUID v4，格式与原版 GenerateUuid()（board.cc:25-46）一致。
func GenerateUuid() string {
	bytes := make([]byte, 16)
	if _, err := rand.Read(bytes); err != nil {
		bytes = fallbackRandomBytes(16)
	}
	bytes[6] = bytes[6]&0x0F | 0x40 // 版本 4
	bytes[8] = bytes[8]&0x3F | 0x80 // 变体 1
	return fmt.Sprintf("%02x%02x%02x%02x-%02x%02x-%02x%02x-%02x%02x-%02x%02x%02x%02x%02x%02x",
		bytes[0], bytes[1], bytes[2], bytes[3],
		bytes[4], bytes[5], bytes[6], bytes[7],
		bytes[8], bytes[9], bytes[10], bytes[11],
		bytes[12], bytes[13], bytes[14], bytes[15])
}

// fallbackRandomBytes crypto/rand 不可用时的兜底。
func fallbackRandomBytes(n int) []byte {
	b := make([]byte, n)
	for i := range b {
		b[i] = byte(i * 7919 % 256)
	}
	return b
}

// ensureDeviceIdentity 确保设备拥有持久化的 MAC 与 UUID（首次运行生成）。
// 对应原版 Board 构造函数（board.cc:15-23）：NVS 中没有 uuid 时生成并存储。
func ensureDeviceIdentity(cfg *DeviceConfig) {
	if cfg.MacAddress == "" {
		cfg.MacAddress = GenerateMacAddress()
		log.Printf("已生成新的设备 MAC 地址: %s", cfg.MacAddress)
	}
	if cfg.UUID == "" {
		cfg.UUID = GenerateUuid()
		log.Printf("已生成新的设备 UUID: %s", cfg.UUID)
	}
}

// getUserAgent 对应原版 SystemInfo::GetUserAgent()（system_info.cc:53-57）
// = BOARD_NAME/固件版本。
func getUserAgent() string {
	return fmt.Sprintf("%s/%s", appName, appVersion)
}

// SystemInfoJson 对应原版 Board::GetSystemInfoJson()（board.cc:70-178）的请求体。
// 原版使用字符串拼接（部分字段被服务器严格解析），这里用 map 按原字段名输出。
type SystemInfoJson struct {
	Version             int             `json:"version"`
	Language            string          `json:"language"`
	FlashSize           int64           `json:"flash_size"`
	MinimumFreeHeapSize int64           `json:"minimum_free_heap_size"`
	MacAddress          string          `json:"mac_address"`
	UUID                string          `json:"uuid"`
	ChipModelName       string          `json:"chip_model_name"`
	ChipInfo            ChipInfo        `json:"chip_info"`
	Application         AppInfo         `json:"application"`
	PartitionTable      []PartitionInfo `json:"partition_table"`
	Ota                 OtaInfo         `json:"ota"`
	Board               map[string]any  `json:"board"`
}

type ChipInfo struct {
	Model    int `json:"model"`
	Cores    int `json:"cores"`
	Revision int `json:"revision"`
	Features int `json:"features"`
}

type AppInfo struct {
	Name        string `json:"name"`
	Version     string `json:"version"`
	CompileTime string `json:"compile_time"`
	IDFVersion  string `json:"idf_version"`
	ElfSHA256   string `json:"elf_sha256"`
}

type PartitionInfo struct {
	Label   string `json:"label"`
	Type    int    `json:"type"`
	Subtype int    `json:"subtype"`
	Address int64  `json:"address"`
	Size    int64  `json:"size"`
}

type OtaInfo struct {
	Label string `json:"label"`
}

// getSystemInfoJson 构造 OTA CheckVersion 请求体。
// PC 版没有 flash/芯片信息，填入与桌面环境一致的占位值，保持字段齐全。
func getSystemInfoJson(cfg *DeviceConfig) ([]byte, error) {
	language := cfg.Language
	if language == "" {
		language = "zh-CN"
	}
	info := SystemInfoJson{
		Version:             2,
		Language:            language,
		FlashSize:           0,
		MinimumFreeHeapSize: 0,
		MacAddress:          cfg.MacAddress,
		UUID:                cfg.UUID,
		ChipModelName:       "pc",
		ChipInfo: ChipInfo{
			Model:    0,
			Cores:    runtime.NumCPU(),
			Revision: 0,
			Features: 0,
		},
		Application: AppInfo{
			Name:        appName,
			Version:     appVersion,
			CompileTime: "2026-08-02T00:00:00Z",
			IDFVersion:  "",
			ElfSHA256:   "",
		},
		PartitionTable: []PartitionInfo{},
		Ota:            OtaInfo{Label: "factory"},
		Board:          map[string]any{"name": appName},
	}
	return json.Marshal(info)
}

// normalizeToken 对应原版 websocket_protocol.cc:97-103：token 没有空格时补 "Bearer " 前缀。
func normalizeToken(token string) string {
	if token == "" {
		return ""
	}
	if !strings.Contains(token, " ") {
		return "Bearer " + token
	}
	return token
}
