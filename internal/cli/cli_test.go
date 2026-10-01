package cli

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestM1WorkflowEndToEnd 完整测试 M1 对象模型与最小工作流的闭环与 Git 兼容性
func TestM1WorkflowEndToEnd(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "gogit_m1_e2e_*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tempDir)

	repoDir := filepath.Join(tempDir, "testrepo")

	// 1. gogit init
	var stdout, stderr bytes.Buffer
	ctx := &Context{
		Args:   []string{"init", "-b", "main", repoDir},
		Stdout: &stdout,
		Stderr: &stderr,
	}
	if code := RunWithContext(ctx); code != ExitSuccess {
		t.Fatalf("gogit init 失败, code=%d, err=%s", code, stderr.String())
	}

	// 2. 在工作区创建文件和子目录（命名为 .go 结尾，避开系统全局 gitignore）
	file1Path := filepath.Join(repoDir, "main.go")
	if err := os.WriteFile(file1Path, []byte("package main\n\nfunc main() {}\n"), 0644); err != nil {
		t.Fatal(err)
	}
	subDirPath := filepath.Join(repoDir, "pkg", "util")
	if err := os.MkdirAll(subDirPath, 0755); err != nil {
		t.Fatal(err)
	}
	file2Path := filepath.Join(subDirPath, "helper.go")
	if err := os.WriteFile(file2Path, []byte("package util\n"), 0755); err != nil {
		t.Fatal(err)
	}

	// 3. gogit add .
	origWd, _ := os.Getwd()
	_ = os.Chdir(repoDir)
	defer os.Chdir(origWd)

	stdout.Reset()
	stderr.Reset()
	ctx = &Context{
		Args:   []string{"add", "."},
		Stdout: &stdout,
		Stderr: &stderr,
	}
	if code := RunWithContext(ctx); code != ExitSuccess {
		t.Fatalf("gogit add 失败: %s", stderr.String())
	}

	// 4. gogit commit
	stdout.Reset()
	stderr.Reset()
	ctx = &Context{
		Args:   []string{"commit", "-m", "first commit from gogit"},
		Stdout: &stdout,
		Stderr: &stderr,
	}
	if code := RunWithContext(ctx); code != ExitSuccess {
		t.Fatalf("gogit commit 失败: %s", stderr.String())
	}

	// 5. 验收门检查 1: 真实 git fsck --strict 零错误
	fsckCmd := exec.Command("git", "fsck", "--strict")
	fsckOut, err := fsckCmd.CombinedOutput()
	if err != nil {
		t.Fatalf("真实 git fsck --strict 失败: %v, 输出: %s", err, string(fsckOut))
	}

	// 6. 验收门检查 2: rev-parse HEAD 与 git rev-parse HEAD 完全一致
	stdout.Reset()
	stderr.Reset()
	ctx = &Context{
		Args:   []string{"rev-parse", "HEAD"},
		Stdout: &stdout,
		Stderr: &stderr,
	}
	if code := RunWithContext(ctx); code != ExitSuccess {
		t.Fatalf("gogit rev-parse HEAD 失败: %s", stderr.String())
	}
	gogitHead := strings.TrimSpace(stdout.String())

	gitRevCmd := exec.Command("git", "rev-parse", "HEAD")
	gitRevOut, err := gitRevCmd.CombinedOutput()
	if err != nil {
		t.Fatalf("真实 git rev-parse 失败: %v", err)
	}
	gitHead := strings.TrimSpace(string(gitRevOut))

	if gogitHead != gitHead {
		t.Fatalf("HEAD 不一致! gogit=%s, git=%s", gogitHead, gitHead)
	}

	// 7. 验收门检查 3: cat-file -p 输出与 git cat-file -p 完全一致
	stdout.Reset()
	stderr.Reset()
	ctx = &Context{
		Args:   []string{"cat-file", "-p", "HEAD"},
		Stdout: &stdout,
		Stderr: &stderr,
	}
	if code := RunWithContext(ctx); code != ExitSuccess {
		t.Fatalf("gogit cat-file -p HEAD 失败: %s", stderr.String())
	}
	gogitCatOut := strings.TrimSpace(stdout.String())

	gitCatCmd := exec.Command("git", "cat-file", "-p", "HEAD")
	gitCatOutBytes, err := gitCatCmd.CombinedOutput()
	if err != nil {
		t.Fatalf("真实 git cat-file 失败: %v", err)
	}
	gitCatOut := strings.TrimSpace(string(gitCatOutBytes))

	if gogitCatOut != gitCatOut {
		t.Fatalf("cat-file -p 输出不一致!\ngogit:\n%s\ngit:\n%s", gogitCatOut, gitCatOut)
	}

	// 8. 验收门检查 4: ls-tree -r HEAD 输出与 git ls-tree -r HEAD 完全一致
	stdout.Reset()
	stderr.Reset()
	ctx = &Context{
		Args:   []string{"ls-tree", "-r", "HEAD"},
		Stdout: &stdout,
		Stderr: &stderr,
	}
	if code := RunWithContext(ctx); code != ExitSuccess {
		t.Fatalf("gogit ls-tree -r HEAD 失败: %s", stderr.String())
	}
	gogitLsOut := strings.TrimSpace(stdout.String())

	gitLsCmd := exec.Command("git", "ls-tree", "-r", "HEAD")
	gitLsOutBytes, err := gitLsCmd.CombinedOutput()
	if err != nil {
		t.Fatalf("真实 git ls-tree 失败: %v", err)
	}
	gitLsOut := strings.TrimSpace(string(gitLsOutBytes))

	if gogitLsOut != gitLsOut {
		t.Fatalf("ls-tree -r 输出不一致!\ngogit:\n%s\ngit:\n%s", gogitLsOut, gitLsOut)
	}

	// 9. 验收门检查 5: gogit 自身的 fsck 同样返回成功
	stdout.Reset()
	stderr.Reset()
	ctx = &Context{
		Args:   []string{"fsck", "--strict"},
		Stdout: &stdout,
		Stderr: &stderr,
	}
	if code := RunWithContext(ctx); code != ExitSuccess {
		t.Fatalf("gogit fsck 校验失败: %s", stderr.String())
	}
}
