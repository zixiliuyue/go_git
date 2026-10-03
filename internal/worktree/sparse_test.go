package worktree

import (
	"gogit/internal/index"
	"gogit/internal/object"
	"gogit/internal/repo"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestSparseIndexCollapseAndNativeGitOracle(t *testing.T) {
	tmpDir := t.TempDir()

	r, err := repo.InitRepository(tmpDir, false, "main")
	if err != nil {
		t.Fatalf("InitRepository failed: %v", err)
	}

	// 1. 创建文件并提交：root.txt, folder1/a.txt, folder2/sub/b.txt
	_ = os.WriteFile(filepath.Join(tmpDir, "root.txt"), []byte("root content"), 0644)
	_ = os.MkdirAll(filepath.Join(tmpDir, "folder1"), 0755)
	_ = os.WriteFile(filepath.Join(tmpDir, "folder1", "a.txt"), []byte("a content"), 0644)
	_ = os.MkdirAll(filepath.Join(tmpDir, "folder2", "sub"), 0755)
	_ = os.WriteFile(filepath.Join(tmpDir, "folder2", "sub", "b.txt"), []byte("b content"), 0644)

	// 构造初始索引
	idx := index.NewIndex()
	for _, p := range []string{"folder1/a.txt", "folder2/sub/b.txt", "root.txt"} {
		data, _ := os.ReadFile(filepath.Join(tmpDir, p))
		h, _ := r.WriteBlob(data)
		fi, _ := os.Stat(filepath.Join(tmpDir, p))
		entry := index.EntryFromOSFileInfo(p, fi, h)
		idx.AddOrReplaceEntry(entry)
	}
	_ = idx.WriteIndex(r.IndexPath)

	// 生成初始 commit
	treeHash, err := idx.WriteTree(r.ObjectsDir)
	if err != nil {
		t.Fatalf("WriteTree failed: %v", err)
	}
	sig := object.Signature{Name: "Tester", Email: "tester@google.com"}
	commit := &object.Commit{
		Tree:      treeHash,
		Author:    sig,
		Committer: sig,
		Message:   "initial commit\n",
	}
	cHash, err := r.WriteCommit(commit)
	if err != nil {
		t.Fatalf("WriteCommit failed: %v", err)
	}
	_ = r.Refs.UpdateRef("refs/heads/main", cHash, sig, "commit")

	// 2. 初始化带 --cone 和 --sparse-index 的稀疏检出
	if err := InitSparseCheckout(r, true, true); err != nil {
		t.Fatalf("InitSparseCheckout failed: %v", err)
	}

	// 3. 设置只检出 folder1
	if err := SetSparseCheckout(r, []string{"folder1"}, true); err != nil {
		t.Fatalf("SetSparseCheckout failed: %v", err)
	}

	// 4. 验证索引结构是否成功折叠 folder2/
	idxAfter, err := index.ReadIndex(r.IndexPath)
	if err != nil {
		t.Fatalf("ReadIndex failed: %v", err)
	}

	foundFolder2 := false
	foundSubB := false
	for _, e := range idxAfter.Entries {
		if e.Path == "folder2/" {
			foundFolder2 = true
			if !e.IsSparseDirectory() {
				t.Errorf("folder2/ expected to be sparse directory, mode is %o", e.Mode)
			}
			if !e.IsSkipWorktree() {
				t.Errorf("folder2/ expected skip-worktree flag")
			}
		}
		if e.Path == "folder2/sub/b.txt" {
			foundSubB = true
		}
	}

	if !foundFolder2 {
		t.Fatalf("expected folder2/ sparse directory entry in index, got entries: %+v", idxAfter.Entries)
	}
	if foundSubB {
		t.Fatalf("folder2/sub/b.txt should be collapsed into folder2/")
	}

	// 5. 验证磁盘文件状态：folder1/a.txt 存在，folder2/sub/b.txt 应该不存在
	if _, err := os.Stat(filepath.Join(tmpDir, "folder1", "a.txt")); err != nil {
		t.Errorf("folder1/a.txt should exist on disk")
	}
	if _, err := os.Stat(filepath.Join(tmpDir, "folder2", "sub", "b.txt")); !os.IsNotExist(err) {
		t.Errorf("folder2/sub/b.txt should be hidden from worktree")
	}

	// 6. 验证原生 Git 是否能无缝识别该稀疏索引 (git ls-files --sparse --stage)
	gitCmd := exec.Command("git", "ls-files", "--sparse", "--stage")
	gitCmd.Dir = tmpDir
	gitCmd.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null")
	output, err := gitCmd.CombinedOutput()
	if err != nil {
		t.Fatalf("native git ls-files failed: %v\nOutput: %s", err, string(output))
	}

	outStr := string(output)
	if !strings.Contains(outStr, "040000") || !strings.Contains(outStr, "folder2/") {
		t.Fatalf("native git ls-files does not show sparse directory entry 040000 folder2/:\n%s", outStr)
	}

	// 7. 验证 WriteTree 在稀疏索引下依然能生成与原树完全一致的根树哈希
	newTreeHash, err := idxAfter.WriteTree(r.ObjectsDir)
	if err != nil {
		t.Fatalf("WriteTree on sparse index failed: %v", err)
	}
	if newTreeHash != treeHash {
		t.Fatalf("sparse index WriteTree mismatch: expected %s, got %s", treeHash, newTreeHash)
	}

	// 8. 验证 ExpandSparseDirectory 展开
	if err := ExpandSparseIndex(r, idxAfter); err != nil {
		t.Fatalf("ExpandSparseIndex failed: %v", err)
	}
	foundSubBExpanded := false
	for _, e := range idxAfter.Entries {
		if e.Path == "folder2/sub/b.txt" {
			foundSubBExpanded = true
		}
	}
	if !foundSubBExpanded {
		t.Fatalf("expected folder2/sub/b.txt after ExpandSparseIndex")
	}
}
