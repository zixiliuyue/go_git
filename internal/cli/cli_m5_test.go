package cli

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// setupM5TestRepo 创建标准测试仓库
func setupM5TestRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()

	cmd := exec.Command("git", "init", "-b", "main", dir)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git init 失败: %v, out: %s", err, string(out))
	}

	_ = exec.Command("git", "-C", dir, "config", "user.name", "M5 Tester").Run()
	_ = exec.Command("git", "-C", dir, "config", "user.email", "m5@tester.com").Run()
	_ = exec.Command("git", "-C", dir, "config", "core.excludesfile", "").Run()

	return dir
}

func runGogitInDir(t *testing.T, dir string, args ...string) (string, string, int) {
	t.Helper()
	origWd, err := os.Getwd()
	if err != nil {
		t.Fatalf("获取当前目录失败: %v", err)
	}
	defer func() { _ = os.Chdir(origWd) }()

	if err := os.Chdir(dir); err != nil {
		t.Fatalf("切换目录失败: %v", err)
	}

	var stdout, stderr bytes.Buffer
	ctx := &Context{
		Args:   args,
		Stdin:  strings.NewReader(""),
		Stdout: &stdout,
		Stderr: &stderr,
	}

	code := RunWithContext(ctx)
	return stdout.String(), stderr.String(), code
}

func TestM5CLI_GC_Repack_Prune_PackRefs(t *testing.T) {
	dir := setupM5TestRepo(t)

	// 创建几个提交与标签
	for i := 1; i <= 3; i++ {
		fn := fmt.Sprintf("file_%d.dat", i)
		_ = os.WriteFile(filepath.Join(dir, fn), []byte(fmt.Sprintf("hello %d", i)), 0644)
		_, _, code := runGogitInDir(t, dir, "add", fn)
		if code != ExitSuccess {
			t.Fatalf("gogit add 失败")
		}
		_, _, code = runGogitInDir(t, dir, "commit", "-m", fmt.Sprintf("commit %d", i))
		if code != ExitSuccess {
			t.Fatalf("gogit commit 失败")
		}
	}
	_, _, _ = runGogitInDir(t, dir, "tag", "-a", "v1.0", "-m", "tag v1.0")

	// 1. pack-refs 测试
	_, _, code := runGogitInDir(t, dir, "pack-refs", "--all")
	if code != ExitSuccess {
		t.Fatalf("gogit pack-refs 失败")
	}
	if _, err := os.Stat(filepath.Join(dir, ".git", "packed-refs")); err != nil {
		t.Fatalf(".git/packed-refs 不存在")
	}

	// 2. repack 测试
	out, _, code := runGogitInDir(t, dir, "repack", "-a", "-d")
	if code != ExitSuccess {
		t.Fatalf("gogit repack 失败: %s", out)
	}

	// 3. verify-pack 测试
	packEntries, _ := os.ReadDir(filepath.Join(dir, ".git", "objects", "pack"))
	var packPath string
	for _, pe := range packEntries {
		if strings.HasSuffix(pe.Name(), ".pack") {
			packPath = filepath.Join(dir, ".git", "objects", "pack", pe.Name())
			break
		}
	}
	if packPath == "" {
		t.Fatal("未找到生成的 pack 文件")
	}

	out, _, code = runGogitInDir(t, dir, "verify-pack", "-v", packPath)
	if code != ExitSuccess || !strings.Contains(out, "ok") {
		t.Fatalf("gogit verify-pack 失败: code=%d, out=%s", code, out)
	}

	// 4. multi-pack-index 测试
	out, _, code = runGogitInDir(t, dir, "multi-pack-index", "write")
	if code != ExitSuccess {
		t.Fatalf("gogit multi-pack-index write 失败: %s", out)
	}
	out, _, code = runGogitInDir(t, dir, "multi-pack-index", "verify")
	if code != ExitSuccess || !strings.Contains(out, "ok") {
		t.Fatalf("gogit multi-pack-index verify 失败: %s", out)
	}

	// 5. gc 测试
	out, errOut, code := runGogitInDir(t, dir, "gc", "--prune=now")
	if code != ExitSuccess {
		t.Fatalf("gogit gc 失败: code=%d, out=%s, err=%s", code, out, errOut)
	}

	// 6. 用真实 git fsck 校验仓库完整性
	fsckCmd := exec.Command("git", "-C", dir, "fsck", "--strict")
	if fsckOut, err := fsckCmd.CombinedOutput(); err != nil {
		t.Fatalf("git fsck 校验失败: %v, out: %s", err, string(fsckOut))
	}
}

func TestM5CLI_Replace(t *testing.T) {
	dir := setupM5TestRepo(t)

	// 创建两个提交
	_ = os.WriteFile(filepath.Join(dir, "doc.txt"), []byte("v1 content"), 0644)
	runGogitInDir(t, dir, "add", "doc.txt")
	runGogitInDir(t, dir, "commit", "-m", "commit 1")
	c1Out, _, _ := runGogitInDir(t, dir, "rev-parse", "HEAD")
	c1 := strings.TrimSpace(c1Out)

	_ = os.WriteFile(filepath.Join(dir, "doc.txt"), []byte("v2 content"), 0644)
	runGogitInDir(t, dir, "add", "doc.txt")
	runGogitInDir(t, dir, "commit", "-m", "commit 2")
	c2Out, _, _ := runGogitInDir(t, dir, "rev-parse", "HEAD")
	c2 := strings.TrimSpace(c2Out)

	// 创建 replace: 用 c1 替换 c2
	_, _, code := runGogitInDir(t, dir, "replace", c2, c1)
	if code != ExitSuccess {
		t.Fatalf("gogit replace 创建失败")
	}

	// 列出 replace
	out, _, code := runGogitInDir(t, dir, "replace", "-l")
	if code != ExitSuccess || !strings.Contains(out, c2) {
		t.Fatalf("gogit replace -l 输出未包含 %s: %s", c2, out)
	}

	// 验证 cat-file 读取 c2 时被透明重定向为 c1 的内容
	catOut, _, code := runGogitInDir(t, dir, "cat-file", "-p", c2)
	if code != ExitSuccess || !strings.Contains(catOut, "commit 1") {
		t.Fatalf("gogit cat-file 预期透明替换为 commit 1，实际输出: %s", catOut)
	}

	// 删除 replace
	_, _, code = runGogitInDir(t, dir, "replace", "-d", c2)
	if code != ExitSuccess {
		t.Fatalf("gogit replace -d 失败")
	}

	// 删除后再 cat-file 应恢复为 c2
	catOut2, _, _ := runGogitInDir(t, dir, "cat-file", "-p", c2)
	if !strings.Contains(catOut2, "commit 2") {
		t.Fatalf("gogit cat-file 预期恢复为 commit 2，实际输出: %s", catOut2)
	}
}

func TestM5CLI_Worktree(t *testing.T) {
	dir := setupM5TestRepo(t)

	_ = os.WriteFile(filepath.Join(dir, "main.go"), []byte("package main"), 0644)
	runGogitInDir(t, dir, "add", "main.go")
	runGogitInDir(t, dir, "commit", "-m", "init commit")

	wtDir := filepath.Join(t.TempDir(), "wt1")

	// 添加工作区
	out, stderr, code := runGogitInDir(t, dir, "worktree", "add", wtDir, "feat")
	if code != ExitSuccess {
		t.Fatalf("gogit worktree add 失败: out=%s, err=%s", out, stderr)
	}

	// 验证工作区目录存在且包含 main.go
	if data, err := os.ReadFile(filepath.Join(wtDir, "main.go")); err != nil || string(data) != "package main" {
		t.Fatalf("工作区文件检出不正确: %v", err)
	}

	// 列出工作区
	listOut, _, code := runGogitInDir(t, dir, "worktree", "list")
	if code != ExitSuccess || !strings.Contains(listOut, wtDir) {
		t.Fatalf("gogit worktree list 未包含附加工作区: %s", listOut)
	}

	// 移除工作区
	_, _, code = runGogitInDir(t, dir, "worktree", "remove", wtDir)
	if code != ExitSuccess {
		t.Fatalf("gogit worktree remove 失败")
	}

	// 再次 list 应不再包含
	listOut2, _, _ := runGogitInDir(t, dir, "worktree", "list")
	if strings.Contains(listOut2, wtDir) {
		t.Fatalf("已移除的工作区仍然出现在 list 中: %s", listOut2)
	}
}

func TestM5CLI_SparseCheckout(t *testing.T) {
	dir := setupM5TestRepo(t)

	_ = os.MkdirAll(filepath.Join(dir, "frontend"), 0755)
	_ = os.MkdirAll(filepath.Join(dir, "backend"), 0755)
	_ = os.WriteFile(filepath.Join(dir, "frontend", "app.js"), []byte("console.log('fe')"), 0644)
	_ = os.WriteFile(filepath.Join(dir, "backend", "server.go"), []byte("package main"), 0644)

	runGogitInDir(t, dir, "add", "frontend/app.js", "backend/server.go")
	runGogitInDir(t, dir, "commit", "-m", "add fe and be")

	// 1. 初始化稀疏检出
	_, _, code := runGogitInDir(t, dir, "sparse-checkout", "init")
	if code != ExitSuccess {
		t.Fatalf("sparse-checkout init 失败")
	}

	// 2. 设置仅检出 backend/
	_, _, code = runGogitInDir(t, dir, "sparse-checkout", "set", "backend/")
	if code != ExitSuccess {
		t.Fatalf("sparse-checkout set 失败")
	}

	// 验证 sparse-checkout list
	listOut, _, _ := runGogitInDir(t, dir, "sparse-checkout", "list")
	if !strings.Contains(listOut, "backend/") {
		t.Fatalf("sparse-checkout list 预期包含 backend/，实际: %s", listOut)
	}

	// 3. 禁用稀疏检出
	_, _, code = runGogitInDir(t, dir, "sparse-checkout", "disable")
	if code != ExitSuccess {
		t.Fatalf("sparse-checkout disable 失败")
	}
}

func TestM5CLI_Hooks(t *testing.T) {
	dir := setupM5TestRepo(t)

	hooksDir := filepath.Join(dir, ".git", "hooks")
	_ = os.MkdirAll(hooksDir, 0755)

	// 1. 测试 pre-commit 阻断
	preCommitPath := filepath.Join(hooksDir, "pre-commit")
	_ = os.WriteFile(preCommitPath, []byte("#!/bin/sh\necho 'pre-commit block'\nexit 1\n"), 0755)

	_ = os.WriteFile(filepath.Join(dir, "test.txt"), []byte("hello"), 0644)
	runGogitInDir(t, dir, "add", "test.txt")

	_, _, code := runGogitInDir(t, dir, "commit", "-m", "blocked commit")
	if code == ExitSuccess {
		t.Fatalf("预期 pre-commit 钩子阻断提交，但提交成功了")
	}

	// 2. 放行 pre-commit 并测试 commit-msg 修改
	_ = os.WriteFile(preCommitPath, []byte("#!/bin/sh\nexit 0\n"), 0755)

	commitMsgPath := filepath.Join(hooksDir, "commit-msg")
	_ = os.WriteFile(commitMsgPath, []byte("#!/bin/sh\necho 'prefix: '\"$(cat \"$1\")\" > \"$1\"\nexit 0\n"), 0755)

	// 设置 post-commit 标记
	markerFile := filepath.Join(dir, "hook_ran.marker")
	postCommitPath := filepath.Join(hooksDir, "post-commit")
	_ = os.WriteFile(postCommitPath, []byte(fmt.Sprintf("#!/bin/sh\ntouch '%s'\nexit 0\n", markerFile)), 0755)

	_, _, code = runGogitInDir(t, dir, "commit", "-m", "original message")
	if code != ExitSuccess {
		t.Fatalf("提交失败")
	}

	// 验证 post-commit 是否被调用
	if _, err := os.Stat(markerFile); err != nil {
		t.Fatalf("post-commit 钩子未被执行")
	}

	// 验证 commit-msg 是否成功修改提交信息
	logOut, _, _ := runGogitInDir(t, dir, "log", "-1")
	if !strings.Contains(logOut, "prefix: original message") {
		t.Fatalf("commit-msg 预期添加前缀，实际 log 为: %s", logOut)
	}
}

func TestM5CLI_Submodule(t *testing.T) {
	mainDir := setupM5TestRepo(t)
	subDir := setupM5TestRepo(t)

	// 在子仓库中提交文件
	_ = os.WriteFile(filepath.Join(subDir, "lib.go"), []byte("package lib"), 0644)
	runGogitInDir(t, subDir, "add", "lib.go")
	runGogitInDir(t, subDir, "commit", "-m", "sub init")
	subHead, _, _ := runGogitInDir(t, subDir, "rev-parse", "HEAD")
	subHead = strings.TrimSpace(subHead)

	// 在主仓库中初始化首个提交
	_ = os.WriteFile(filepath.Join(mainDir, "main.go"), []byte("package main"), 0644)
	runGogitInDir(t, mainDir, "add", "main.go")
	runGogitInDir(t, mainDir, "commit", "-m", "main init")

	// 添加子模块
	out, stderr, code := runGogitInDir(t, mainDir, "submodule", "add", subDir, "libs/sub")
	if code != ExitSuccess {
		t.Fatalf("submodule add 失败: out=%s, err=%s", out, stderr)
	}

	// 验证 status
	statusOut, _, code := runGogitInDir(t, mainDir, "submodule", "status")
	if code != ExitSuccess || !strings.Contains(statusOut, "libs/sub") {
		t.Fatalf("submodule status 未包含 libs/sub: %s", statusOut)
	}

	// 验证 init
	initOut, _, code := runGogitInDir(t, mainDir, "submodule", "init")
	if code != ExitSuccess || !strings.Contains(initOut, "libs/sub") {
		t.Fatalf("submodule init 失败: %s", initOut)
	}

	// 提交并在 git fsck 下校验
	runGogitInDir(t, mainDir, "commit", "-m", "add submodule")
	fsckCmd := exec.Command("git", "-C", mainDir, "fsck", "--strict")
	if fsckOut, err := fsckCmd.CombinedOutput(); err != nil {
		t.Fatalf("git fsck 严格校验失败: %v, out: %s", err, string(fsckOut))
	}
}
