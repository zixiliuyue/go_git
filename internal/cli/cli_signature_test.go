package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestCLISignedCommitAndVerify 测试 commit -S 生成附带 SSHSIG 签名的提交并使用 verify-commit 进行校验
func TestCLISignedCommitAndVerify(t *testing.T) {
	tmpDir := t.TempDir()
	origDir, _ := os.Getwd()
	_ = os.Chdir(tmpDir)
	defer func() { _ = os.Chdir(origDir) }()

	// 1. 初始化空仓库
	var stdout, stderr bytes.Buffer
	code := RunWithContext(&Context{
		Args:   []string{"init"},
		Stdout: &stdout,
		Stderr: &stderr,
	})
	if code != ExitSuccess {
		t.Fatalf("init failed: %s", stderr.String())
	}

	// 配置用户信息
	code = RunWithContext(&Context{
		Args:   []string{"config", "user.name", "Security Engineer"},
		Stdout: &stdout,
		Stderr: &stderr,
	})
	code = RunWithContext(&Context{
		Args:   []string{"config", "user.email", "sec@google.com"},
		Stdout: &stdout,
		Stderr: &stderr,
	})

	// 2. 准备文件并提交带签名提交 (commit -S)
	f1 := filepath.Join(tmpDir, "secure_code.go")
	_ = os.WriteFile(f1, []byte("package main\n\nfunc main() {}\n"), 0644)

	stdout.Reset()
	stderr.Reset()
	code = RunWithContext(&Context{
		Args:   []string{"add", "secure_code.go"},
		Stdout: &stdout,
		Stderr: &stderr,
	})
	if code != ExitSuccess {
		t.Fatalf("add failed: %s", stderr.String())
	}

	stdout.Reset()
	stderr.Reset()
	code = RunWithContext(&Context{
		Args:   []string{"commit", "-S", "-m", "feat: signed secure commit"},
		Stdout: &stdout,
		Stderr: &stderr,
	})
	if code != ExitSuccess {
		t.Fatalf("commit -S failed: %s", stderr.String())
	}

	// 3. 执行 gogit verify-commit HEAD 验证有效签名
	stdout.Reset()
	stderr.Reset()
	code = RunWithContext(&Context{
		Args:   []string{"verify-commit", "--verbose", "HEAD"},
		Stdout: &stdout,
		Stderr: &stderr,
	})
	if code != ExitSuccess {
		t.Fatalf("verify-commit HEAD failed (exit %d): %s", code, stderr.String())
	}
	out := stdout.String()
	if !strings.Contains(out, "Good \"git\" signature") || !strings.Contains(out, "ED25519") {
		t.Errorf("expected Good signature message in stdout, got: %s", out)
	}

	// 4. 创建未签名的普通提交，验证 verify-commit 能够准确报错拦截
	f2 := filepath.Join(tmpDir, "unsigned.txt")
	_ = os.WriteFile(f2, []byte("unsigned\n"), 0644)
	_ = RunWithContext(&Context{Args: []string{"add", "unsigned.txt"}, Stdout: &stdout, Stderr: &stderr})
	stdout.Reset()
	stderr.Reset()
	code = RunWithContext(&Context{
		Args:   []string{"commit", "-m", "chore: unsigned commit"},
		Stdout: &stdout,
		Stderr: &stderr,
	})
	if code != ExitSuccess {
		t.Fatalf("unsigned commit failed: %s", stderr.String())
	}

	stdout.Reset()
	stderr.Reset()
	code = RunWithContext(&Context{
		Args:   []string{"verify-commit", "HEAD"},
		Stdout: &stdout,
		Stderr: &stderr,
	})
	if code != ExitGeneral {
		t.Fatalf("expected verify-commit on unsigned commit to fail with ExitGeneral, got exit %d", code)
	}
	if !strings.Contains(stderr.String(), "未找到数字签名") && !strings.Contains(stderr.String(), "no signature found") {
		t.Errorf("expected no signature error message, got: %s", stderr.String())
	}
}

// TestCLISignedTagAndVerify 测试 tag -s 创建数字签名标签并使用 verify-tag 校验
func TestCLISignedTagAndVerify(t *testing.T) {
	tmpDir := t.TempDir()
	origDir, _ := os.Getwd()
	_ = os.Chdir(tmpDir)
	defer func() { _ = os.Chdir(origDir) }()

	var stdout, stderr bytes.Buffer
	_ = RunWithContext(&Context{Args: []string{"init"}, Stdout: &stdout, Stderr: &stderr})
	_ = RunWithContext(&Context{Args: []string{"config", "user.name", "Release Manager"}, Stdout: &stdout, Stderr: &stderr})
	_ = RunWithContext(&Context{Args: []string{"config", "user.email", "release@google.com"}, Stdout: &stdout, Stderr: &stderr})

	f1 := filepath.Join(tmpDir, "version.txt")
	_ = os.WriteFile(f1, []byte("v1.0.0\n"), 0644)
	_ = RunWithContext(&Context{Args: []string{"add", "version.txt"}, Stdout: &stdout, Stderr: &stderr})
	_ = RunWithContext(&Context{Args: []string{"commit", "-m", "release v1.0.0"}, Stdout: &stdout, Stderr: &stderr})

	// 1. 创建签名标签 (tag -s)
	stdout.Reset()
	stderr.Reset()
	code := RunWithContext(&Context{
		Args:   []string{"tag", "-s", "-m", "Official Signed Release v1.0.0", "v1.0.0"},
		Stdout: &stdout,
		Stderr: &stderr,
	})
	if code != ExitSuccess {
		t.Fatalf("tag -s failed: %s", stderr.String())
	}

	// 2. 校验签名标签 (verify-tag)
	stdout.Reset()
	stderr.Reset()
	code = RunWithContext(&Context{
		Args:   []string{"verify-tag", "--verbose", "v1.0.0"},
		Stdout: &stdout,
		Stderr: &stderr,
	})
	if code != ExitSuccess {
		t.Fatalf("verify-tag v1.0.0 failed (exit %d): %s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "Good \"git\" signature") {
		t.Errorf("expected good signature output, got: %s", stdout.String())
	}

	// 3. 创建普通轻量标签并校验（应当拒绝）
	_ = RunWithContext(&Context{Args: []string{"tag", "lightweight-v1"}, Stdout: &stdout, Stderr: &stderr})
	stdout.Reset()
	stderr.Reset()
	code = RunWithContext(&Context{
		Args:   []string{"verify-tag", "lightweight-v1"},
		Stdout: &stdout,
		Stderr: &stderr,
	})
	if code != ExitGeneral {
		t.Fatalf("expected verify-tag on lightweight tag to fail with ExitGeneral, got exit %d", code)
	}
}
