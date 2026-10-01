package worktree

import (
	"gogit/internal/repo"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestIgnoreRulePatterns 测试各类 gitignore 通配符、取反与目录匹配规则
func TestIgnoreRulePatterns(t *testing.T) {
	ignorer := NewIgnoreMatcher()
	rules := `
# 忽略所有 .log 文件
*.log
# 但不忽略 important.log
!important.log
# 仅忽略 build 目录
build/
# 双星匹配
temp/**/cache.dat
`
	ignorer.ParseRules([]byte(rules), "")

	tests := []struct {
		path     string
		isDir    bool
		expected bool
	}{
		{"test.log", false, true},
		{"sub/test.log", false, true},
		{"important.log", false, false},
		{"build", true, true},
		{"build", false, false},
		{"temp/a/b/cache.dat", false, true},
		{"other.go", false, false},
	}

	for _, tc := range tests {
		actual := ignorer.Match(tc.path, tc.isDir)
		if actual != tc.expected {
			t.Errorf("Match(%q, %v) = %v, 期望 %v", tc.path, tc.isDir, actual, tc.expected)
		}
	}
}

// TestStatusPorcelainVsGitOracle 测试 gogit 计算的 status --porcelain 与原生 git status --porcelain 逐行一致
func TestStatusPorcelainVsGitOracle(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "gogit_status_test_*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tempDir)

	// 使用原生 git 初始化并配置用户
	_ = exec.Command("git", "init", "-b", "main", tempDir).Run()
	_ = exec.Command("git", "-C", tempDir, "config", "user.name", "tester").Run()
	_ = exec.Command("git", "-C", tempDir, "config", "user.email", "test@test.com").Run()

	// 1. 创建基础提交
	f1 := filepath.Join(tempDir, "tracked.go")
	_ = os.WriteFile(f1, []byte("line1\nline2\n"), 0644)
	_ = exec.Command("git", "-C", tempDir, "add", "tracked.go").Run()
	_ = exec.Command("git", "-C", tempDir, "commit", "-m", "initial").Run()

	// 2. 制造各类变动：
	// - tracked.go 在工作区被修改（Unstaged ' M'）
	_ = os.WriteFile(f1, []byte("line1 modified\nline2\n"), 0644)
	// - staged.go 暂存新文件（Staged 'A '）
	f2 := filepath.Join(tempDir, "staged.go")
	_ = os.WriteFile(f2, []byte("staged file\n"), 0644)
	_ = exec.Command("git", "-C", tempDir, "add", "staged.go").Run()
	// - untracked.go 未跟踪文件（'??'）
	f3 := filepath.Join(tempDir, "untracked.go")
	_ = os.WriteFile(f3, []byte("untracked file\n"), 0644)

	// 运行真实 git status --porcelain
	gitOut, err := exec.Command("git", "-C", tempDir, "status", "--porcelain").CombinedOutput()
	if err != nil {
		t.Fatalf("git status 失败: %v", err)
	}
	expectedOutput := strings.TrimSpace(string(gitOut))

	// 使用 gogit ComputeStatus 计算
	origWd, _ := os.Getwd()
	_ = os.Chdir(tempDir)
	defer os.Chdir(origWd)

	repoObj, err := repo.FindRepository(tempDir)
	if err != nil {
		t.Fatalf("FindRepository 失败: %v", err)
	}

	st, err := ComputeStatus(repoObj)
	if err != nil {
		t.Fatalf("ComputeStatus 失败: %v", err)
	}
	actualOutput := strings.TrimSpace(st.FormatPorcelain())

	if actualOutput != expectedOutput {
		t.Fatalf("Status 输出不一致!\ngogit:\n%s\ngit:\n%s", actualOutput, expectedOutput)
	}
}
