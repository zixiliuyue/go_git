package cli

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestCLICommitGraph(t *testing.T) {
	tmpDir := t.TempDir()

	origDir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.Chdir(origDir) }()

	if err := os.Chdir(tmpDir); err != nil {
		t.Fatal(err)
	}

	// 1. 初始化仓库并提交若干文件
	if code := Run([]string{"init", "."}); code != 0 {
		t.Fatalf("gogit init failed with code %d", code)
	}

	_ = os.WriteFile("a.txt", []byte("hello A"), 0644)
	if code := Run([]string{"add", "a.txt"}); code != 0 {
		t.Fatalf("gogit add failed with code %d", code)
	}
	if code := Run([]string{"commit", "-m", "first commit"}); code != 0 {
		t.Fatalf("gogit commit failed with code %d", code)
	}

	_ = os.WriteFile("b.txt", []byte("hello B"), 0644)
	if code := Run([]string{"add", "b.txt"}); code != 0 {
		t.Fatalf("gogit add b failed with code %d", code)
	}
	if code := Run([]string{"commit", "-m", "second commit"}); code != 0 {
		t.Fatalf("gogit commit 2 failed with code %d", code)
	}

	// 2. 执行 gogit commit-graph write
	var stdout, stderr bytes.Buffer
	ctx := &Context{
		Args:   []string{"commit-graph", "write"},
		Stdin:  os.Stdin,
		Stdout: &stdout,
		Stderr: &stderr,
	}
	if code := RunWithContext(ctx); code != 0 {
		t.Fatalf("commit-graph write failed: code %d, stderr: %s", code, stderr.String())
	}

	graphFile := filepath.Join(tmpDir, ".git", "objects", "info", "commit-graph")
	if _, err := os.Stat(graphFile); err != nil {
		t.Fatalf("expected commit-graph file to exist: %v", err)
	}

	// 3. 执行 gogit commit-graph verify
	stdout.Reset()
	stderr.Reset()
	ctx = &Context{
		Args:   []string{"commit-graph", "verify"},
		Stdin:  os.Stdin,
		Stdout: &stdout,
		Stderr: &stderr,
	}
	if code := RunWithContext(ctx); code != 0 {
		t.Fatalf("commit-graph verify failed: code %d, stderr: %s", code, stderr.String())
	}

	// 4. 调用原生 git commit-graph verify
	gitCmd := exec.Command("git", "commit-graph", "verify")
	gitCmd.Dir = tmpDir
	gitCmd.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null")
	if out, err := gitCmd.CombinedOutput(); err != nil {
		t.Fatalf("native git commit-graph verify failed: %v\nOutput: %s", err, string(out))
	}
}
