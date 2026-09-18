package main

import (
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// ttsPlayer 把语音播报挪出推理协程：speak 入队（带类别 key），loop 串行播放。
// 三种通道按优先级：
//  1. 智谱 TTS（z 非 nil）：按文本 hash 缓存 wav（启动时预合成），aplay 播放
//  2. 命令模板 cmdTemplate（{text} 占位）：调用外部 TTS 命令
//  3. 预录音目录 wavDir：aplay 播放按类别命名的 ok/knee/hip/elbow/depth.wav

type ttsMsg struct{ key, text string }

type ttsPlayer struct {
	z          *zhipuTTS
	cmdTemplate string
	wavDir     string
	cooldown   time.Duration
	mu         sync.Mutex
	last       map[string]time.Time
	q          chan ttsMsg
}

func newTTSPlayer(z *zhipuTTS, cmdTemplate, wavDir string, cooldown time.Duration) *ttsPlayer {
	p := &ttsPlayer{z: z, cmdTemplate: cmdTemplate, wavDir: wavDir, cooldown: cooldown,
		last: map[string]time.Time{}, q: make(chan ttsMsg, 8)}
	if z != nil || cmdTemplate != "" || wavDir != "" {
		go p.loop()
	}
	return p
}

func (p *ttsPlayer) available() bool {
	return p != nil && (p.z != nil || p.cmdTemplate != "" || p.wavDir != "")
}

func (p *ttsPlayer) speak(key, text string) {
	if !p.available() || text == "" {
		return
	}
	p.mu.Lock()
	now := time.Now()
	if last, ok := p.last[key]; ok && now.Sub(last) < p.cooldown {
		p.mu.Unlock()
		return
	}
	p.last[key] = now
	p.mu.Unlock()
	select {
	case p.q <- ttsMsg{key, text}:
	default: // 队列满则丢弃，绝不阻塞推理
	}
}

func (p *ttsPlayer) loop() {
	for m := range p.q {
		switch {
		case p.z != nil:
			p.z.play(m.text) // 文本 hash 缓存，缺失时现场补合成
		case p.cmdTemplate != "":
			cmd := strings.ReplaceAll(p.cmdTemplate, "{text}", shellQuote(m.text))
			_ = exec.Command("sh", "-c", cmd).Run()
		default:
			_ = exec.Command("aplay", "-q", filepath.Join(p.wavDir, m.key+".wav")).Run()
		}
	}
}

func shellQuote(s string) string { return "'" + strings.ReplaceAll(s, "'", "'\\''") + "'" }

func (p *ttsPlayer) Close() {
	if p != nil {
		close(p.q)
	}
}
