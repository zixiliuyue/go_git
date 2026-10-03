package trace2

import (
	"encoding/json"
	"fmt"
	"math/rand"
	"os"
	"runtime"
	"sync"
	"time"
)

// Session 代表一个 Trace2 遥测会话
type Session struct {
	mu          sync.Mutex
	sid         string
	startTime   time.Time
	targets     []*targetWriter
	regionStack []regionEntry
	closed      bool
}

type regionEntry struct {
	category  string
	label     string
	enterTime time.Time
	nesting   int
}

var (
	globalMu      sync.Mutex
	globalSession *Session
)

// Initialize 初始化全局 Trace2 会话，自动读取 GIT_TRACE2, GIT_TRACE2_EVENT, GIT_TRACE2_PERF 环境变量
func Initialize(cmdName string, argv []string) *Session {
	globalMu.Lock()
	defer globalMu.Unlock()

	if globalSession != nil {
		return globalSession
	}

	targets := make([]*targetWriter, 0, 3)

	// 1. 尝试打开 GIT_TRACE2_EVENT 目标 (JSONL)
	if tw, err := openTarget(os.Getenv("GIT_TRACE2_EVENT"), TargetEvent); err == nil && tw != nil {
		targets = append(targets, tw)
	}
	// 2. 尝试打开 GIT_TRACE2_PERF 目标 (性能层次树)
	if tw, err := openTarget(os.Getenv("GIT_TRACE2_PERF"), TargetPerf); err == nil && tw != nil {
		targets = append(targets, tw)
	}
	// 3. 尝试打开 GIT_TRACE2 目标 (标准文本)
	if tw, err := openTarget(os.Getenv("GIT_TRACE2"), TargetNormal); err == nil && tw != nil {
		targets = append(targets, tw)
	}

	if len(targets) == 0 {
		return nil
	}

	now := time.Now()
	// 生成符合 Git 标准的 Session ID (SID)
	sid := generateSID(now)

	s := &Session{
		sid:       sid,
		startTime: now,
		targets:   targets,
	}
	globalSession = s

	// 发送初始事件：version, start, cmd_name
	s.emitVersion()
	s.emitStart(argv)
	if cmdName != "" {
		s.emitCmdName(cmdName)
	}

	return s
}

// generateSID 生成规范的 Trace2 Session ID
func generateSID(t time.Time) string {
	ts := t.UTC().Format("20060102T150405.000000Z")
	pid := os.Getpid()
	rnd := rand.Intn(100000)
	sid := fmt.Sprintf("%s-P%06d-R%05d", ts, pid, rnd)

	parentSID := os.Getenv("GIT_TRACE2_PARENT_SID")
	if parentSID != "" {
		return parentSID + "/" + sid
	}
	return sid
}

// RegionEnter 记录阶段进入
func RegionEnter(category, label string) {
	globalMu.Lock()
	s := globalSession
	globalMu.Unlock()
	if s != nil {
		s.RegionEnter(category, label)
	}
}

// RegionLeave 记录阶段退出
func RegionLeave(category, label string) {
	globalMu.Lock()
	s := globalSession
	globalMu.Unlock()
	if s != nil {
		s.RegionLeave(category, label)
	}
}

// Region 提供 defer 使用的快捷包装器：defer trace2.Region("cat", "lbl")()
func Region(category, label string) func() {
	RegionEnter(category, label)
	return func() {
		RegionLeave(category, label)
	}
}

// Data 记录键值对监控指标
func Data(category, key string, val any) {
	globalMu.Lock()
	s := globalSession
	globalMu.Unlock()
	if s != nil {
		s.Data(category, key, val)
	}
}

// Error 记录错误事件
func Error(msg string) {
	globalMu.Lock()
	s := globalSession
	globalMu.Unlock()
	if s != nil {
		s.Error(msg)
	}
}

// Close 关闭全局会话并刷新 atexit 事件
func Close(exitCode int) {
	globalMu.Lock()
	s := globalSession
	globalSession = nil
	globalMu.Unlock()

	if s != nil {
		s.Close(exitCode)
	}
}

// ---------------- 会话内部实现 ----------------

func (s *Session) getCaller() (string, int) {
	_, file, line, ok := runtime.Caller(3)
	if !ok {
		return "unknown", 0
	}
	return file, line
}

func (s *Session) emitEvent(eventMap map[string]any, perfMsg string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.closed {
		return
	}

	tAbs := time.Since(s.startTime).Seconds()
	now := time.Now().UTC()
	file, line := s.getCaller()

	eventMap["sid"] = s.sid
	eventMap["thread"] = "main"
	eventMap["time"] = now.Format(time.RFC3339Nano)
	eventMap["file"] = file
	eventMap["line"] = line
	eventMap["t_abs"] = tAbs
	eventMap["evt"] = "3"

	for _, tw := range s.targets {
		switch tw.targetType {
		case TargetEvent:
			data, err := json.Marshal(eventMap)
			if err == nil {
				_, _ = tw.writer.Write(append(data, '\n'))
			}
		case TargetPerf:
			if perfMsg != "" {
				line := fmt.Sprintf("d0 | main | %s\n", perfMsg)
				_, _ = tw.writer.Write([]byte(line))
			}
		case TargetNormal:
			evtName, _ := eventMap["event"].(string)
			line := fmt.Sprintf("%s %s:%d %s\n", now.Format("15:04:05.000000"), file, line, evtName)
			_, _ = tw.writer.Write([]byte(line))
		}
	}
}

func (s *Session) emitVersion() {
	s.emitEvent(map[string]any{
		"event":   "version",
		"exe":     "gogit",
		"version": "0.1.0",
	}, "version | 0.1.0")
}

func (s *Session) emitStart(argv []string) {
	s.emitEvent(map[string]any{
		"event": "start",
		"argv":  argv,
	}, fmt.Sprintf("start | %v", argv))
}

func (s *Session) emitCmdName(cmdName string) {
	s.emitEvent(map[string]any{
		"event":     "cmd_name",
		"name":      cmdName,
		"hierarchy": cmdName,
	}, fmt.Sprintf("cmd_name | %s", cmdName))
}

func (s *Session) RegionEnter(category, label string) {
	s.mu.Lock()
	nesting := len(s.regionStack) + 1
	entry := regionEntry{
		category:  category,
		label:     label,
		enterTime: time.Now(),
		nesting:   nesting,
	}
	s.regionStack = append(s.regionStack, entry)
	s.mu.Unlock()

	s.emitEvent(map[string]any{
		"event":    "region_enter",
		"category": category,
		"label":    label,
		"nesting":  nesting,
	}, fmt.Sprintf("region_enter | r%d | %s | %s", nesting, category, label))
}

func (s *Session) RegionLeave(category, label string) {
	s.mu.Lock()
	if len(s.regionStack) == 0 {
		s.mu.Unlock()
		return
	}
	last := s.regionStack[len(s.regionStack)-1]
	s.regionStack = s.regionStack[:len(s.regionStack)-1]
	s.mu.Unlock()

	tRel := time.Since(last.enterTime).Seconds()

	s.emitEvent(map[string]any{
		"event":    "region_leave",
		"category": category,
		"label":    label,
		"nesting":  last.nesting,
		"t_rel":    tRel,
	}, fmt.Sprintf("region_leave | r%d | %.6f | %s | %s", last.nesting, tRel, category, label))
}

func (s *Session) Data(category, key string, val any) {
	s.emitEvent(map[string]any{
		"event":    "data",
		"category": category,
		"key":      key,
		"value":    fmt.Sprintf("%v", val),
	}, fmt.Sprintf("data | %s | %s:%v", category, key, val))
}

func (s *Session) Error(msg string) {
	s.emitEvent(map[string]any{
		"event": "error",
		"msg":   msg,
	}, fmt.Sprintf("error | %s", msg))
}

func (s *Session) Close(exitCode int) {
	s.emitEvent(map[string]any{
		"event": "atexit",
		"code":  exitCode,
	}, fmt.Sprintf("atexit | code:%d", exitCode))

	s.mu.Lock()
	defer s.mu.Unlock()
	s.closed = true

	for _, tw := range s.targets {
		_ = tw.writer.Close()
	}
	s.targets = nil
}
