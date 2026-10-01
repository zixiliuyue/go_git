package merge

import (
	"fmt"
	"gogit/internal/object"
	"gogit/internal/repo"
	"gogit/internal/rev"
	"gogit/internal/worktree"
	"os"
	"path/filepath"
)

type MergeStatus int

const (
	MergeAlreadyUpToDate MergeStatus = iota
	MergeFastForward
	MergeClean
	MergeConflictStatus
)

type MergeOptions struct {
	NoFF    bool
	Message string
}

type MergeResult struct {
	Status        MergeStatus
	CommitOID     object.Hash
	FastForwardTo object.Hash
	Conflicts     []MergeConflict
	Message       string
}

// DoMerge 执行分支合并（支持 Fast-forward、--no-ff 与真实三路合并）
func DoMerge(r *repo.Repository, targetBranchOrCommit string, opts MergeOptions) (*MergeResult, error) {
	headOID, err := r.Refs.ResolveHEAD()
	if err != nil {
		return nil, fmt.Errorf("无法解析 HEAD: %w", err)
	}

	targetOID, err := rev.ParseRevision(r, targetBranchOrCommit)
	if err != nil {
		return nil, fmt.Errorf("无法解析目标修订版本 %s: %w", targetBranchOrCommit, err)
	}

	lcaOID, err := FindMergeBase(r, headOID, targetOID)
	if err != nil {
		return nil, fmt.Errorf("查找 merge-base 失败: %w", err)
	}

	// 1. 检查是否已经是最新
	if lcaOID == targetOID {
		return &MergeResult{
			Status:  MergeAlreadyUpToDate,
			Message: "Already up to date.",
		}, nil
	}

	// 2. 检查是否为 Fast-forward
	if lcaOID == headOID && !opts.NoFF {
		origHeadRef, _ := r.Refs.ReadHEAD()

		// 执行 Fast-forward 检出与引用推进
		checkoutOpts := worktree.CheckoutOptions{}
		if err := worktree.CheckoutSwitch(r, targetOID.String(), checkoutOpts); err != nil {
			return nil, fmt.Errorf("快进检出失败: %w", err)
		}

		reflogMsg := fmt.Sprintf("merge %s: Fast-forward", targetBranchOrCommit)
		if origHeadRef != nil && origHeadRef.IsSymref {
			_ = r.Refs.SetHEADSymbolic(origHeadRef.Target)
			_ = r.Refs.UpdateRef(origHeadRef.Target, targetOID, r.CommitterSignature(), reflogMsg)
		} else {
			_ = r.Refs.UpdateRef("HEAD", targetOID, r.CommitterSignature(), reflogMsg)
		}

		return &MergeResult{
			Status:        MergeFastForward,
			FastForwardTo: targetOID,
			Message:       fmt.Sprintf("Updating %s..%s\nFast-forward", headOID.String()[:7], targetOID.String()[:7]),
		}, nil
	}

	// 3. 真实三路合并
	headCommit, err := r.ReadCommit(headOID)
	if err != nil {
		return nil, err
	}
	targetCommit, err := r.ReadCommit(targetOID)
	if err != nil {
		return nil, err
	}
	lcaCommit, err := r.ReadCommit(lcaOID)
	if err != nil {
		return nil, err
	}

	treeRes, err := MergeTrees(r, lcaCommit.Tree, headCommit.Tree, targetCommit.Tree, "HEAD", targetBranchOrCommit)
	if err != nil {
		return nil, fmt.Errorf("执行树级合并失败: %w", err)
	}

	// 保存更新后的索引
	if err := r.SaveIndex(treeRes.NewIndex); err != nil {
		return nil, fmt.Errorf("保存合并索引失败: %w", err)
	}

	mergeMsg := opts.Message
	if mergeMsg == "" {
		mergeMsg = fmt.Sprintf("Merge branch '%s'", targetBranchOrCommit)
	}

	// 如果有冲突，写入 MERGE_HEAD 与 MERGE_MSG 并中止提交
	if len(treeRes.Conflicts) > 0 {
		mergeHeadFile := filepath.Join(r.GitDir, "MERGE_HEAD")
		mergeMsgFile := filepath.Join(r.GitDir, "MERGE_MSG")
		mergeModeFile := filepath.Join(r.GitDir, "MERGE_MODE")

		_ = os.WriteFile(mergeHeadFile, []byte(targetOID.String()+"\n"), 0644)
		_ = os.WriteFile(mergeMsgFile, []byte(mergeMsg+"\n"), 0644)
		if opts.NoFF {
			_ = os.WriteFile(mergeModeFile, []byte("no-ff\n"), 0644)
		}

		return &MergeResult{
			Status:    MergeConflictStatus,
			Conflicts: treeRes.Conflicts,
			Message:   "Automatic merge failed; fix conflicts and then commit the result.",
		}, nil
	}

	// 4. 无冲突自动生成合并提交
	newTreeOID, err := r.WriteTreeFromIndex(treeRes.NewIndex)
	if err != nil {
		return nil, fmt.Errorf("写入合并树对象失败: %w", err)
	}

	author := r.AuthorSignature()
	committer := r.CommitterSignature()
	parents := []object.Hash{headOID, targetOID}

	commit := &object.Commit{
		Tree:      newTreeOID,
		Parents:   parents,
		Author:    author,
		Committer: committer,
		Message:   mergeMsg + "\n",
	}

	commitOID, err := r.WriteCommit(commit)
	if err != nil {
		return nil, fmt.Errorf("创建合并提交失败: %w", err)
	}

	// 更新分支引用与 reflog
	headRef, _ := r.Refs.ReadHEAD()
	reflogMsg := fmt.Sprintf("merge %s: Merge made by the 'ort' strategy.", targetBranchOrCommit)
	if headRef != nil && headRef.IsSymref {
		_ = r.Refs.UpdateRef(headRef.Target, commitOID, committer, reflogMsg)
	} else {
		_ = r.Refs.UpdateRef("HEAD", commitOID, committer, reflogMsg)
	}

	// 清理合并状态文件
	_ = os.Remove(filepath.Join(r.GitDir, "MERGE_HEAD"))
	_ = os.Remove(filepath.Join(r.GitDir, "MERGE_MSG"))
	_ = os.Remove(filepath.Join(r.GitDir, "MERGE_MODE"))

	return &MergeResult{
		Status:    MergeClean,
		CommitOID: commitOID,
		Message:   mergeMsg,
	}, nil
}
