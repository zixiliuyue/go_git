package history

import (
	"fmt"
	"gogit/internal/merge"
	"gogit/internal/object"
	"gogit/internal/repo"
	"gogit/internal/rev"
	"gogit/internal/worktree"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// RebaseTodoItem 表示交互式或序列 rebase 中的单步指令
type RebaseTodoItem struct {
	Action string // pick, squash, fixup, drop, edit
	Hash   object.Hash
	Title  string
}

// StartRebase 启动 rebase 流程
func StartRebase(r *repo.Repository, upstreamStr, ontoStr string, todoItems []RebaseTodoItem) (*merge.MergeResult, error) {
	rebaseDir := filepath.Join(r.GitDir, "rebase-merge")
	if _, err := os.Stat(rebaseDir); err == nil {
		return nil, fmt.Errorf("已有正在进行的 rebase 流程，请使用 --continue 或 --abort")
	}

	headOID, err := r.Refs.ResolveHEAD()
	if err != nil {
		return nil, fmt.Errorf("无法解析 HEAD: %w", err)
	}

	headRef, _ := r.Refs.ReadHEAD()
	branchName := "HEAD"
	if headRef != nil && headRef.IsSymref {
		branchName = headRef.Target
	}

	upstreamOID, err := rev.ParseRevision(r, upstreamStr)
	if err != nil {
		return nil, fmt.Errorf("无法解析 upstream %s: %w", upstreamStr, err)
	}

	ontoOID := upstreamOID
	if ontoStr != "" {
		o, err := rev.ParseRevision(r, ontoStr)
		if err != nil {
			return nil, fmt.Errorf("无法解析 onto %s: %w", ontoStr, err)
		}
		ontoOID = o
	}

	// 若未显式传入 todoItems，则通过 rev-list 收集 upstream..HEAD 之间的提交（按正序）
	if len(todoItems) == 0 {
		commits, err := rev.RevList(r, []object.Hash{headOID}, []object.Hash{upstreamOID}, rev.RevListOptions{})
		if err != nil {
			return nil, fmt.Errorf("获取待 rebase 提交列表失败: %w", err)
		}
		if len(commits) == 0 {
			return &merge.MergeResult{
				Status:  merge.MergeAlreadyUpToDate,
				Message: "Current branch is up to date.",
			}, nil
		}

		// 倒序转换为正序（从老提交到新提交重放）
		for i := len(commits) - 1; i >= 0; i-- {
			c := commits[i]
			title := strings.TrimSpace(strings.Split(c.Commit.Message, "\n")[0])
			todoItems = append(todoItems, RebaseTodoItem{
				Action: "pick",
				Hash:   c.Hash,
				Title:  title,
			})
		}
	}

	// 创建 rebase-merge 状态目录
	if err := os.MkdirAll(rebaseDir, 0755); err != nil {
		return nil, err
	}

	_ = os.WriteFile(filepath.Join(rebaseDir, "head-name"), []byte(branchName+"\n"), 0644)
	_ = os.WriteFile(filepath.Join(rebaseDir, "onto"), []byte(ontoOID.String()+"\n"), 0644)
	_ = os.WriteFile(filepath.Join(rebaseDir, "orig-head"), []byte(headOID.String()+"\n"), 0644)
	_ = os.WriteFile(filepath.Join(rebaseDir, "msgnum"), []byte("0\n"), 0644)
	_ = os.WriteFile(filepath.Join(rebaseDir, "end"), []byte(fmt.Sprintf("%d\n", len(todoItems))), 0644)

	var todoSb strings.Builder
	for _, item := range todoItems {
		todoSb.WriteString(fmt.Sprintf("%s %s %s\n", item.Action, item.Hash.String(), item.Title))
	}
	_ = os.WriteFile(filepath.Join(rebaseDir, "git-rebase-todo"), []byte(todoSb.String()), 0644)

	// 将工作区与 HEAD 分离切换至 ontoOID
	if err := worktree.CheckoutSwitch(r, ontoOID.String(), worktree.CheckoutOptions{}); err != nil {
		_ = os.RemoveAll(rebaseDir)
		return nil, fmt.Errorf("检出 onto 提交失败: %w", err)
	}

	return ContinueRebase(r)
}

// ContinueRebase 继续当前进行中的 rebase 流程
func ContinueRebase(r *repo.Repository) (*merge.MergeResult, error) {
	rebaseDir := filepath.Join(r.GitDir, "rebase-merge")
	if _, err := os.Stat(rebaseDir); os.IsNotExist(err) {
		return nil, fmt.Errorf("没有正在进行的 rebase")
	}

	todoBytes, err := os.ReadFile(filepath.Join(rebaseDir, "git-rebase-todo"))
	if err != nil {
		return nil, err
	}

	lines := strings.Split(strings.TrimSpace(string(todoBytes)), "\n")
	msgNumBytes, _ := os.ReadFile(filepath.Join(rebaseDir, "msgnum"))
	currentIdx, _ := strconv.Atoi(strings.TrimSpace(string(msgNumBytes)))

	for currentIdx < len(lines) {
		line := strings.TrimSpace(lines[currentIdx])
		if line == "" || strings.HasPrefix(line, "#") {
			currentIdx++
			continue
		}

		parts := strings.Fields(line)
		if len(parts) < 2 {
			currentIdx++
			continue
		}

		action := parts[0]
		targetHash, err := object.NewHashFromHex(parts[1])
		if err != nil {
			return nil, fmt.Errorf("非法 todo 提交哈希: %s", parts[1])
		}

		if action == "drop" {
			currentIdx++
			_ = os.WriteFile(filepath.Join(rebaseDir, "msgnum"), []byte(fmt.Sprintf("%d\n", currentIdx)), 0644)
			continue
		}

		// 执行单步 cherry-pick
		res, err := CherryPick(r, targetHash)
		if err != nil {
			return nil, err
		}

		if res.Status == merge.MergeConflictStatus {
			// 发生冲突，保存当前步骤并暂停
			_ = os.WriteFile(filepath.Join(rebaseDir, "msgnum"), []byte(fmt.Sprintf("%d\n", currentIdx)), 0644)
			return res, nil
		}

		currentIdx++
		_ = os.WriteFile(filepath.Join(rebaseDir, "msgnum"), []byte(fmt.Sprintf("%d\n", currentIdx)), 0644)
	}

	// 所有步骤完成，将原分支指针指向新 HEAD 并清理状态
	headBranchBytes, _ := os.ReadFile(filepath.Join(rebaseDir, "head-name"))
	headBranch := strings.TrimSpace(string(headBranchBytes))

	newHeadOID, err := r.Refs.ResolveHEAD()
	if err != nil {
		return nil, err
	}

	if headBranch != "" && headBranch != "HEAD" {
		_ = r.Refs.UpdateRef(headBranch, newHeadOID, r.CommitterSignature(), "rebase: fast-forward")
		_ = r.Refs.SetHEADSymbolic(headBranch)
	}

	_ = os.RemoveAll(rebaseDir)

	return &merge.MergeResult{
		Status:    merge.MergeClean,
		CommitOID: newHeadOID,
		Message:   "Successfully rebased and updated.",
	}, nil
}

// AbortRebase 放弃当前正在进行的 rebase 并恢复原状态
func AbortRebase(r *repo.Repository) error {
	rebaseDir := filepath.Join(r.GitDir, "rebase-merge")
	if _, err := os.Stat(rebaseDir); os.IsNotExist(err) {
		return fmt.Errorf("没有正在进行的 rebase")
	}

	origHeadBytes, err := os.ReadFile(filepath.Join(rebaseDir, "orig-head"))
	if err != nil {
		return err
	}
	origHead := strings.TrimSpace(string(origHeadBytes))

	headBranchBytes, _ := os.ReadFile(filepath.Join(rebaseDir, "head-name"))
	headBranch := strings.TrimSpace(string(headBranchBytes))

	// 还原工作区与索引到 orig-head
	if err := worktree.CheckoutSwitch(r, origHead, worktree.CheckoutOptions{}); err != nil {
		return err
	}

	if headBranch != "" && headBranch != "HEAD" {
		_ = r.Refs.SetHEADSymbolic(headBranch)
	}

	_ = os.RemoveAll(rebaseDir)
	return nil
}
