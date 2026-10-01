package index

import (
	"gogit/internal/object"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestIndexCompatibilityWithGitOracle 测试 gogit 写入的 index 文件能被真实 git 准确读取
func TestIndexCompatibilityWithGitOracle(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "gogit_index_test_*")
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
	objectsDir := filepath.Join(gitDir, "objects")
	indexPath := filepath.Join(gitDir, "index")

	// 创建几个工作区文件
	file1Path := filepath.Join(tempDir, "hello.txt")
	file2Path := filepath.Join(tempDir, "src", "main.go")
	if err := os.MkdirAll(filepath.Dir(file2Path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(file1Path, []byte("Hello gogit index!\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(file2Path, []byte("package main\n"), 0755); err != nil {
		t.Fatal(err)
	}

	// 用 gogit 写入 blob 对象并构建 index
	idx := NewIndex()

	fi1, err := os.Stat(file1Path)
	if err != nil {
		t.Fatal(err)
	}
	blob1Content, _ := os.ReadFile(file1Path)
	h1, err := gogitWriteBlob(objectsDir, blob1Content)
	if err != nil {
		t.Fatal(err)
	}
	entry1 := EntryFromOSFileInfo("hello.txt", fi1, h1)
	idx.AddOrReplaceEntry(entry1)

	fi2, err := os.Stat(file2Path)
	if err != nil {
		t.Fatal(err)
	}
	blob2Content, _ := os.ReadFile(file2Path)
	h2, err := gogitWriteBlob(objectsDir, blob2Content)
	if err != nil {
		t.Fatal(err)
	}
	entry2 := EntryFromOSFileInfo("src/main.go", fi2, h2)
	idx.AddOrReplaceEntry(entry2)

	// 写入 index 文件
	if err := idx.WriteIndex(indexPath); err != nil {
		t.Fatalf("WriteIndex 失败: %v", err)
	}

	// 用真实 git ls-files -s 检查暂存区内容与权限模式
	lsCmd := exec.Command("git", "--git-dir="+gitDir, "--work-tree="+tempDir, "ls-files", "-s")
	out, err := lsCmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git ls-files 失败: %v, 输出: %s", err, string(out))
	}
	lsOut := string(out)

	if !strings.Contains(lsOut, "hello.txt") || !strings.Contains(lsOut, "src/main.go") {
		t.Fatalf("git ls-files 未包含暂存文件: %s", lsOut)
	}
	if !strings.Contains(lsOut, h1.String()) || !strings.Contains(lsOut, h2.String()) {
		t.Fatalf("git ls-files 哈希不匹配: %s", lsOut)
	}

	// 验证 WriteTree 与 git write-tree 输出完全一致
	gogitTreeHash, err := idx.WriteTree(objectsDir)
	if err != nil {
		t.Fatalf("idx.WriteTree 失败: %v", err)
	}

	wtCmd := exec.Command("git", "--git-dir="+gitDir, "write-tree")
	wtOut, err := wtCmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git write-tree 失败: %v, 输出: %s", err, string(wtOut))
	}
	gitTreeHash := strings.TrimSpace(string(wtOut))

	if gogitTreeHash.String() != gitTreeHash {
		t.Fatalf("WriteTree 哈希与 git write-tree 不一致! gogit=%s, git=%s", gogitTreeHash.String(), gitTreeHash)
	}

	// 用真实 git fsck --strict 校验整个仓库结构健康状态
	fsckCmd := exec.Command("git", "--git-dir="+gitDir, "fsck", "--strict")
	fsckOut, err := fsckCmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git fsck 校验失败: %v, 输出: %s", err, string(fsckOut))
	}
}

// 辅助函数：向 objects 写入 blob
func gogitWriteBlob(objectsDir string, content []byte) (gogitHash, error) {
	return object.WriteLooseObjectToDir(objectsDir, object.TypeBlob, content)
}

type gogitHash = object.Hash
