package main

// audio.go 音频管线：麦克风采集 → Opus 编码发送；接收 Opus → 解码播放。
// 对应原版 main/audio/ 模块的采集/播放链路。
//
// 采样率处理：上行音频固定 16kHz/60ms 编码（Opus 编码输入采样率）；
// 下行音频按服务器 hello 下发的 sample_rate（api.tenclass.net 为 24000）
// 用独立的 24kHz 输出流播放，与原版 server_sample_rate_ 行为一致。
// 若设备不支持 24kHz 输出，自动降级为 16kHz 解码（Opus 解码器内部重采样）。

import (
	"log"
	"math"
	"sync/atomic"
 "context"
 "time"
	"sync"

	"github.com/gordonklaus/portaudio"
	"github.com/hraban/opus"
)

// AudioManager 管理输入/输出音频流与编解码器。
type AudioManager struct {
	inStream  *portaudio.Stream // 16kHz 输入（麦克风）
	outStream *portaudio.Stream // 服务器采样率输出（扬声器）
	enc       *opus.Encoder     // 16kHz 编码器
	dec       *opus.Decoder     // 按服务器采样率创建

	// 播放队列：mutex 保护的 PCM 帧列表（60ms/帧）
	mu     sync.Mutex
	outBuf [][]int16

	// Hệ số âm lượng đầu ra (0-1, mặc định 70%).
	volume float32
	// Microphone level from the last captured PCM frame (0..100%).
	micLevel atomic.Uint32

	// 回调注入：编码帧发送函数 / 当前状态查询
	sendFn  func([]byte)
	stateFn func() State

	decoderReady bool
	serverRate   int
	serverFrames int
 musicBuf [][]int16
 musicGeneration uint64
 musicPaused bool
 musicLastFrame time.Time
}

// InitAudio 初始化 portaudio 与 Opus 编码器。
func InitAudio() (*AudioManager, error) {
	if err := portaudio.Initialize(); err != nil {
		return nil, err
	}
	enc, err := opus.NewEncoder(audioSampleRate, audioChannels, opus.AppVoIP)
	if err != nil {
		portaudio.Terminate()
		return nil, err
	}
	return &AudioManager{enc: enc, volume: 0.7}, nil
}

func (a *AudioManager) Close() {
	if a.inStream != nil {
		a.inStream.Stop()
		a.inStream.Close()
	}
	if a.outStream != nil {
		a.outStream.Stop()
		a.outStream.Close()
	}
	portaudio.Terminate()
}

// SetSendCallback 注入编码帧发送回调（由 main 设置）。
func (a *AudioManager) SetSendCallback(fn func([]byte)) { a.sendFn = fn }

// SetStateCallback 注入状态查询回调。
func (a *AudioManager) SetStateCallback(fn func() State) { a.stateFn = fn }

// StartInput 启动 16kHz 麦克风采集流。
func (a *AudioManager) StartInput() error {
	stream, err := portaudio.OpenDefaultStream(
		audioChannels, 0, float64(audioSampleRate),
		audioSampleRate*frameDurationMs/1000, a.inputCallback)
	if err != nil {
		return err
	}
	if err := stream.Start(); err != nil {
		return err
	}
	a.inStream = stream
	log.Printf("音频输入流已启动: %dHz/%dms", audioSampleRate, frameDurationMs)
	return nil
}

// SetupDecoder 按服务器下发的采样率创建解码器并启动输出流。
// 服务器 sampleRate 与设备输出能力不匹配时降级处理。
func (a *AudioManager) SetupDecoder(sampleRate int) error {
	a.mu.Lock()
	defer a.mu.Unlock()

	if a.decoderReady && a.serverRate == sampleRate {
		return nil // 已就绪且采样率未变化
	}

	rate := sampleRate
	if rate <= 0 {
		rate = audioSampleRate
	}
	// 优先按服务器采样率开输出流；失败则降级 16kHz
	stream, err := portaudio.OpenDefaultStream(
		0, audioChannels, float64(rate),
		rate*frameDurationMs/1000, a.outputCallback)
	if err != nil {
		if rate == audioSampleRate {
			return err
		}
		log.Printf("输出流 %dHz 不可用，降级为 %dHz: %v", rate, audioSampleRate, err)
		rate = audioSampleRate
		stream, err = portaudio.OpenDefaultStream(
			0, audioChannels, float64(rate),
			rate*frameDurationMs/1000, a.outputCallback)
		if err != nil {
			return err
		}
	}
	if err := stream.Start(); err != nil {
		return err
	}

	dec, err := opus.NewDecoder(rate, audioChannels)
	if err != nil {
		stream.Stop()
		stream.Close()
		return err
	}

	// 关闭旧输出流
	if a.outStream != nil {
		a.outStream.Stop()
		a.outStream.Close()
	}
	a.outStream = stream
	a.dec = dec
	a.decoderReady = true
	a.serverRate = rate
	a.serverFrames = rate * frameDurationMs / 1000
	log.Printf("音频输出流已启动: %dHz/%dms", rate, frameDurationMs)
	return nil
}

// inputCallback 麦克风回调：Listening 状态下编码并发送。
func (a *AudioManager) inputCallback(in, _ []int16) {
	if a.stateFn == nil || a.sendFn == nil {
		return
	}
	if a.stateFn() != StateListening {
		a.micLevel.Store(0)
		return
	}
	// Meter is based on the microphone PCM, not the server response.
	a.micLevel.Store(pcmLevel(in))
	data := make([]byte, 4096)
	n, err := a.enc.Encode(in, data)
	if err != nil {
		log.Printf("Opus 编码失败: %v", err)
		return
	}
	a.sendFn(data[:n])
}

// outputCallback 扬声器回调：从播放队列取 PCM 帧，应用音量。
func (a *AudioManager) outputCallback(_, out []int16) {
	a.mu.Lock()
	if len(a.outBuf) > 0 {
		frame := a.outBuf[0]
		copy(out, frame)
		a.outBuf = a.outBuf[1:]
		volume := a.volume
		a.mu.Unlock()
		applyVolume(out, volume)
		return
	}
    if len(a.musicBuf)>0 && !a.musicPaused && (a.stateFn==nil||a.stateFn()==StateIdle) {
        frame:=a.musicBuf[0]
        copy(out,frame)
        a.musicBuf=a.musicBuf[1:]
        volume:=a.volume
        a.mu.Unlock()
        applyVolume(out,volume)
        return
    }
	a.mu.Unlock()
	for i := range out {
		out[i] = 0
	}
}

// PushPcm 将解码后的 PCM 帧加入播放队列。
// 只接受与当前输出采样率一致的帧长（60ms）；长度不符时按输出帧长截断/补零。
func (a *AudioManager) PushPcm(pcm []int16) {
	a.mu.Lock()
	defer a.mu.Unlock()
	frameSize := a.serverFrames
	if frameSize == 0 {
		frameSize = audioSampleRate * frameDurationMs / 1000
	}
	if len(pcm) >= frameSize {
		frame := make([]int16, frameSize)
		copy(frame, pcm[:frameSize])
		a.outBuf = append(a.outBuf, frame)
	}
}

// ClearOutput 清空播放队列（TTS start 时调用，对应原版 ResetDecoder 前的清空行为）。
func (a *AudioManager) ClearOutput() {
	a.mu.Lock()
	a.outBuf = nil
	a.mu.Unlock()
}

// DecodeAudio 解码一帧 Opus 数据，返回 PCM 或 nil。
func (a *AudioManager) DecodeAudio(opusData []byte) []int16 {
	a.mu.Lock()
	dec := a.dec
	rate := a.serverRate
	a.mu.Unlock()
	if dec == nil {
		return nil
	}
	frameSize := rate * frameDurationMs / 1000
	pcm := make([]int16, frameSize)
	n, err := dec.Decode(opusData, pcm)
	if err != nil {
		log.Printf("Opus 解码失败: %v", err)
		return nil
	}
	return pcm[:n]
}

// SetVolume applies software attenuation in the safe 0–100% range.
func (a *AudioManager) SetVolume(v float32) {
	if v < 0 { v = 0 }
	if v > 1 { v = 1 }
	a.mu.Lock()
	a.volume = v
	a.mu.Unlock()
	log.Printf("Âm lượng phát: %.0f%%", v*100)
}
func (a *AudioManager) Volume() float32 {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.volume
}
func (a *AudioManager) MicLevel() uint32 { return a.micLevel.Load() }

// pcmLevel estimates microphone RMS and scales it perceptually for a UI meter.
func pcmLevel(samples []int16) uint32 {
	if len(samples) == 0 { return 0 }
	var energy float64
	for _, s := range samples { v:=float64(s)/32768; energy+=v*v }
	rms:=math.Sqrt(energy/float64(len(samples)))
	// sqrt() provides visibility for normal speech without any audio gain.
	pct:=math.Sqrt(rms)*100
	if pct>100 { pct=100 }
	return uint32(math.Round(pct))
}

// applyVolume 应用音量增益。
func applyVolume(samples []int16, volume float32) {
	if volume == 1.0 {
		return
	}
	for i, s := range samples {
		v := float32(s) * volume
		if v > 32767 {
			v = 32767
		} else if v < -32768 {
			v = -32768
		}
		samples[i] = int16(v)
	}
}

func (a *AudioManager) OutputRate()int {
 a.mu.Lock();defer a.mu.Unlock();return a.serverRate
}
func (a *AudioManager) BeginMusic(gen uint64) {
 a.mu.Lock();a.musicGeneration=gen;a.musicBuf=nil;a.musicPaused=false;a.mu.Unlock()
}
func (a *AudioManager) StopMusic(gen uint64) {
 a.mu.Lock();a.musicGeneration=gen;a.musicBuf=nil;a.musicPaused=false;a.mu.Unlock()
}
func (a *AudioManager) SetMusicPaused(paused bool) {
 a.mu.Lock();a.musicPaused=paused;a.mu.Unlock()
}
func (a *AudioManager) PushMusicFrame(ctx context.Context,gen uint64,frame []int16)bool {
 ticker:=time.NewTicker(15*time.Millisecond);defer ticker.Stop()
 for{
  a.mu.Lock()
  if gen!=a.musicGeneration{a.mu.Unlock();return false}
  // Queue max 36 * 60ms = ~2.2 seconds. Music pauses while speaking.
  if len(a.musicBuf)<36{a.musicBuf=append(a.musicBuf,frame);a.mu.Unlock();return true}
  a.mu.Unlock()
  select{case <-ctx.Done():return false;case <-ticker.C:}
 }
}
