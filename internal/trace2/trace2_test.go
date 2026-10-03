package trace2

import (
	"bufio"
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestTrace2EventJSONLFile(t *testing.T) {
	logFile := filepath.Join(t.TempDir(), "trace2.event.jsonl")
	t.Setenv("GIT_TRACE2_EVENT", logFile)
	t.Setenv("GIT_TRACE2_PERF", "")
	t.Setenv("GIT_TRACE2", "")

	session := Initialize("status", []string{"gogit", "status"})
	if session == nil {
		t.Fatalf("expected non-nil session when GIT_TRACE2_EVENT is set")
	}

	RegionEnter("status", "worktree_scan")
	time.Sleep(5 * time.Millisecond)
	Data("status", "scanned_files", 42)
	RegionLeave("status", "worktree_scan")

	Close(0)

	// 读取并解析生成的 JSONL 事件
	content, err := os.ReadFile(logFile)
	if err != nil {
		t.Fatalf("failed to read trace2 log file: %v", err)
	}

	lines := make([]map[string]any, 0)
	scanner := bufio.NewScanner(os.NewFile(0, "mock"))
	_ = scanner
	for _, l := range splitLines(content) {
		if len(l) == 0 {
			continue
		}
		var m map[string]any
		if err := json.Unmarshal(l, &m); err != nil {
			t.Fatalf("invalid json line %s: %v", string(l), err)
		}
		lines = append(lines, m)
	}

	// 验证事件流按顺序发出
	expectedEvents := []string{"version", "start", "cmd_name", "region_enter", "data", "region_leave", "atexit"}
	if len(lines) != len(expectedEvents) {
		t.Fatalf("expected %d events, got %d: %+v", len(expectedEvents), len(lines), lines)
	}

	for i, exp := range expectedEvents {
		if lines[i]["event"] != exp {
			t.Errorf("event %d expected %s, got %v", i, exp, lines[i]["event"])
		}
		if lines[i]["sid"] == nil || lines[i]["sid"] == "" {
			t.Errorf("event %d missing sid", i)
		}
		if lines[i]["t_abs"] == nil {
			t.Errorf("event %d missing t_abs", i)
		}
	}

	// 验证 region_leave 中的相对耗时 t_rel
	leaveEvt := lines[5]
	tRel, ok := leaveEvt["t_rel"].(float64)
	if !ok || tRel <= 0 {
		t.Errorf("expected positive t_rel in region_leave, got %v", leaveEvt["t_rel"])
	}
}

func TestTrace2UnixDomainSocket(t *testing.T) {
	sockDir := t.TempDir()
	sockPath := filepath.Join(sockDir, "trace2.sock")

	listener, err := net.Listen("unix", sockPath)
	if err != nil {
		t.Fatalf("failed to listen on unix socket: %v", err)
	}
	defer listener.Close()

	receivedCh := make(chan []byte, 100)
	go func() {
		conn, err := listener.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		scanner := bufio.NewScanner(conn)
		for scanner.Scan() {
			receivedCh <- append([]byte(nil), scanner.Bytes()...)
		}
	}()

	t.Setenv("GIT_TRACE2_EVENT", "af_unix:"+sockPath)
	t.Setenv("GIT_TRACE2_PERF", "")
	t.Setenv("GIT_TRACE2", "")

	session := Initialize("fetch", []string{"gogit", "fetch"})
	if session == nil {
		t.Fatalf("expected non-nil session for af_unix")
	}

	Data("transport", "bytes_read", 1024)
	Close(0)

	// 等待接收 socket 消息
	select {
	case data := <-receivedCh:
		var m map[string]any
		if err := json.Unmarshal(data, &m); err != nil {
			t.Fatalf("invalid json received over socket: %v", err)
		}
		if m["event"] != "version" {
			t.Errorf("first event expected version, got %v", m["event"])
		}
	case <-time.After(2 * time.Second):
		t.Fatalf("timeout waiting for socket event")
	}
}

func splitLines(data []byte) [][]byte {
	var res [][]byte
	start := 0
	for i, b := range data {
		if b == '\n' {
			res = append(res, data[start:i])
			start = i + 1
		}
	}
	if start < len(data) {
		res = append(res, data[start:])
	}
	return res
}
