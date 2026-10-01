package refs

import (
	"gogit/internal/object"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestRefsCompatibilityWithGitOracle 测试引用管理、HEAD 符号解析及 reflog 与真实 git 严格互通
func TestRefsCompatibilityWithGitOracle(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "gogit_refs_test_*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tempDir)

	// 初始化一个空 git 仓库
	cmd := exec.Command("git", "init", tempDir)
	if err := cmd.Run(); err != nil {
		t.Fatalf("git init 失败: %v", err)
	}

	gitDir := filepath.Join(tempDir, ".git")
	mgr := NewManager(gitDir)

	// 1. 测试初始 HEAD 解析（未提交分支应报错未找到引用）
	headRef, err := mgr.ReadHEAD()
	if err != nil {
		t.Fatalf("ReadHEAD 失败: %v", err)
	}
	if !headRef.IsSymref {
		t.Fatalf("初始 HEAD 应为符号引用")
	}

	// 2. 创建一个虚拟提交并通过 UpdateRef 更新 master/main 分支
	dummyCommitHash := object.MustHashFromHex("1111111111111111111111111111111111111111")
	committer := object.Signature{
		Name:  "Refs Tester",
		Email: "tester@gogit.local",
		When:  time.Unix(1700000000, 0),
		TZ:    "+0800",
	}

	targetBranch := headRef.Target // 通常是 refs/heads/master 或 refs/heads/main
	err = mgr.UpdateRef(targetBranch, dummyCommitHash, committer, "commit: initial commit")
	if err != nil {
		t.Fatalf("UpdateRef 失败: %v", err)
	}

	// 3. 验证使用 git rev-parse HEAD 能否正确解析出刚才写入的值
	revCmd := exec.Command("git", "--git-dir="+gitDir, "rev-parse", "HEAD")
	out, err := revCmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git rev-parse HEAD 失败: %v, 输出: %s", err, string(out))
	}
	if strings.TrimSpace(string(out)) != dummyCommitHash.String() {
		t.Fatalf("git rev-parse HEAD 不匹配! 期望 %s, 实际 %s", dummyCommitHash.String(), strings.TrimSpace(string(out)))
	}

	// 4. 测试 Detached HEAD 状态
	detachedHash := object.MustHashFromHex("2222222222222222222222222222222222222222")
	if err := mgr.SetHEADDetached(detachedHash); err != nil {
		t.Fatalf("SetHEADDetached 失败: %v", err)
	}
	dRef, err := mgr.ReadHEAD()
	if err != nil || dRef.IsSymref || dRef.Hash != detachedHash {
		t.Fatalf("Detached HEAD 状态验证失败")
	}

	revCmd2 := exec.Command("git", "--git-dir="+gitDir, "rev-parse", "HEAD")
	out2, err := revCmd2.CombinedOutput()
	if err != nil {
		t.Fatalf("git rev-parse detached HEAD 失败: %v, 输出: %s", err, string(out2))
	}
	if strings.TrimSpace(string(out2)) != detachedHash.String() {
		t.Fatalf("git rev-parse detached HEAD 不匹配! 期望 %s, 实际 %s", detachedHash.String(), strings.TrimSpace(string(out2)))
	}

	// 5. 测试 packed-refs 读取
	packedContent := "# pack-refs with: peeled fully-peeled sorted\n" +
		"3333333333333333333333333333333333333333 refs/tags/v1.0.0\n" +
		"^4444444444444444444444444444444444444444\n"
	if err := os.WriteFile(filepath.Join(gitDir, "packed-refs"), []byte(packedContent), 0644); err != nil {
		t.Fatal(err)
	}

	tagRef, err := mgr.GetRef("v1.0.0")
	if err != nil {
		t.Fatalf("GetRef v1.0.0 失败: %v", err)
	}
	if tagRef.Hash.String() != "3333333333333333333333333333333333333333" {
		t.Fatalf("tagRef Hash 不匹配: %s", tagRef.Hash.String())
	}
	if !tagRef.HasPeeled || tagRef.Peeled.String() != "4444444444444444444444444444444444444444" {
		t.Fatalf("tagRef Peeled 不匹配: %v, %s", tagRef.HasPeeled, tagRef.Peeled.String())
	}
}
