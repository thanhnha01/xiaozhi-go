package main

// state.go 设备状态机，忠实移植原版 main/device_state.h + device_state_machine.cc。
// PC 版去掉 ESP32 专属状态（wifi_configuring/upgrading/audio_testing/fatal_error），
// 保留的迁移规则与原版完全一致。

import (
	"log"
	"sync"
)

// State 设备状态（对应原版 DeviceState 枚举，device_state.h）。
type State int

const (
	StateUnknown State = iota
	StateStarting
	StateActivating // 激活中（检查绑定/等待绑定）
	StateIdle
	StateConnecting
	StateListening
	StateSpeaking
)

var stateNames = map[State]string{
	StateUnknown:    "unknown",
	StateStarting:   "starting",
	StateActivating: "activating",
	StateIdle:       "idle",
	StateConnecting: "connecting",
	StateListening:  "listening",
	StateSpeaking:   "speaking",
}

func (s State) String() string {
	if name, ok := stateNames[s]; ok {
		return name
	}
	return "invalid_state"
}

// StateMachine 对应原版 DeviceStateMachine（线程安全状态迁移 + 监听器）。
type StateMachine struct {
	mu        sync.RWMutex
	current   State
	nextID    int
	listeners map[int]func(oldState, newState State)
}

// NewStateMachine 初始状态为 Unknown（对应原版构造函数）。
func NewStateMachine() *StateMachine {
	return &StateMachine{current: StateUnknown, listeners: make(map[int]func(State, State))}
}

func (sm *StateMachine) Current() State {
	sm.mu.RLock()
	defer sm.mu.RUnlock()
	return sm.current
}

// isValidTransition 对应原版 IsValidTransition()（device_state_machine.cc:34-102），
// 去掉 ESP32 专属状态后保留的规则原样不变。
func isValidTransition(from, to State) bool {
	if from == to {
		return true
	}
	switch from {
	case StateUnknown:
		return to == StateStarting
	case StateStarting:
		return to == StateActivating
	case StateActivating:
		return to == StateIdle
	case StateIdle:
		return to == StateConnecting ||
			to == StateListening ||
			to == StateSpeaking ||
			to == StateActivating
	case StateConnecting:
		return to == StateIdle || to == StateListening
	case StateListening:
		return to == StateSpeaking || to == StateIdle
	case StateSpeaking:
		return to == StateListening || to == StateIdle
	default:
		return false
	}
}

// TransitionTo 对应原版 TransitionTo()：校验迁移规则，成功则通知监听器。
func (sm *StateMachine) TransitionTo(target State) bool {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	old := sm.current
	if !isValidTransition(old, target) {
		log.Printf("非法状态迁移: %s -> %s（忽略）", old, target)
		return false
	}
	sm.current = target
	log.Printf("状态: %s -> %s", old, target)
	// 复制监听器列表后调用，避免持锁期间回调死锁
	callbacks := make([]func(State, State), 0, len(sm.listeners))
	for _, cb := range sm.listeners {
		callbacks = append(callbacks, cb)
	}
	for _, cb := range callbacks {
		cb(old, target)
	}
	return true
}

// AddListener 注册状态变更监听器，返回取消函数。
func (sm *StateMachine) AddListener(cb func(oldState, newState State)) func() {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	id := sm.nextID
	sm.nextID++
	sm.listeners[id] = cb
	return func() {
		sm.mu.Lock()
		defer sm.mu.Unlock()
		delete(sm.listeners, id)
	}
}
