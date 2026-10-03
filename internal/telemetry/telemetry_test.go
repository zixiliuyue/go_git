package telemetry

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestStructuredLoggingJSON(t *testing.T) {
	logFile := filepath.Join(t.TempDir(), "test.log.json")
	logger := ResetLogger("debug", "json", logFile)

	subLog := ForSubsystem("transport")
	subLog.Info("fetching remote packfile", "remote", "origin", "wants_count", 15)
	subLog.Debug("negotiating capabilities", "proto", "v2")

	content, err := os.ReadFile(logFile)
	if err != nil {
		t.Fatalf("failed to read log file: %v", err)
	}

	lines := strings.Split(strings.TrimSpace(string(content)), "\n")
	if len(lines) != 2 {
		t.Fatalf("expected 2 log lines, got %d", len(lines))
	}

	var m1 map[string]any
	if err := json.Unmarshal([]byte(lines[0]), &m1); err != nil {
		t.Fatalf("invalid json: %v", err)
	}

	if m1["msg"] != "fetching remote packfile" {
		t.Errorf("msg mismatch: %v", m1["msg"])
	}
	if m1["subsystem"] != "transport" {
		t.Errorf("subsystem mismatch: %v", m1["subsystem"])
	}
	if m1["remote"] != "origin" {
		t.Errorf("remote mismatch: %v", m1["remote"])
	}

	_ = logger
}

func TestProfilerExport(t *testing.T) {
	tmpDir := t.TempDir()
	cpuFile := filepath.Join(tmpDir, "cpu.pprof")
	memFile := filepath.Join(tmpDir, "mem.pprof")
	traceFile := filepath.Join(tmpDir, "trace.out")

	p, err := StartProfiler(cpuFile, memFile, traceFile)
	if err != nil {
		t.Fatalf("StartProfiler failed: %v", err)
	}
	if p == nil {
		t.Fatalf("expected non-nil profiler")
	}

	// 模拟计算负载与内存分配
	var dummy [][]byte
	for i := 0; i < 50; i++ {
		dummy = append(dummy, bytes.Repeat([]byte("piper-scale"), 1024))
		time.Sleep(1 * time.Millisecond)
	}
	_ = dummy

	if err := p.Stop(); err != nil {
		t.Fatalf("Stop failed: %v", err)
	}

	// 校验产物文件均已创建且不为空
	for _, f := range []string{cpuFile, memFile, traceFile} {
		fi, err := os.Stat(f)
		if err != nil {
			t.Fatalf("profile file %s was not created: %v", f, err)
		}
		if fi.Size() == 0 {
			t.Fatalf("profile file %s is empty", f)
		}
	}
}
