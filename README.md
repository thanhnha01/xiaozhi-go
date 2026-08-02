# XiaoZhi-Go

小智对话助手 **PC 版**，用 Go 语言实现 [xiaozhi-esp32](https://github.com/78/xiaozhi-esp32) 的核心功能。通过 WebSocket 协议连接小智服务器（默认 `wss://api.tenclass.net/xiaozhi/v1/`），实现语音对话、设备绑定、IoT（MCP）控制。

协议遵循原版 [websocket.md](https://github.com/78/xiaozhi-esp32/blob/main/docs/websocket.md) 与 OTA 激活规范（`main/ota.cc`）。

## 功能特性

### 1. 设备绑定（OTA 激活）—— 核心功能

与原版 ESP32 完全一致的绑定流程：

- **首次启动生成真实随机 MAC 地址与 UUID v4**，持久化到 `device_config.json`（对应原版 NVS 存储），重启不更换身份。
- 开机 POST 到 OTA 接口（`https://api.tenclass.net/xiaozhi/ota/`），请求头携带 `Device-Id`（MAC）、`Client-Id`（UUID）、`Activation-Version`，请求体为系统信息 JSON。
- 服务器按 MAC 判断是否已绑定账号：
  - **未绑定** → 返回 `activation` 对象（6 位验证码 + message + challenge）→ 终端醒目显示并**语音逐位播报验证码**（macOS 系统语音），随后**轮询 `POST {ota_url}/activate`**（202 等待 → 3 秒重试，其他失败 → 10 秒重试，最多 10 次），直到在小智控制台（xiaozhi.me）中输入验证码完成绑定。
  - **已绑定** → 无 `activation` 段 → 直接进入正常对话流程。
- 服务器响应中的 `websocket{url,token,version}` 配置自动持久化，用于后续连接。

### 2. 语音对话

- **语音输入**：麦克风 16kHz 采集，60ms/帧 Opus 编码实时发送。
- **语音输出**：按服务器下发的采样率（24000Hz）解码播放（双速率音频流，自动降级处理）。
- **自动监听**：AI 回复结束后自动进入监听，直接说话即可，无需按键。

### 3. 完整消息协议

| 方向            | 消息                                                                                                                            |
| --------------- | ------------------------------------------------------------------------------------------------------------------------------- |
| 客户端 → 服务器 | `hello`（能力协商）、`listen`(start/stop/detect)、`abort`、`mcp`                                                                |
| 服务器 → 客户端 | `hello`（session_id/audio_params）、`tts`(start/stop/sentence_start)、`stt`、`llm`、`mcp`、`system`(reboot)、`alert`、`goodbye` |
| 音频            | v1 裸 Opus；v2/v3 按 BinaryProtocol2/3 包头（大端序）收发                                                                       |

### 4. 状态机

忠实移植原版 `device_state_machine.cc` 迁移规则：`unknown → starting → activating → idle ⇄ connecting ⇄ listening ⇄ speaking`，非法迁移被拒绝并记录日志。

### 5. 断线重连

连接断开后指数退避自动重连（1s → 2s → 4s → ... → 30s 封顶），重连成功后自动恢复。

### 6. 键盘交互

| 按键                   | 功能                                         |
| ---------------------- | -------------------------------------------- |
| `空格`                 | 空闲时开始监听 / 监听时停止 / 播放时打断对话 |
| `1` / `2`              | 开始 / 停止监听                              |
| `3`                    | 发送唤醒词（你好小智）                       |
| `4`                    | 中止对话                                     |
| `5`                    | 发送 MCP 测试消息                            |
| `+` / `-` 或 `↑` / `↓` | 音量调节                                     |
| `6`                    | 退出                                         |

## 安装

### 前置条件

- Go 1.24+，`pkg-config`
- macOS：`brew install portaudio opus pkg-config`
- Linux：`sudo apt-get install libopus-dev portaudio19-dev pkg-config`

### 编译

```bash
go mod tidy
go build -o xiaozhi .
```

### 运行

```bash
./xiaozhi
```

可选参数：

```bash
./xiaozhi -config ./device_config.json   # 指定配置文件路径
./xiaozhi -ota https://your-server/ota/  # 指定 OTA 服务器
./xiaozhi -listen-timeout 10             # 监听超时秒数（0 为不自动停止）
```

### 首次使用（设备绑定）

1. 运行程序，首次启动自动生成随机 MAC 地址与 UUID（保存于 `device_config.json`）。
2. 若设备未绑定，程序会显示并语音播报 **6 位激活码**。
3. 打开「小智」控制台（xiaozhi.me），输入激活码完成绑定。
4. 绑定成功后程序自动进入对话流程，此后该 MAC 对应的设备不再需要激活。

> 删除 `device_config.json` 会生成新的设备身份（相当于重置设备）。

## 项目结构

```
main.go        入口与流程编排（激活 → 连接 → 对话）
config.go      配置持久化（替代原版 NVS：MAC/UUID/websocket 配置）
device.go      设备身份：随机 MAC、UUID v4、系统信息 JSON
ota.go         设备绑定：CheckVersion + 激活码播报 + Activate 轮询
protocol.go    WebSocket 协议：hello/listen/abort/mcp + 二进制音频
state.go       状态机（移植原版迁移规则）
audio.go       音频管线：16kHz 采集编码 + 服务器采样率解码播放
input.go       键盘交互
main_test.go   单元测试
```

## 与原版的差异（有意为之）

- **固件 OTA 升级**：PC 程序无需升级固件，不实现（仅解析 `firmware` 段并记录版本）。
- **MQTT/UDP 协议**：仅实现 WebSocket 协议；OTA 响应中的 mqtt 配置仅保存不使用。
- **唤醒词/VAD**：由键盘交互与监听超时替代硬件唤醒词检测。
- **配网（WiFi/BluFi）**：PC 无配网需求，不实现。

## 测试

```bash
go test ./...
```

## 许可证

MIT
