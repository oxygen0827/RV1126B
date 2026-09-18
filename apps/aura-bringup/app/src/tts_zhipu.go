package main

import (
	"bytes"
	"crypto/sha1"
	"crypto/tls"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"time"
)

// tlsConfigInsecure 仅作无 CA 环境的演示兜底（板端 uClibc 常缺根证书；
// 优先用 SSL_CERT_FILE 指向 CA bundle）
var tlsConfigInsecure = &tls.Config{InsecureSkipVerify: true}

func execAplay(path string) bool {
	return exec.Command("aplay", "-q", path).Run() == nil
}

// 智谱 TTS（OpenAI 兼容 /v4/audio/speech）。板端策略：启动时把全部固定提示语
// 预合成缓存为 wav，运行时 aplay 直播缓存，不吃 API 延迟；缓存缺失时现场补合成。

func cueCacheName(text string) string {
	sum := sha1.Sum([]byte(text))
	return hex.EncodeToString(sum[:6]) + ".wav"
}

type zhipuTTS struct {
	apiKey  string
	url     string
	model   string
	voice   string
	format  string
	cacheD  string
	client  *http.Client
	enabled bool
}

func newZhipuTTS(apiKey, url, model, voice, format, cacheDir string, timeout time.Duration, insecure bool) *zhipuTTS {
	if apiKey == "" {
		return &zhipuTTS{enabled: false}
	}
	tr := &http.Transport{}
	if insecure {
		// 仅演示兜底：正常走系统 CA（板端需 SSL_CERT_FILE 指向 CA bundle）
		tr.TLSClientConfig = tlsConfigInsecure
	}
	return &zhipuTTS{
		apiKey: apiKey, url: url, model: model, voice: voice, format: format,
		cacheD: cacheDir, client: &http.Client{Timeout: timeout, Transport: tr},
		enabled: true,
	}
}

// prefetch 预合成全部提示语（文本列表由规则包驱动，见 rule_correction.go）；全部失败返回 false（调用方据此禁用 TTS）。
func (z *zhipuTTS) prefetch(texts []string, logf func(string, ...interface{})) bool {
	if !z.enabled {
		return false
	}
	if err := os.MkdirAll(z.cacheD, 0o755); err != nil {
		logf("tts: cache dir %v", err)
		return false
	}
	ok, missing := 0, 0
	for _, text := range texts {
		path := filepath.Join(z.cacheD, cueCacheName(text))
		if st, err := os.Stat(path); err == nil && st.Size() > 44 {
			ok++
			continue
		}
		audio, err := z.synth(text)
		if err != nil {
			logf("tts: synth %q: %v", text, err)
			missing++
			continue
		}
		if err := os.WriteFile(path, audio, 0o644); err != nil {
			logf("tts: write cache: %v", err)
			missing++
			continue
		}
		ok++
	}
	logf("tts: prefetch %d/%d cues cached in %s", ok, ok+missing, z.cacheD)
	return ok > 0
}

// synth 单条文本合成，返回音频字节。智谱错误（含 HTTP 200 内嵌 error）统一转 error。
func (z *zhipuTTS) synth(text string) ([]byte, error) {
	req := map[string]string{
		"model": z.model, "input": text, "voice": z.voice, "response_format": z.format,
	}
	body, _ := json.Marshal(req)
	httpReq, err := http.NewRequest("POST", z.url, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+z.apiKey)
	resp, err := z.client.Do(httpReq)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("http %d: %s", resp.StatusCode, trunc(data, 200))
	}
	var env struct {
		Error *struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if json.Unmarshal(data, &env) == nil && env.Error != nil {
		return nil, fmt.Errorf("api error %s: %s", env.Error.Code, env.Error.Message)
	}
	if len(data) < 100 {
		return nil, fmt.Errorf("suspiciously short audio (%d bytes): %s", len(data), trunc(data, 100))
	}
	return data, nil
}

// play 播放缓存；缺失则现场补合成（在 TTS 协程内串行执行，不阻塞推理）。
func (z *zhipuTTS) play(text string) bool {
	path := filepath.Join(z.cacheD, cueCacheName(text))
	if _, err := os.Stat(path); err != nil {
		if !z.enabled {
			return false
		}
		audio, err := z.synth(text)
		if err != nil {
			return false
		}
		if err := os.WriteFile(path, audio, 0o644); err != nil {
			return false
		}
	}
	return execAplay(path)
}

func trunc(b []byte, n int) string {
	if len(b) > n {
		return string(b[:n]) + "..."
	}
	return string(b)
}
