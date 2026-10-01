package merge

import (
	"gogit/internal/index"
	"gogit/internal/object"
	"gogit/internal/repo"
	"gogit/internal/worktree"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func setupTestRepo(t *testing.T) (*repo.Repository, string) {
	t.Helper()
	dir, err := os.MkdirTemp("", "gogit-merge-test-*")
	if err != nil {
		t.Fatalf("创建临时目录失败: %v", err)
	}

	r, err := repo.InitRepository(dir, false, "main")
	if err != nil {
		_ = os.RemoveAll(dir)
		t.Fatalf("初始化仓库失败: %v", err)
	}
	return r, dir
}

func commitFile(t *testing.T, r *repo.Repository, filename, content, msg string) object.Hash {
	t.Helper()
	filePath := filepath.Join(r.WorkTree, filename)
	if err := os.MkdirAll(filepath.Dir(filePath), 0755); err != nil {
		t.Fatalf("创建目录失败: %v", err)
	}
	if err := os.WriteFile(filePath, []byte(content), 0644); err != nil {
		t.Fatalf("写入文件失败: %v", err)
	}

	idx, err := r.GetIndex()
	if err != nil {
		t.Fatalf("获取索引失败: %v", err)
	}

	fi, err := os.Stat(filePath)
	if err != nil {
		t.Fatalf("stat 失败: %v", err)
	}

	blobOID, err := r.WriteBlob([]byte(content))
	if err != nil {
		t.Fatalf("写入 blob 失败: %v", err)
	}

	rel, _ := filepath.Rel(r.WorkTree, filePath)
	idx.AddOrReplaceEntry(&index.IndexEntry{
		Path: rel,
		OID:  blobOID,
		Mode: 0100644,
		Size: uint32(fi.Size()),
	})
	if err := r.SaveIndex(idx); err != nil {
		t.Fatalf("保存索引失败: %v", err)
	}

	treeOID, err := r.WriteTreeFromIndex(idx)
	if err != nil {
		t.Fatalf("写入 Tree 失败: %v", err)
	}

	var parents []object.Hash
	headOID, err := r.Refs.ResolveHEAD()
	if err == nil && !headOID.IsZero() {
		parents = append(parents, headOID)
	}

	c := &object.Commit{
		Tree:      treeOID,
		Parents:   parents,
		Author:    r.AuthorSignature(),
		Committer: r.CommitterSignature(),
		Message:   msg + "\n",
	}

	commitOID, err := r.WriteCommit(c)
	if err != nil {
		t.Fatalf("写入 commit 失败: %v", err)
	}

	headRef, _ := r.Refs.ReadHEAD()
	if headRef != nil && headRef.IsSymref {
		_ = r.Refs.UpdateRef(headRef.Target, commitOID, r.CommitterSignature(), msg)
	} else {
		_ = r.Refs.UpdateRef("HEAD", commitOID, r.CommitterSignature(), msg)
	}

	return commitOID
}

func TestDoMerge_FastForward(t *testing.T) {
	r, dir := setupTestRepo(t)
	defer os.RemoveAll(dir)

	// main: 提交 1
	commitFile(t, r, "file.txt", "initial content\n", "initial commit")

	// 创建 feature 分支并提交 2
	headOID, _ := r.Refs.ResolveHEAD()
	_ = r.Refs.UpdateRef("refs/heads/feature", headOID, r.CommitterSignature(), "branch: create")
	_ = r.Refs.SetHEADSymbolic("refs/heads/feature")

	c2 := commitFile(t, r, "file.txt", "initial content\nfeature line\n", "feature commit")

	// 切回 main
	_ = worktree.CheckoutSwitch(r, "main", worktree.CheckoutOptions{})

	// 在 main 上合并 feature -> 应当触发 Fast-forward
	res, err := DoMerge(r, "feature", MergeOptions{})
	if err != nil {
		t.Fatalf("DoMerge 失败: %v", err)
	}

	if res.Status != MergeFastForward {
		t.Fatalf("期望状态 MergeFastForward，实际为 %v", res.Status)
	}
	if res.FastForwardTo != c2 {
		t.Fatalf("FastForward 目标提交不匹配: %s != %s", res.FastForwardTo.String(), c2.String())
	}

	// 验证 main 分支指针已推进到 c2
	mainOID, _ := r.Refs.ResolveRef("refs/heads/main")
	if mainOID != c2 {
		t.Fatalf("main 分支未推进至 c2: %s", mainOID.String())
	}
}

func TestDoMerge_ThreeWayConflict(t *testing.T) {
	r, dir := setupTestRepo(t)
	defer os.RemoveAll(dir)

	// 1. 基线提交
	commitFile(t, r, "app.txt", "common base\n", "base commit")

	// 2. 切到 feature 分支修改
	headOID, _ := r.Refs.ResolveHEAD()
	_ = r.Refs.UpdateRef("refs/heads/feature", headOID, r.CommitterSignature(), "branch: create")
	_ = r.Refs.SetHEADSymbolic("refs/heads/feature")
	commitFile(t, r, "app.txt", "common base\nfeature change\n", "feature branch commit")

	// 3. 切回 main 分支产生冲突修改
	_ = worktree.CheckoutSwitch(r, "main", worktree.CheckoutOptions{})
	commitFile(t, r, "app.txt", "common base\nmain change\n", "main branch commit")

	// 4. 执行合并 -> 应当产生冲突
	res, err := DoMerge(r, "feature", MergeOptions{})
	if err != nil {
		t.Fatalf("DoMerge 失败: %v", err)
	}

	if res.Status != MergeConflictStatus {
		t.Fatalf("期望状态 MergeConflictStatus，实际为 %v", res.Status)
	}

	// 检查工作区内容包含冲突标记
	content, _ := os.ReadFile(filepath.Join(r.WorkTree, "app.txt"))
	sContent := string(content)
	if !strings.Contains(sContent, "<<<<<<< HEAD") || !strings.Contains(sContent, "=======") || !strings.Contains(sContent, ">>>>>>> feature") {
		t.Fatalf("工作区文件未包含标准冲突标记:\n%s", sContent)
	}

	// 检查 MERGE_HEAD 与 MERGE_MSG 是否生成
	if _, err := os.Stat(filepath.Join(r.GitDir, "MERGE_HEAD")); err != nil {
		t.Fatalf("未生成 MERGE_HEAD")
	}
	if _, err := os.Stat(filepath.Join(r.GitDir, "MERGE_MSG")); err != nil {
		t.Fatalf("未生成 MERGE_MSG")
	}
}
