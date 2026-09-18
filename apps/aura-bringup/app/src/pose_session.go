package main

import (
	"fmt"
	"os"
	"time"
)

// 手动判定窗（M0-lite）：sessionFile 存在即开窗，窗口时长 activeSeconds 后自动关
// （并删除触发器，需再次 touch 重开）。判定只在窗口内进行；窗口外 tracker 保持
// 重置（不武装、不计数、不播报），解决路人经过/误检噪声导致的误报。
type poseSession struct {
	file    string
	seconds int
	active  bool
	opened  bool
	start   time.Time
	last    bool
}

func newPoseSession(file string, seconds int) *poseSession {
	if file == "" || seconds <= 0 {
		return nil
	}
	return &poseSession{file: file, seconds: seconds}
}

// tick 每帧调用（25Hz，tmpfs stat 开销可忽略）。返回 (是否刚开窗, 是否允许判定)。
func (s *poseSession) tick() (opened, allow bool) {
	if s == nil {
		return false, true
	}
	now := time.Now()
	if !s.active {
		if _, err := os.Stat(s.file); err == nil {
			s.active = true
			s.opened = true
			s.start = now
			fmt.Println("session: opened")
		}
		return s.opened, s.active
	}
	s.opened = false
	if now.Sub(s.start) > time.Duration(s.seconds)*time.Second {
		s.active = false
		os.Remove(s.file)
		fmt.Println("session: closed after", s.seconds, "s")
		return false, false
	}
	return false, true
}

// forceClose 屏幕「结束」按钮：删触发器 + 立即关窗（返回是否刚关闭）。
func (s *poseSession) forceClose() bool {
	if s == nil || !s.active {
		return false
	}
	s.active = false
	s.opened = false
	os.Remove(s.file)
	fmt.Println("session: closed by user")
	return true
}
