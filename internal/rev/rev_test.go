package rev

import (
	"gogit/internal/object"
	"gogit/internal/repo"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestRevParseCompatibilityWithGit 测试 ParseRevision 解析 HEAD, ~1, 缩写哈希与真实 git rev-parse 一致
func TestRevParseCompatibilityWithGit(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "gogit_rev_test_*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tempDir)

	// 初始化一个空 git 仓库
	cmd := exec.Command("git", "init", tempDir)
	if err := cmd.Run(); err != nil {
		t.Fatalf("git init 失败: %v", err)
	}

	// 提交 3 次
	for i := 1; i <= 3; i++ {
		filePath := filepath.Join(tempDir, "hello.go")
		_ = os.WriteFile(filePath, []byte(strings.Repeat("a", i)), 0644)
		addOut, err := exec.Command("git", "-C", tempDir, "add", "hello.go").CombinedOutput()
		if err != nil {
			t.Fatalf("git add 失败: %v, %s", err, string(addOut))
		}
		commitOut, err := exec.Command("git", "-C", tempDir, "commit", "-m", "commit "+strings.Repeat("c", i)).CombinedOutput()
		if err != nil {
			t.Fatalf("git commit 失败: %v, %s", err, string(commitOut))
		}
	}

	r, err := repo.OpenRepository(filepath.Join(tempDir, ".git"), tempDir)
	if err != nil {
		t.Fatalf("OpenRepository 失败: %v", err)
	}

	testCases := []string{
		"HEAD",
		"HEAD~1",
		"HEAD~2",
		"HEAD^",
		"HEAD^1",
	}

	for _, tc := range testCases {
		gogitHash, err := ParseRevision(r, tc)
		if err != nil {
			t.Fatalf("ParseRevision %s 失败: %v", tc, err)
		}

		gitCmd := exec.Command("git", "-C", tempDir, "rev-parse", tc)
		out, err := gitCmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git rev-parse %s 失败: %v", tc, err)
		}
		gitHash := strings.TrimSpace(string(out))

		if gogitHash.String() != gitHash {
			t.Fatalf("修订版本 %s 解析不一致! gogit=%s, git=%s", tc, gogitHash.String(), gitHash)
		}

		// 测试 7 位缩写哈希能否正确解析
		shortSha := gitHash[:7]
		parsedShort, err := ParseRevision(r, shortSha)
		if err != nil {
			t.Fatalf("ParseRevision 缩写 %s 失败: %v", shortSha, err)
		}
		if parsedShort.String() != gitHash {
			t.Fatalf("缩写哈希解析不一致! 期望 %s, 实际 %s", gitHash, parsedShort.String())
		}
	}
}

// TestRevListTraversal 测试提交链遍历与排除集合
func TestRevListTraversal(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "gogit_revlist_test_*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tempDir)

	r, err := repo.InitRepository(tempDir, false, "main")
	if err != nil {
		t.Fatal(err)
	}

	// 手动创建 3 个提交
	c1 := &object.Commit{
		Tree:    object.MustHashFromHex("1111111111111111111111111111111111111111"),
		Author:  object.Signature{Name: "Tester", Email: "test@example.com", When: time.Unix(1000, 0)},
		Message: "first commit\n",
	}
	h1, _ := r.WriteObject(c1)

	c2 := &object.Commit{
		Tree:    object.MustHashFromHex("2222222222222222222222222222222222222222"),
		Parents: []object.Hash{h1},
		Author:  object.Signature{Name: "Tester", Email: "test@example.com", When: time.Unix(2000, 0)},
		Message: "second commit\n",
	}
	h2, _ := r.WriteObject(c2)

	c3 := &object.Commit{
		Tree:    object.MustHashFromHex("3333333333333333333333333333333333333333"),
		Parents: []object.Hash{h2},
		Author:  object.Signature{Name: "Tester", Email: "test@example.com", When: time.Unix(3000, 0)},
		Message: "third commit\n",
	}
	h3, _ := r.WriteObject(c3)

	// 遍历所有提交
	commits, err := RevList(r, []object.Hash{h3}, nil, RevListOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(commits) != 3 {
		t.Fatalf("提交数应为 3，实际为 %d", len(commits))
	}
	if commits[0].Hash != h3 || commits[1].Hash != h2 || commits[2].Hash != h1 {
		t.Fatalf("遍历时间排序不符合预期")
	}

	// 测试范围排除 (h1..h3)
	commitsRange, err := RevList(r, []object.Hash{h3}, []object.Hash{h1}, RevListOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(commitsRange) != 2 {
		t.Fatalf("范围排除后应剩余 2 个提交，实际为 %d", len(commitsRange))
	}
}
