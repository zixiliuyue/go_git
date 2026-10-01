package worktree

import (
	"errors"
	"fmt"
	"gogit/internal/index"
	"gogit/internal/object"
	"gogit/internal/repo"
	"gogit/internal/rev"
	"os"
	"path/filepath"
)

// CheckoutOptions 包含分支切换与检出的控制选项
type CheckoutOptions struct {
	CreateBranch bool
	BranchName   string
	Force        bool
}

// CheckoutSwitch 执行分支切换或检出指定修订版本，并更新暂存区与工作区文件
func CheckoutSwitch(r *repo.Repository, target string, opts CheckoutOptions) error {
	if r.IsBare {
		return errors.New("cannot checkout in bare repository")
	}

	var targetCommitHash object.Hash
	isBranch := false
	targetBranchRef := "refs/heads/" + target

	// 1. 判断 target 是否为已有分支
	if _, err := r.Refs.GetRef(targetBranchRef); err == nil {
		isBranch = true
		h, err := r.Refs.ResolveRef(targetBranchRef)
		if err != nil {
			return err
		}
		targetCommitHash = h
	} else if opts.CreateBranch {
		// 创建并切换到新分支
		curHead, err := r.Refs.ResolveHEAD()
		if err != nil {
			return fmt.Errorf("无法获取当前 HEAD 作为新分支起点: %w", err)
		}
		targetCommitHash = curHead
		if err := r.Refs.UpdateRef(targetBranchRef, targetCommitHash, r.CommitterSignature(), "branch: Created from HEAD"); err != nil {
			return err
		}
		isBranch = true
	} else {
		// 尝试解析为 commit / tag / 缩写哈希（detached HEAD 模式）
		h, err := rev.ParseRevision(r, target)
		if err != nil {
			return fmt.Errorf("error: pathspec '%s' did not match any file(s) known to git", target)
		}
		targetCommitHash = h
	}

	// 2. 加载目标提交的树对象
	targetCommit, err := r.ReadCommit(targetCommitHash)
	if err != nil {
		return fmt.Errorf("加载目标提交失败: %w", err)
	}

	targetFiles := make(map[string]object.TreeEntry)
	flattenTree(r, targetCommit.Tree, "", targetFiles)

	// 3. 读取当前索引与工作区
	curIdx, err := r.GetIndex()
	if err != nil {
		curIdx = index.NewIndex()
	}

	// 4. 清理旧索引中存在但在目标提交中不存在的工作区文件
	for _, entry := range curIdx.Entries {
		if _, exists := targetFiles[entry.Path]; !exists {
			absPath := filepath.Join(r.WorkTree, entry.Path)
			_ = os.Remove(absPath)
		}
	}

	// 5. 检出目标树中的所有文件到工作区，并重建索引
	newIdx := index.NewIndex()
	for p, te := range targetFiles {
		rawObj, err := r.ReadObject(te.OID)
		if err != nil {
			return fmt.Errorf("读取对象 %s 失败: %w", te.OID.String(), err)
		}

		absPath := filepath.Join(r.WorkTree, p)
		if err := os.MkdirAll(filepath.Dir(absPath), 0755); err != nil {
			return err
		}

		fileMode := os.FileMode(0644)
		if te.Mode == object.ModeExec {
			fileMode = 0755
		}

		if err := os.WriteFile(absPath, rawObj.Content, fileMode); err != nil {
			return fmt.Errorf("写入工作区文件 %s 失败: %w", p, err)
		}

		fi, err := os.Lstat(absPath)
		if err != nil {
			return err
		}

		entry := index.EntryFromOSFileInfo(p, fi, te.OID)
		newIdx.AddOrReplaceEntry(entry)
	}

	// 6. 保存新索引
	if err := r.SaveIndex(newIdx); err != nil {
		return fmt.Errorf("保存新索引失败: %w", err)
	}

	// 7. 更新 HEAD 指向
	curHead, _ := r.Refs.ResolveHEAD()
	committer := r.CommitterSignature()
	if isBranch {
		logMsg := fmt.Sprintf("checkout: moving to %s", target)
		if err := r.Refs.SetHEADSymbolic(targetBranchRef); err != nil {
			return err
		}
		_ = r.Refs.AppendReflog("HEAD", curHead, targetCommitHash, committer, logMsg)
	} else {
		logMsg := fmt.Sprintf("checkout: moving to %s", targetCommitHash.String())
		if err := r.Refs.SetHEADDetached(targetCommitHash); err != nil {
			return err
		}
		_ = r.Refs.AppendReflog("HEAD", curHead, targetCommitHash, committer, logMsg)
	}

	return nil
}

// RestoreFiles 将指定路径恢复到暂存区或工作区状态
func RestoreFiles(r *repo.Repository, paths []string, staged bool, worktree bool) error {
	idx, err := r.GetIndex()
	if err != nil {
		return err
	}

	headOID, _ := r.Refs.ResolveHEAD()
	var headFiles map[string]object.TreeEntry
	if !headOID.IsZero() {
		headFiles = make(map[string]object.TreeEntry)
		if c, err := r.ReadCommit(headOID); err == nil {
			flattenTree(r, c.Tree, "", headFiles)
		}
	}

	for _, spec := range paths {
		cleanSpec := filepath.ToSlash(spec)

		if staged {
			// --staged 模式：用 HEAD 恢复索引中的条目
			if te, ok := headFiles[cleanSpec]; ok {
				idx.AddOrReplaceEntry(&index.IndexEntry{
					Path: cleanSpec,
					OID:  te.OID,
					Mode: uint32(te.Mode),
				})
			} else {
				idx.RemoveEntry(cleanSpec)
			}
		}

		if worktree {
			// 默认/--worktree 模式：用索引恢复工作区文件
			entry, ok := idx.FindEntry(cleanSpec)
			absPath := filepath.Join(r.WorkTree, cleanSpec)
			if !ok {
				_ = os.Remove(absPath)
			} else {
				raw, err := r.ReadObject(entry.OID)
				if err == nil {
					_ = os.MkdirAll(filepath.Dir(absPath), 0755)
					mode := os.FileMode(0644)
					if entry.Mode == 0100755 {
						mode = 0755
					}
					_ = os.WriteFile(absPath, raw.Content, mode)
				}
			}
		}
	}

	return r.SaveIndex(idx)
}

// CleanWorktree 清理工作区中的未跟踪与忽略文件
func CleanWorktree(r *repo.Repository, removeDirs bool, removeIgnored bool) ([]string, error) {
	status, err := ComputeStatus(r)
	if err != nil {
		return nil, err
	}

	var removed []string
	for _, item := range status.Items {
		if item.IsUntracked || (removeIgnored && item.IsIgnored) {
			absPath := filepath.Join(r.WorkTree, item.Path)
			fi, err := os.Stat(absPath)
			if err != nil {
				continue
			}
			if fi.IsDir() {
				if removeDirs {
					_ = os.RemoveAll(absPath)
					removed = append(removed, item.Path)
				}
			} else {
				_ = os.Remove(absPath)
				removed = append(removed, item.Path)
			}
		}
	}

	return removed, nil
}

func flattenTree(r *repo.Repository, treeHash object.Hash, prefix string, result map[string]object.TreeEntry) {
	t, err := r.ReadTree(treeHash)
	if err != nil {
		return
	}
	for _, e := range t.Entries {
		entryPath := filepath.Join(prefix, e.Name)
		entryPath = filepath.ToSlash(entryPath)
		if e.Mode == object.ModeDirectory {
			flattenTree(r, e.OID, entryPath, result)
		} else {
			result[entryPath] = e
		}
	}
}

