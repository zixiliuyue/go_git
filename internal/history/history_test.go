package history

import (
	"gogit/internal/index"
	"gogit/internal/merge"
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
	dir, err := os.MkdirTemp("", "gogit-history-test-*")
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

func TestCherryPickAndRevert(t *testing.T) {
	r, dir := setupTestRepo(t)
	defer os.RemoveAll(dir)

	// 提交 1: c1
	commitFile(t, r, "file1.txt", "line 1\n", "initial commit")

	// 创建 feature 分支并提交 c2
	headOID, _ := r.Refs.ResolveHEAD()
	_ = r.Refs.UpdateRef("refs/heads/feature", headOID, r.CommitterSignature(), "branch: create")
	_ = r.Refs.SetHEADSymbolic("refs/heads/feature")

	c2 := commitFile(t, r, "file2.txt", "feature content\n", "feature commit")

	// 切回 main
	_ = worktree.CheckoutSwitch(r, "main", worktree.CheckoutOptions{})

	// 在 main 上 cherry-pick c2
	res, err := CherryPick(r, c2)
	if err != nil {
		t.Fatalf("CherryPick 失败: %v", err)
	}
	if res.Status != merge.MergeClean {
		t.Fatalf("CherryPick 状态非 Clean: %v", res.Status)
	}

	// 检查 file2.txt 是否在 main 检出
	data, err := os.ReadFile(filepath.Join(r.WorkTree, "file2.txt"))
	if err != nil || string(data) != "feature content\n" {
		t.Fatalf("CherryPick 未正确产生文件: %s", string(data))
	}

	// 在 main 上 revert 刚产生的 cherry-pick 提交
	headAfterPick, _ := r.Refs.ResolveHEAD()
	revRes, err := Revert(r, headAfterPick)
	if err != nil {
		t.Fatalf("Revert 失败: %v", err)
	}
	if revRes.Status != merge.MergeClean {
		t.Fatalf("Revert 状态非 Clean: %v", revRes.Status)
	}

	// file2.txt 应当在工作区中被删除
	if _, err := os.Stat(filepath.Join(r.WorkTree, "file2.txt")); !os.IsNotExist(err) {
		t.Fatalf("Revert 未能删除 file2.txt")
	}
}

func TestStash(t *testing.T) {
	r, dir := setupTestRepo(t)
	defer os.RemoveAll(dir)

	commitFile(t, r, "test.txt", "v1\n", "c1")

	// 在工作区修改 test.txt
	_ = os.WriteFile(filepath.Join(r.WorkTree, "test.txt"), []byte("v1 modified\n"), 0644)

	// Stash push
	_, err := StashPush(r, "my stash", false)
	if err != nil {
		t.Fatalf("StashPush 失败: %v", err)
	}

	// 验证工作区已还原为 v1
	data, _ := os.ReadFile(filepath.Join(r.WorkTree, "test.txt"))
	if string(data) != "v1\n" {
		t.Fatalf("StashPush 未能还原工作区: %s", string(data))
	}

	// Stash list
	entries, err := StashList(r)
	if err != nil || len(entries) != 1 {
		t.Fatalf("StashList 期望 1 条记录，实际 %d 条, err: %v", len(entries), err)
	}
	if !strings.Contains(entries[0].Message, "my stash") {
		t.Fatalf("Stash 记录信息不匹配: %s", entries[0].Message)
	}

	// Stash pop
	popRes, err := StashPop(r, "")
	if err != nil {
		t.Fatalf("StashPop 失败: %v", err)
	}
	if popRes.Status != merge.MergeClean {
		t.Fatalf("StashPop 期望 Clean，实际: %v", popRes.Status)
	}

	// 验证修改已恢复
	dataAfterPop, _ := os.ReadFile(filepath.Join(r.WorkTree, "test.txt"))
	if string(dataAfterPop) != "v1 modified\n" {
		t.Fatalf("StashPop 未能恢复工作区修改: %s", string(dataAfterPop))
	}
}

func TestTagsAndDescribe(t *testing.T) {
	r, dir := setupTestRepo(t)
	defer os.RemoveAll(dir)

	c1 := commitFile(t, r, "a.txt", "v1\n", "first commit")

	// 创建轻量标签
	_, err := CreateTag(r, "v1.0", TagOptions{})
	if err != nil {
		t.Fatalf("CreateTag 轻量标签失败: %v", err)
	}

	// 创建附注标签
	c2 := commitFile(t, r, "a.txt", "v2\n", "second commit")
	_, err = CreateTag(r, "v2.0", TagOptions{
		Annotated: true,
		Message:   "Release 2.0",
		Target:    c2.String(),
	})
	if err != nil {
		t.Fatalf("CreateTag 附注标签失败: %v", err)
	}

	// 列出标签
	tags, err := ListTags(r)
	if err != nil || len(tags) != 2 || tags[0] != "v1.0" || tags[1] != "v2.0" {
		t.Fatalf("ListTags 输出异常: %v", tags)
	}

	// 剥离附注标签
	tagRefOID, _ := r.Refs.ResolveRef("refs/tags/v2.0")
	peeled, err := PeelTag(r, tagRefOID)
	if err != nil || peeled != c2 {
		t.Fatalf("PeelTag 剥离失败: %s != %s", peeled.String(), c2.String())
	}

	// Describe 提交 c2 应当直接返回 v2.0
	desc, err := Describe(r, c2.String(), DescribeOptions{})
	if err != nil || desc != "v2.0" {
		t.Fatalf("Describe c2 失败: %s, err: %v", desc, err)
	}

	// 追加一次提交 c3，Describe 应当返回 v2.0-1-g<shortHash>
	c3 := commitFile(t, r, "a.txt", "v3\n", "third commit")
	desc3, err := Describe(r, c3.String(), DescribeOptions{})
	if err != nil || !strings.HasPrefix(desc3, "v2.0-1-g") {
		t.Fatalf("Describe c3 失败: %s, err: %v", desc3, err)
	}
	_ = c1
}

func TestBlame(t *testing.T) {
	r, dir := setupTestRepo(t)
	defer os.RemoveAll(dir)

	c1 := commitFile(t, r, "code.txt", "line1\nline2\n", "commit 1")
	c2 := commitFile(t, r, "code.txt", "line1\nline2 modified\nline3 added\n", "commit 2")

	lines, err := Blame(r, "code.txt", "HEAD", 1, 3)
	if err != nil {
		t.Fatalf("Blame 失败: %v", err)
	}

	if len(lines) != 3 {
		t.Fatalf("Blame 期望 3 行，实际 %d 行", len(lines))
	}

	// line 1 归属于 c1
	if lines[0].CommitOID != c1 {
		t.Fatalf("Line 1 期望归属 c1 (%s)，实际 %s", c1.String(), lines[0].CommitOID.String())
	}

	// line 2 归属于 c2
	if lines[1].CommitOID != c2 {
		t.Fatalf("Line 2 期望归属 c2 (%s)，实际 %s", c2.String(), lines[1].CommitOID.String())
	}

	// line 3 归属于 c2
	if lines[2].CommitOID != c2 {
		t.Fatalf("Line 3 期望归属 c2 (%s)，实际 %s", c2.String(), lines[2].CommitOID.String())
	}
}

func TestBisect(t *testing.T) {
	r, dir := setupTestRepo(t)
	defer os.RemoveAll(dir)

	cGood := commitFile(t, r, "f.txt", "good\n", "good commit")
	commitFile(t, r, "f.txt", "good 2\n", "middle commit 1")
	cBad1 := commitFile(t, r, "f.txt", "bug introduced\n", "bad commit introduced")
	commitFile(t, r, "f.txt", "bug 2\n", "middle commit 2")
	cBadLatest := commitFile(t, r, "f.txt", "bug latest\n", "latest bad commit")

	// 初始化 bisect
	err := BisectStart(r, cBadLatest.String(), cGood.String())
	if err != nil {
		t.Fatalf("BisectStart 失败: %v", err)
	}

	// 运行 bisect 检查逻辑：以内容是否包含 good 为基线通过判定（exit 0 为 good，非 0 为 bad）
	res, err := BisectRun(r, "grep", []string{"-q", "good", "f.txt"})
	if err != nil {
		t.Fatalf("BisectRun 失败: %v", err)
	}

	if !res.Finished {
		t.Fatalf("BisectRun 未能完成定位")
	}

	if res.BadCommit != cBad1 {
		t.Fatalf("Bisect 首个坏提交定位不匹配: %s != %s", res.BadCommit.String(), cBad1.String())
	}

	// 重置 bisect
	_ = BisectReset(r, "main")
	if _, err := os.Stat(filepath.Join(r.GitDir, "BISECT_START")); !os.IsNotExist(err) {
		t.Fatalf("BisectReset 未清理 BISECT_START")
	}
}

func TestNotes(t *testing.T) {
	r, dir := setupTestRepo(t)
	defer os.RemoveAll(dir)

	c := commitFile(t, r, "f.txt", "v1\n", "c1")

	// 添加 note
	err := AddNote(r, c.String(), "This is an important note")
	if err != nil {
		t.Fatalf("AddNote 失败: %v", err)
	}

	// 查看 note
	noteText, err := ShowNote(r, c.String())
	if err != nil {
		t.Fatalf("ShowNote 失败: %v", err)
	}

	if strings.TrimSpace(noteText) != "This is an important note" {
		t.Fatalf("Note 内容不匹配: %s", noteText)
	}
}
