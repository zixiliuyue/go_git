package history

import (
	"fmt"
	"gogit/internal/merge"
	"gogit/internal/object"
	"gogit/internal/repo"
	"os"
	"path/filepath"
	"strings"
)

// CherryPick 重放指定提交到当前 HEAD
func CherryPick(r *repo.Repository, commitOID object.Hash) (*merge.MergeResult, error) {
	c, err := r.ReadCommit(commitOID)
	if err != nil {
		return nil, fmt.Errorf("读取提交 %s 失败: %w", commitOID.String(), err)
	}

	if len(c.Parents) == 0 {
		return nil, fmt.Errorf("不能 cherry-pick 根提交")
	}

	parentCommit, err := r.ReadCommit(c.Parents[0])
	if err != nil {
		return nil, fmt.Errorf("读取父提交失败: %w", err)
	}

	headOID, err := r.Refs.ResolveHEAD()
	if err != nil {
		return nil, fmt.Errorf("无法解析 HEAD: %w", err)
	}
	headCommit, err := r.ReadCommit(headOID)
	if err != nil {
		return nil, err
	}

	// 3-way merge: base = parent, ours = HEAD, theirs = target commit
	treeRes, err := merge.MergeTrees(r, parentCommit.Tree, headCommit.Tree, c.Tree, "HEAD", commitOID.String()[:7])
	if err != nil {
		return nil, err
	}

	if err := r.SaveIndex(treeRes.NewIndex); err != nil {
		return nil, err
	}

	cherryHeadFile := filepath.Join(r.GitDir, "CHERRY_PICK_HEAD")
	mergeMsgFile := filepath.Join(r.GitDir, "MERGE_MSG")

	if len(treeRes.Conflicts) > 0 {
		_ = os.WriteFile(cherryHeadFile, []byte(commitOID.String()+"\n"), 0644)
		_ = os.WriteFile(mergeMsgFile, []byte(c.Message), 0644)
		return &merge.MergeResult{
			Status:    merge.MergeConflictStatus,
			Conflicts: treeRes.Conflicts,
			Message:   "could not apply " + commitOID.String()[:7],
		}, nil
	}

	// 自动生成 cherry-pick 提交（继承原作者签名与提交信息）
	newTreeOID, err := r.WriteTreeFromIndex(treeRes.NewIndex)
	if err != nil {
		return nil, err
	}

	newCommit := &object.Commit{
		Tree:      newTreeOID,
		Parents:   []object.Hash{headOID},
		Author:    c.Author,
		Committer: r.CommitterSignature(),
		Message:   c.Message,
	}

	newCommitOID, err := r.WriteCommit(newCommit)
	if err != nil {
		return nil, err
	}

	headRef, _ := r.Refs.ReadHEAD()
	logMsg := fmt.Sprintf("cherry-pick: %s", strings.TrimSpace(strings.Split(c.Message, "\n")[0]))
	if headRef != nil && headRef.IsSymref {
		_ = r.Refs.UpdateRef(headRef.Target, newCommitOID, r.CommitterSignature(), logMsg)
	} else {
		_ = r.Refs.UpdateRef("HEAD", newCommitOID, r.CommitterSignature(), logMsg)
	}

	_ = os.Remove(cherryHeadFile)
	_ = os.Remove(mergeMsgFile)

	return &merge.MergeResult{
		Status:    merge.MergeClean,
		CommitOID: newCommitOID,
		Message:   c.Message,
	}, nil
}

// Revert 创建与指定提交相反的提交，撤销其变动
func Revert(r *repo.Repository, commitOID object.Hash) (*merge.MergeResult, error) {
	c, err := r.ReadCommit(commitOID)
	if err != nil {
		return nil, fmt.Errorf("读取提交 %s 失败: %w", commitOID.String(), err)
	}

	if len(c.Parents) == 0 {
		return nil, fmt.Errorf("不能 revert 根提交")
	}

	parentCommit, err := r.ReadCommit(c.Parents[0])
	if err != nil {
		return nil, fmt.Errorf("读取父提交失败: %w", err)
	}

	headOID, err := r.Refs.ResolveHEAD()
	if err != nil {
		return nil, fmt.Errorf("无法解析 HEAD: %w", err)
	}
	headCommit, err := r.ReadCommit(headOID)
	if err != nil {
		return nil, err
	}

	// 反向 3-way merge: base = target commit, ours = HEAD, theirs = parent
	treeRes, err := merge.MergeTrees(r, c.Tree, headCommit.Tree, parentCommit.Tree, "HEAD", "parent of "+commitOID.String()[:7])
	if err != nil {
		return nil, err
	}

	if err := r.SaveIndex(treeRes.NewIndex); err != nil {
		return nil, err
	}

	firstLine := strings.TrimSpace(strings.Split(c.Message, "\n")[0])
	revertMsg := fmt.Sprintf("Revert \"%s\"\n\nThis reverts commit %s.\n", firstLine, commitOID.String())

	revertHeadFile := filepath.Join(r.GitDir, "REVERT_HEAD")
	mergeMsgFile := filepath.Join(r.GitDir, "MERGE_MSG")

	if len(treeRes.Conflicts) > 0 {
		_ = os.WriteFile(revertHeadFile, []byte(commitOID.String()+"\n"), 0644)
		_ = os.WriteFile(mergeMsgFile, []byte(revertMsg), 0644)
		return &merge.MergeResult{
			Status:    merge.MergeConflictStatus,
			Conflicts: treeRes.Conflicts,
			Message:   "could not revert " + commitOID.String()[:7],
		}, nil
	}

	newTreeOID, err := r.WriteTreeFromIndex(treeRes.NewIndex)
	if err != nil {
		return nil, err
	}

	committer := r.CommitterSignature()
	newCommit := &object.Commit{
		Tree:      newTreeOID,
		Parents:   []object.Hash{headOID},
		Author:    committer,
		Committer: committer,
		Message:   revertMsg,
	}

	newCommitOID, err := r.WriteCommit(newCommit)
	if err != nil {
		return nil, err
	}

	headRef, _ := r.Refs.ReadHEAD()
	logMsg := fmt.Sprintf("revert: %s", firstLine)
	if headRef != nil && headRef.IsSymref {
		_ = r.Refs.UpdateRef(headRef.Target, newCommitOID, committer, logMsg)
	} else {
		_ = r.Refs.UpdateRef("HEAD", newCommitOID, committer, logMsg)
	}

	_ = os.Remove(revertHeadFile)
	_ = os.Remove(mergeMsgFile)

	return &merge.MergeResult{
		Status:    merge.MergeClean,
		CommitOID: newCommitOID,
		Message:   revertMsg,
	}, nil
}
