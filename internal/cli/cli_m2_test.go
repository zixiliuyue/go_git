package cli

import (
	"bytes"
	"os"
	"os/exec"
	"strings"
	"testing"
)

func TestM2Workflow(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "gogit_m2_test_*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tempDir)

	origWd, _ := os.Getwd()
	_ = os.Chdir(tempDir)
	defer os.Chdir(origWd)

	var stdout, stderr bytes.Buffer
	runCmd := func(args ...string) (string, string, int) {
		stdout.Reset()
		stderr.Reset()
		code := RunWithContext(&Context{
			Args:   args,
			Stdout: &stdout,
			Stderr: &stderr,
		})
		return stdout.String(), stderr.String(), code
	}

	// 1. 初始化
	_, _, code := runCmd("init", "-b", "main")
	if code != 0 {
		t.Fatalf("init 失败: %s", stderr.String())
	}

	// 2. 写入文件并提交
	_ = os.WriteFile("file1.go", []byte("line1\nline2\n"), 0644)
	runCmd("add", "file1.go")
	_, _, code = runCmd("commit", "-m", "commit 1")
	if code != 0 {
		t.Fatalf("commit 失败: %s", stderr.String())
	}

	// 3. 测试 branch 与 checkout -b
	runCmd("branch", "feature")
	out, _, _ := runCmd("branch")
	if !strings.Contains(out, "* main") || !strings.Contains(out, "feature") {
		t.Fatalf("branch 列表输出异常: %s", out)
	}

	runCmd("checkout", "feature")
	_ = os.WriteFile("file2.go", []byte("feature file\n"), 0644)
	runCmd("add", "file2.go")
	runCmd("commit", "-m", "feature commit")

	// 4. 切回 main 并验证状态与 diff
	runCmd("switch", "main")
	if _, err := os.Stat("file2.go"); !os.IsNotExist(err) {
		t.Fatalf("切换到 main 分支后 file2.go 不应存在")
	}

	// 5. 修改 file1.go 并验证 status 与 diff
	_ = os.WriteFile("file1.go", []byte("line1\nline2\nline3 added\n"), 0644)
	out, _, _ = runCmd("status", "--porcelain")
	if !strings.Contains(out, " M file1.go") {
		t.Fatalf("status --porcelain 输出不符合预期: %s", out)
	}

	out, _, _ = runCmd("diff")
	if !strings.Contains(out, "+line3 added") {
		t.Fatalf("diff 未能检测到新增行: %s", out)
	}

	// 6. 暂存并验证 diff --cached
	runCmd("add", "file1.go")
	out, _, _ = runCmd("diff", "--cached")
	if !strings.Contains(out, "+line3 added") {
		t.Fatalf("diff --cached 未能检测到暂存行: %s", out)
	}

	// 7. 测试 restore --staged 取消暂存
	runCmd("restore", "--staged", "file1.go")
	out, _, _ = runCmd("status", "--porcelain")
	if !strings.Contains(out, " M file1.go") {
		t.Fatalf("restore --staged 恢复状态不正确: %s", out)
	}

	// 8. 测试 restore 撤销工作区修改
	runCmd("restore", "file1.go")
	out, _, _ = runCmd("status", "--porcelain")
	if strings.TrimSpace(out) != "" {
		t.Fatalf("restore 撤销工作区修改后 status 应为空: %s", out)
	}

	// 9. 使用 git fsck --strict 校验互操作健康度
	gitFsck, err := exec.Command("git", "fsck", "--strict").CombinedOutput()
	if err != nil {
		t.Fatalf("git fsck --strict 失败: %s\n%v", string(gitFsck), err)
	}
}
