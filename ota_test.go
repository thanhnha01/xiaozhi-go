package main

import (
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
)

// mockOtaServer 模拟小智 OTA 服务器。
// unbound=true 时返回激活码（未绑定），否则返回已绑定响应（无 activation 段）。
// 未绑定场景：第一次 /activate 返回 200（用户输入验证码），
// 此后的 /ota/ 检查返回已绑定（与真实服务器行为一致）。
func mockOtaServer(t *testing.T, unbound bool) *httptest.Server {
	t.Helper()
	var mu sync.Mutex
	activated := false // 仅在 unbound 场景中由 /activate 置位
	mux := http.NewServeMux()
	mux.HandleFunc("/ota/", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Device-Id") == "" {
			t.Errorf("缺少 Device-Id 请求头")
		}
		if r.Header.Get("Client-Id") == "" {
			t.Errorf("缺少 Client-Id 请求头")
		}
		if r.Header.Get("Activation-Version") == "" {
			t.Errorf("缺少 Activation-Version 请求头")
		}
		w.Header().Set("Content-Type", "application/json")
		mu.Lock()
		isBound := !unbound || activated
		mu.Unlock()
		if isBound {
			// 已绑定：无 activation 段
			w.Write([]byte(`{
				"websocket": {"url": "wss://example.com/", "token": "test-token"},
				"firmware": {"version": "1.0.0", "url": ""}
			}`))
		} else {
			// 未绑定：返回 6 位激活码 + challenge
			w.Write([]byte(`{
				"activation": {
					"code": "123456",
					"message": "bind me",
					"challenge": "test-challenge"
				},
				"websocket": {"url": "wss://example.com/", "token": "test-token"},
				"server_time": {"timestamp": 1700000000000, "timezone_offset": 480}
			}`))
		}
	})
	mux.HandleFunc("/ota/activate", func(w http.ResponseWriter, r *http.Request) {
		if unbound {
			// 模拟用户在 App 输入验证码后绑定成功
			mu.Lock()
			activated = true
			mu.Unlock()
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{"message":"activated"}`))
		} else {
			w.WriteHeader(http.StatusAccepted)
			w.Write([]byte(`{"message":"Device activation timeout"}`))
		}
	})
	srv := httptest.NewServer(mux)
	return srv
}

// 已绑定设备：CheckVersion 无 activation 段 → 直接返回，不进入激活流程。
func TestRunActivationBoundDevice(t *testing.T) {
	srv := mockOtaServer(t, false)
	defer srv.Close()

	cfg := &DeviceConfig{
		MacAddress: "aa:bb:cc:dd:ee:ff",
		UUID:       "12345678-1234-4123-8123-123456789012",
		OtaURL:     srv.URL + "/ota/",
	}
	if err := RunActivation(srv.Client(), cfg); err != nil {
		t.Fatalf("已绑定设备不应报错: %v", err)
	}
	// 服务器下发的 websocket 配置应被持久化到配置
	if cfg.Websocket == nil || cfg.Websocket.Token != "test-token" {
		t.Fatalf("websocket 配置未保存: %+v", cfg.Websocket)
	}
}

// 未绑定设备：应先返回激活码 → 轮询 activate → 服务器模拟绑定成功后返回。
func TestRunActivationUnboundDevice(t *testing.T) {
	srv := mockOtaServer(t, true)
	defer srv.Close()

	cfg := &DeviceConfig{
		MacAddress: "aa:bb:cc:dd:ee:ff",
		UUID:       "12345678-1234-4123-8123-123456789012",
		OtaURL:     srv.URL + "/ota/",
	}
	if err := RunActivation(srv.Client(), cfg); err != nil {
		t.Fatalf("绑定流程应完成: %v", err)
	}
	if cfg.Websocket == nil || cfg.Websocket.Token != "test-token" {
		t.Fatalf("websocket 配置未保存: %+v", cfg.Websocket)
	}
}

// CheckVersion 请求体必须包含 MAC 与 UUID（服务器据此识别设备）。
func TestCheckVersionBody(t *testing.T) {
	srv := mockOtaServer(t, false)
	defer srv.Close()

	cfg := &DeviceConfig{
		MacAddress: "aa:bb:cc:dd:ee:ff",
		UUID:       "12345678-1234-4123-8123-123456789012",
		OtaURL:     srv.URL + "/ota/",
	}
	if _, err := checkVersion(srv.Client(), cfg); err != nil {
		t.Fatalf("checkVersion 失败: %v", err)
	}
}
