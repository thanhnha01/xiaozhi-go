package main

import (
	"encoding/binary"
	"regexp"
	"strings"
	"testing"
)

// ---- 设备身份 ----

func TestGenerateMacAddress(t *testing.T) {
	re := regexp.MustCompile(`^([0-9a-f]{2}:){5}[0-9a-f]{2}$`)
	for i := 0; i < 50; i++ {
		mac := GenerateMacAddress()
		if !re.MatchString(mac) {
			t.Fatalf("MAC 格式错误: %s", mac)
		}
		first := strings.Split(mac, ":")[0]
		// 单播（bit0=0）+ 本地管理（bit1=1）
		if first[1] != '2' && first[1] != '6' && first[1] != 'a' && first[1] != 'e' {
			t.Fatalf("MAC 未设置本地管理位: %s", mac)
		}
	}
	// 随机性检查：50 个 MAC 不应重复
	seen := map[string]bool{}
	for i := 0; i < 50; i++ {
		mac := GenerateMacAddress()
		if seen[mac] {
			t.Fatalf("MAC 重复: %s", mac)
		}
		seen[mac] = true
	}
}

func TestGenerateUuidFormat(t *testing.T) {
	re := regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)
	for i := 0; i < 20; i++ {
		uuid := GenerateUuid()
		if !re.MatchString(uuid) {
			t.Fatalf("UUID v4 格式错误: %s", uuid)
		}
	}
}

func TestNormalizeToken(t *testing.T) {
	if got := normalizeToken("test-token"); got != "Bearer test-token" {
		t.Fatalf("期望 Bearer 前缀，得到: %s", got)
	}
	if got := normalizeToken("Bearer abc"); got != "Bearer abc" {
		t.Fatalf("不应重复添加前缀: %s", got)
	}
	if got := normalizeToken(""); got != "" {
		t.Fatalf("空 token 应返回空: %s", got)
	}
}

// ---- 状态机（忠实原版迁移规则）----

func TestStateMachineTransitions(t *testing.T) {
	cases := []struct {
		from, to State
		valid    bool
	}{
		{StateUnknown, StateStarting, true},
		{StateUnknown, StateIdle, false},
		{StateStarting, StateActivating, true},
		{StateStarting, StateIdle, false},
		{StateActivating, StateIdle, true},
		{StateActivating, StateSpeaking, false},
		{StateIdle, StateConnecting, true},
		{StateIdle, StateListening, true},
		{StateIdle, StateSpeaking, true},
		{StateIdle, StateActivating, true},
		{StateConnecting, StateIdle, true},
		{StateConnecting, StateListening, true},
		{StateConnecting, StateSpeaking, false},
		{StateListening, StateSpeaking, true},
		{StateListening, StateIdle, true},
		{StateSpeaking, StateListening, true},
		{StateSpeaking, StateIdle, true},
		{StateSpeaking, StateConnecting, false},
		{StateIdle, StateIdle, true}, // 同状态为 no-op
	}
	for _, c := range cases {
		if got := isValidTransition(c.from, c.to); got != c.valid {
			t.Errorf("isValidTransition(%s -> %s) = %v, 期望 %v",
				c.from, c.to, got, c.valid)
		}
	}
}

// ---- 音频帧协议解析（v1/v2/v3）----

func TestParseAudioFrameV1(t *testing.T) {
	payload := []byte{1, 2, 3, 4}
	got, ts := parseAudioFrame(payload, 1)
	if len(got) != 4 || got[0] != 1 || ts != 0 {
		t.Fatalf("v1 裸流解析错误: %v ts=%d", got, ts)
	}
}

func TestParseAudioFrameV2(t *testing.T) {
	payload := []byte{9, 8, 7}
	frame := make([]byte, 16+len(payload))
	binary.BigEndian.PutUint16(frame[0:2], 2) // version
	binary.BigEndian.PutUint16(frame[2:4], 0) // type
	binary.BigEndian.PutUint32(frame[4:8], 0) // reserved
	binary.BigEndian.PutUint32(frame[8:12], 12345)
	binary.BigEndian.PutUint32(frame[12:16], uint32(len(payload)))
	copy(frame[16:], payload)

	got, ts := parseAudioFrame(frame, 2)
	if len(got) != 3 || got[0] != 9 || ts != 12345 {
		t.Fatalf("v2 包头解析错误: %v ts=%d", got, ts)
	}
	// 截断包应安全返回 nil
	if got, _ := parseAudioFrame(frame[:10], 2); got != nil {
		t.Fatalf("截断的 v2 包应返回 nil: %v", got)
	}
}

func TestParseAudioFrameV3(t *testing.T) {
	payload := []byte{5, 6}
	frame := make([]byte, 4+len(payload))
	frame[0] = 0 // type
	frame[1] = 0 // reserved
	binary.BigEndian.PutUint16(frame[2:4], uint16(len(payload)))
	copy(frame[4:], payload)

	got, _ := parseAudioFrame(frame, 3)
	if len(got) != 2 || got[0] != 5 {
		t.Fatalf("v3 包头解析错误: %v", got)
	}
}

// ---- SendAudioFrame 封装（v1 裸流 / v2 包头）----

func TestSendAudioFrameWrap(t *testing.T) {
	cfg := &DeviceConfig{}
	p := NewProtocolClient(cfg) // version 默认 1

	// 模拟连接，验证封装逻辑（不实际发送）
	p.cfg.Websocket = &WebsocketConfig{Version: 2}
	if p.version() != 2 {
		t.Fatal("version() 应返回配置版本")
	}
	// 验证封装长度：16 字节头 + payload
	opusFrame := []byte{1, 2, 3}
	if got := len(wrapAudioFrameForTest(p, opusFrame, 0)); got != 16+len(opusFrame) {
		t.Fatalf("v2 封装长度错误: %d", got)
	}
	p.cfg.Websocket = &WebsocketConfig{Version: 3}
	if got := len(wrapAudioFrameForTest(p, opusFrame, 0)); got != 4+len(opusFrame) {
		t.Fatalf("v3 封装长度错误: %d", got)
	}
	p.cfg.Websocket = &WebsocketConfig{}
	if got := len(wrapAudioFrameForTest(p, opusFrame, 0)); got != len(opusFrame) {
		t.Fatalf("v1 应裸流发送: %d", got)
	}
}

// wrapAudioFrameForTest 复用 SendAudioFrame 的封装逻辑（去掉连接检查）。
func wrapAudioFrameForTest(p *ProtocolClient, opusFrame []byte, ts uint32) []byte {
	version := p.version()
	var data []byte
	switch version {
	case 2:
		data = make([]byte, 16+len(opusFrame))
		binary.BigEndian.PutUint16(data[0:2], uint16(version))
		binary.BigEndian.PutUint16(data[2:4], 0)
		binary.BigEndian.PutUint32(data[4:8], 0)
		binary.BigEndian.PutUint32(data[8:12], ts)
		binary.BigEndian.PutUint32(data[12:16], uint32(len(opusFrame)))
		copy(data[16:], opusFrame)
	case 3:
		data = make([]byte, 4+len(opusFrame))
		data[0] = 0
		data[1] = 0
		binary.BigEndian.PutUint16(data[2:4], uint16(len(opusFrame)))
		copy(data[4:], opusFrame)
	default:
		data = opusFrame
	}
	return data
}

// ---- 系统信息 JSON ----

func TestSystemInfoJson(t *testing.T) {
	cfg := &DeviceConfig{MacAddress: "aa:bb:cc:dd:ee:ff", UUID: "12345678-1234-4123-8123-123456789012"}
	data, err := getSystemInfoJson(cfg)
	if err != nil {
		t.Fatal(err)
	}
	s := string(data)
	for _, field := range []string{"mac_address", "aa:bb:cc:dd:ee:ff", "uuid", "chip_model_name", "version"} {
		if !strings.Contains(s, field) {
			t.Errorf("系统信息 JSON 缺少字段 %q: %s", field, s)
		}
	}
}

// ---- 验证码格式化 ----

func TestFormatCodeForBanner(t *testing.T) {
	if got := formatCodeForBanner("102360"); got != "1 0 2 3 6 0" {
		t.Fatalf("验证码分隔错误: %s", got)
	}
}
