package main

// input.go 终端键盘交互。
// 对应原版的按钮交互（main/boards/common/button.h / compact_wifi_board.cc）：
// Boot 键单击 = 对话/打断（这里用空格）；Touch 按下/松开 = 开始/停止聆听。

import (
	"fmt"
	"log"

	"github.com/eiannone/keyboard"
)

// showCommandMenu 打印命令菜单（对应原版 README 中的交互说明）。
func showCommandMenu() {
	fmt.Println()
	fmt.Println("═══════════════════════════════════════════════")
	fmt.Println("  小智对话助手（xiaozhi-go）")
	fmt.Println("═══════════════════════════════════════════════")
	fmt.Println("  [空格]  按住说话 / 松开结束（PTT）")
	fmt.Println("  [1] 开始监听    [2] 停止监听")
	fmt.Println("  [3] 发送唤醒词  [4] 中止对话")
	fmt.Println("  [5] 设备 WebSocket / MCP 测试说明")
	fmt.Println("  [+]/[-] 音量调节  [6] 退出")
	fmt.Println("═══════════════════════════════════════════════")
	fmt.Println()
}

// startKeyboardInput 启动键盘监听循环，把输入分发到回调。
// 返回的 channel 在退出（按 6）时收到信号。
func startKeyboardInput(
	onToggle func(),
	onStart func(),
	onStop func(),
	onWakeWord func(),
	onAbort func(),
	onMcp func(),
	onVolume func(float32),
	onQuit func(),
) {
	if err := keyboard.Open(); err != nil {
		log.Printf("无法打开键盘监听（可忽略，仍可使用音频交互）: %v", err)
		return
	}
	defer keyboard.Close()

	for {
		char, key, err := keyboard.GetKey()
		if err != nil {
			log.Printf("键盘监听错误: %v", err)
			return
		}
		switch {
		case key == keyboard.KeySpace:
			onToggle()
		case key == keyboard.KeyArrowUp:
			onVolume(0.1)
		case key == keyboard.KeyArrowDown:
			onVolume(-0.1)
		case char == '=' || char == '+':
			onVolume(0.1)
		case char == '-' || char == '_':
			onVolume(-0.1)
		case char == '1':
			onStart()
		case char == '2':
			onStop()
		case char == '3':
			onWakeWord()
		case char == '4':
			onAbort()
		case char == '5':
			onMcp()
		case char == '6' || char == 'q' || char == 'Q':
			onQuit()
			return
		}
	}
}
