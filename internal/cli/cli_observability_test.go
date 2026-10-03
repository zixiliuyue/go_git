package cli

import (
	"bufio"
	"bytes"
	"encoding/json"
	"gogit/internal/repo"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCLITrace2EventGeneration(t *testing.T) {
	tmpDir := t.TempDir()
	r, err := repo.InitRepository(tmpDir, false, "main")
	if err != nil {
		t.Fatalf("InitRepository failed: %v", err)
	}

	trace2File := filepath.Join(tmpDir, "trace2_events.jsonl")
	t.Setenv("GIT_TRACE2_EVENT", trace2File)
	t.Setenv("GIT_TRACE2_PERF", "")
	t.Setenv("GIT_TRACE2", "")

	origWd, _ := os.Getwd()
	defer os.Chdir(origWd)
	_ = os.Chdir(r.WorkTree)

	var stdout, stderr bytes.Buffer
	code := RunWithContext(&Context{
		Args:   []string{"status"},
		Stdout: &stdout,
		Stderr: &stderr,
	})
	if code != ExitSuccess {
		t.Fatalf("status failed (%d): %s\n%s", code, stdout.String(), stderr.String())
	}

	// 验证生成的 trace2 文件
	raw, err := os.ReadFile(trace2File)
	if err != nil {
		t.Fatalf("failed to read trace2 output: %v", err)
	}

	var events []map[string]any
	scanner := bufio.NewScanner(bytes.NewReader(raw))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var m map[string]any
		if err := json.Unmarshal([]byte(line), &m); err != nil {
			t.Fatalf("invalid json line %s: %v", line, err)
		}
		events = append(events, m)
	}

	foundCmdName := false
	foundRegionComputeStatus := false
	foundAtexit := false

	for _, e := range events {
		switch e["event"] {
		case "cmd_name":
			if e["name"] == "status" {
				foundCmdName = true
			}
		case "region_enter":
			if e["category"] == "status" && e["label"] == "compute_status" {
				foundRegionComputeStatus = true
			}
		case "atexit":
			if e["code"] == float64(0) {
				foundAtexit = true
			}
		}
	}

	if !foundCmdName {
		t.Errorf("missing cmd_name event for status")
	}
	if !foundRegionComputeStatus {
		t.Errorf("missing region_enter event for status.compute_status")
	}
	if !foundAtexit {
		t.Errorf("missing atexit event with code 0")
	}
}

func TestCLIProfilingFlags(t *testing.T) {
	tmpDir := t.TempDir()
	r, err := repo.InitRepository(tmpDir, false, "main")
	if err != nil {
		t.Fatalf("InitRepository failed: %v", err)
	}

	cpuFile := filepath.Join(tmpDir, "profile_cpu.pprof")
	memFile := filepath.Join(tmpDir, "profile_mem.pprof")
	traceFile := filepath.Join(tmpDir, "profile_trace.out")

	origWd, _ := os.Getwd()
	defer os.Chdir(origWd)
	_ = os.Chdir(r.WorkTree)

	var stdout, stderr bytes.Buffer
	code := RunWithContext(&Context{
		Args: []string{
			"--cpuprofile=" + cpuFile,
			"--memprofile=" + memFile,
			"--trace=" + traceFile,
			"status",
		},
		Stdout: &stdout,
		Stderr: &stderr,
	})
	if code != ExitSuccess {
		t.Fatalf("status with profiling flags failed (%d): %s\n%s", code, stdout.String(), stderr.String())
	}

	// 校验生成的 profile 文件
	for _, f := range []string{cpuFile, memFile, traceFile} {
		fi, err := os.Stat(f)
		if err != nil {
			t.Fatalf("expected profile file %s to exist: %v", f, err)
		}
		if fi.Size() == 0 {
			t.Fatalf("profile file %s should not be empty", f)
		}
	}
}

func TestCLIStructuredLoggingJSON(t *testing.T) {
	tmpDir := t.TempDir()
	r, err := repo.InitRepository(tmpDir, false, "main")
	if err != nil {
		t.Fatalf("InitRepository failed: %v", err)
	}

	logFile := filepath.Join(tmpDir, "cli_slog.json")
	t.Setenv("GOGIT_LOG_FORMAT", "json")
	t.Setenv("GOGIT_LOG_LEVEL", "debug")
	t.Setenv("GOGIT_LOG_FILE", logFile)

	origWd, _ := os.Getwd()
	defer os.Chdir(origWd)
	_ = os.Chdir(r.WorkTree)

	var stdout, stderr bytes.Buffer
	code := RunWithContext(&Context{
		Args:   []string{"status"},
		Stdout: &stdout,
		Stderr: &stderr,
	})
	if code != ExitSuccess {
		t.Fatalf("status failed (%d): %s\n%s", code, stdout.String(), stderr.String())
	}

	raw, err := os.ReadFile(logFile)
	if err != nil {
		t.Fatalf("failed to read json log file: %v", err)
	}

	scanner := bufio.NewScanner(bytes.NewReader(raw))
	foundWorktreeLog := false
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var m map[string]any
		if err := json.Unmarshal([]byte(line), &m); err != nil {
			t.Fatalf("invalid json log line: %s", line)
		}
		if m["subsystem"] == "worktree" {
			foundWorktreeLog = true
		}
	}

	if !foundWorktreeLog {
		t.Errorf("expected structured log from worktree subsystem, got raw:\n%s", string(raw))
	}
}
