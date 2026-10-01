package repo

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestRepoInitCompatibilityWithGitFsck 测试 gogit 初始化仓库后，真实 git fsck --strict 零错误通过
func TestRepoInitCompatibilityWithGitFsck(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "gogit_repo_init_*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tempDir)

	repoDir := filepath.Join(tempDir, "myrepo")
	repo, err := InitRepository(repoDir, false, "main")
	if err != nil {
		t.Fatalf("InitRepository 失败: %v", err)
	}

	if repo.IsBare {
		t.Fatalf("期望非 bare 仓库")
	}

	// 运行 git fsck --strict
	fsckCmd := exec.Command("git", "--git-dir="+repo.GitDir, "fsck", "--strict")
	out, err := fsckCmd.CombinedOutput()
	if err != nil {
		t.Fatalf("真实 git fsck --strict 失败: %v, 输出: %s", err, string(out))
	}

	// 运行 git status 验证初始状态
	statusCmd := exec.Command("git", "--git-dir="+repo.GitDir, "--work-tree="+repo.WorkTree, "status")
	statusOut, err := statusCmd.CombinedOutput()
	if err != nil {
		t.Fatalf("真实 git status 失败: %v, 输出: %s", err, string(statusOut))
	}

	if !strings.Contains(string(statusOut), "On branch main") && !strings.Contains(string(statusOut), "No commits yet") {
		t.Fatalf("git status 未显示预期 main 分支无提交状态: %s", string(statusOut))
	}
}

// TestBareRepoInit 测试 bare 仓库初始化
func TestBareRepoInit(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "gogit_repo_bare_*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tempDir)

	repoDir := filepath.Join(tempDir, "mybare.git")
	repo, err := InitRepository(repoDir, true, "master")
	if err != nil {
		t.Fatalf("InitRepository bare 失败: %v", err)
	}

	if !repo.IsBare {
		t.Fatalf("期望 bare 仓库")
	}

	fsckCmd := exec.Command("git", "--git-dir="+repo.GitDir, "fsck", "--strict")
	out, err := fsckCmd.CombinedOutput()
	if err != nil {
		t.Fatalf("真实 git fsck bare 失败: %v, 输出: %s", err, string(out))
	}
}
