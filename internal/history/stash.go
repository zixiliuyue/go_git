package history

import (
	"bufio"
	"fmt"
	"gogit/internal/diff"
	"gogit/internal/index"
	"gogit/internal/merge"
	"gogit/internal/object"
	"gogit/internal/repo"
	"gogit/internal/rev"
	"gogit/internal/worktree"
	"os"
	"path/filepath"
	"strings"
)

type StashEntry struct {
	Index   int
	Hash    object.Hash
	Message string
}

// StashPush 保存当前工作区与暂存区的修改到 stash
func StashPush(r *repo.Repository, msg string, includeUntracked bool) (object.Hash, error) {
	st, err := worktree.ComputeStatus(r)
	if err != nil {
		return object.ZeroHash, err
	}

	hasChanges := false
	for _, item := range st.Items {
		if item.Staged != ' ' || item.Unstaged != ' ' || (includeUntracked && item.IsUntracked) {
			hasChanges = true
			break
		}
	}

	if !hasChanges {
		return object.ZeroHash, fmt.Errorf("No local changes to save")
	}

	headOID, err := r.Refs.ResolveHEAD()
	if err != nil {
		return object.ZeroHash, err
	}
	headCommit, err := r.ReadCommit(headOID)
	if err != nil {
		return object.ZeroHash, err
	}

	committer := r.CommitterSignature()
	shortHead := headOID.String()[:7]
	headSubject := strings.TrimSpace(strings.Split(headCommit.Message, "\n")[0])
	branch := st.BranchName

	// 1. 创建 Index commit
	idx, err := r.GetIndex()
	if err != nil {
		return object.ZeroHash, err
	}
	indexTreeOID, err := r.WriteTreeFromIndex(idx)
	if err != nil {
		return object.ZeroHash, err
	}

	indexCommit := &object.Commit{
		Tree:      indexTreeOID,
		Parents:   []object.Hash{headOID},
		Author:    committer,
		Committer: committer,
		Message:   fmt.Sprintf("index on %s: %s %s\n", branch, shortHead, headSubject),
	}
	indexCommitOID, err := r.WriteCommit(indexCommit)
	if err != nil {
		return object.ZeroHash, err
	}

	// 2. 将当前工作区全盘暂存构建临时 Tree
	worktreeIdx := index.NewIndex()
	for _, e := range idx.Entries {
		worktreeIdx.AddOrReplaceEntry(e)
	}

	for _, item := range st.Items {
		if item.IsUntracked && !includeUntracked {
			continue
		}
		absPath := filepath.Join(r.WorkTree, item.Path)
		fi, err := os.Lstat(absPath)
		if err != nil {
			if os.IsNotExist(err) {
				worktreeIdx.RemoveEntry(item.Path)
			}
			continue
		}

		var data []byte
		if fi.Mode()&os.ModeSymlink != 0 {
			target, _ := os.Readlink(absPath)
			data = []byte(target)
		} else {
			data, _ = os.ReadFile(absPath)
		}

		blobOID, err := r.WriteBlob(data)
		if err != nil {
			return object.ZeroHash, err
		}
		entry := index.EntryFromOSFileInfo(item.Path, fi, blobOID)
		worktreeIdx.AddOrReplaceEntry(entry)
	}

	worktreeTreeOID, err := r.WriteTreeFromIndex(worktreeIdx)
	if err != nil {
		return object.ZeroHash, err
	}

	// 3. 构建 Stash 提交
	stashSubject := msg
	if stashSubject == "" {
		stashSubject = fmt.Sprintf("WIP on %s: %s %s", branch, shortHead, headSubject)
	}

	stashCommit := &object.Commit{
		Tree:      worktreeTreeOID,
		Parents:   []object.Hash{headOID, indexCommitOID},
		Author:    committer,
		Committer: committer,
		Message:   stashSubject + "\n",
	}

	stashOID, err := r.WriteCommit(stashCommit)
	if err != nil {
		return object.ZeroHash, err
	}

	// 4. 更新 refs/stash（UpdateRef 内部会自动记入 reflog）
	_ = r.Refs.UpdateRef("refs/stash", stashOID, committer, stashSubject)

	// 5. 将工作区与索引还原为 HEAD
	_ = worktree.CheckoutSwitch(r, headOID.String(), worktree.CheckoutOptions{})

	if includeUntracked {
		for _, item := range st.Items {
			if item.IsUntracked {
				_ = os.RemoveAll(filepath.Join(r.WorkTree, item.Path))
			}
		}
	}

	return stashOID, nil
}

// StashList 列出所有储藏记录
func StashList(r *repo.Repository) ([]StashEntry, error) {
	reflogFile := filepath.Join(r.GitDir, "logs", "refs", "stash")
	f, err := os.Open(reflogFile)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	defer f.Close()

	var entries []StashEntry
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Text()
		parts := strings.SplitN(line, "\t", 2)
		if len(parts) < 2 {
			continue
		}
		metaParts := strings.Fields(parts[0])
		if len(metaParts) < 2 {
			continue
		}
		newHash, err := object.NewHashFromHex(metaParts[1])
		if err != nil {
			continue
		}
		entries = append(entries, StashEntry{
			Hash:    newHash,
			Message: parts[1],
		})
	}

	// 倒序排列（最新的在最前）
	n := len(entries)
	res := make([]StashEntry, n)
	for i := 0; i < n; i++ {
		res[i] = entries[n-1-i]
		res[i].Index = i
	}

	return res, nil
}

// StashApply 应用指定储藏（默认 stash@{0}）
func StashApply(r *repo.Repository, stashSpec string) (*merge.MergeResult, error) {
	if stashSpec == "" {
		stashSpec = "refs/stash"
	}
	stashOID, err := rev.ParseRevision(r, stashSpec)
	if err != nil {
		return nil, fmt.Errorf("无法解析储藏引用 %s: %w", stashSpec, err)
	}

	stashCommit, err := r.ReadCommit(stashOID)
	if err != nil {
		return nil, err
	}

	if len(stashCommit.Parents) == 0 {
		return nil, fmt.Errorf("非法 stash 提交")
	}

	headOID, err := r.Refs.ResolveHEAD()
	if err != nil {
		return nil, err
	}
	headCommit, err := r.ReadCommit(headOID)
	if err != nil {
		return nil, err
	}

	baseCommit, err := r.ReadCommit(stashCommit.Parents[0])
	if err != nil {
		return nil, err
	}

	// 3-way merge: base = baseCommit.Tree, ours = HEAD, theirs = stashCommit.Tree
	treeRes, err := merge.MergeTrees(r, baseCommit.Tree, headCommit.Tree, stashCommit.Tree, "HEAD", stashSpec)
	if err != nil {
		return nil, err
	}

	if err := r.SaveIndex(treeRes.NewIndex); err != nil {
		return nil, err
	}

	if len(treeRes.Conflicts) > 0 {
		return &merge.MergeResult{
			Status:    merge.MergeConflictStatus,
			Conflicts: treeRes.Conflicts,
			Message:   "Applied stash with conflicts",
		}, nil
	}

	return &merge.MergeResult{
		Status:  merge.MergeClean,
		Message: "Applied stash successfully",
	}, nil
}

// StashPop 应用并弹出指定储藏
func StashPop(r *repo.Repository, stashSpec string) (*merge.MergeResult, error) {
	res, err := StashApply(r, stashSpec)
	if err != nil {
		return nil, err
	}

	if res.Status == merge.MergeClean {
		_ = StashDrop(r, stashSpec)
	}

	return res, nil
}

// StashDrop 删除指定储藏
func StashDrop(r *repo.Repository, stashSpec string) error {
	reflogFile := filepath.Join(r.GitDir, "logs", "refs", "stash")
	data, err := os.ReadFile(reflogFile)
	if err != nil {
		return err
	}

	lines := strings.Split(strings.TrimRight(string(data), "\n"), "\n")
	if len(lines) == 0 {
		return fmt.Errorf("No stash entries found")
	}

	// 默认删除最新的一条（即最后一行）
	dropIdx := len(lines) - 1
	if strings.Contains(stashSpec, "@{") {
		// 解析指定序号
		start := strings.Index(stashSpec, "@{")
		end := strings.Index(stashSpec, "}")
		if start != -1 && end != -1 {
			var n int
			_, _ = fmt.Sscanf(stashSpec[start+2:end], "%d", &n)
			dropIdx = len(lines) - 1 - n
		}
	}

	if dropIdx < 0 || dropIdx >= len(lines) {
		return fmt.Errorf("Invalid stash index")
	}

	lines = append(lines[:dropIdx], lines[dropIdx+1:]...)

	if len(lines) == 0 {
		_ = os.Remove(reflogFile)
		_ = os.Remove(filepath.Join(r.GitDir, "refs", "stash"))
	} else {
		newContent := strings.Join(lines, "\n") + "\n"
		_ = os.WriteFile(reflogFile, []byte(newContent), 0644)
		lastLine := lines[len(lines)-1]
		parts := strings.SplitN(lastLine, "\t", 2)
		if len(parts) >= 1 {
			meta := strings.Fields(parts[0])
			if len(meta) >= 2 {
				_ = os.WriteFile(filepath.Join(r.GitDir, "refs", "stash"), []byte(meta[1]+"\n"), 0644)
			}
		}
	}

	return nil
}

// StashShow 显示储藏与基线提交的 diff
func StashShow(r *repo.Repository, stashSpec string) (string, error) {
	if stashSpec == "" {
		stashSpec = "refs/stash"
	}
	stashOID, err := rev.ParseRevision(r, stashSpec)
	if err != nil {
		return "", err
	}

	stashCommit, err := r.ReadCommit(stashOID)
	if err != nil {
		return "", err
	}

	if len(stashCommit.Parents) == 0 {
		return "", fmt.Errorf("非法 stash 提交")
	}

	baseCommit, err := r.ReadCommit(stashCommit.Parents[0])
	if err != nil {
		return "", err
	}

	dummyIdx := index.NewIndex()
	opts := diff.DiffOptions{Stat: true}
	diffs, err := diff.DiffIndexWithTree(r, dummyIdx, baseCommit.Tree, opts)
	if err != nil {
		return "", err
	}

	return diff.FormatDiff(diffs, opts), nil
}
