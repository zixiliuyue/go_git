package worktree

import (
	"fmt"
	"gogit/internal/index"
	"gogit/internal/object"
	"gogit/internal/repo"
	"os"
	"path/filepath"
	"strings"
)

// WorktreeInfo 表示工作区信息
type WorktreeInfo struct {
	Path     string
	HeadOID  object.Hash
	Branch   string
	IsBare   bool
	IsMain   bool
	IsLocked bool
}

// ListWorktrees 列出主仓库及其所有关联的附加工作区 (Linked Worktrees)
func ListWorktrees(r *repo.Repository) ([]WorktreeInfo, error) {
	var list []WorktreeInfo

	// 1. 主工作区
	mainInfo := WorktreeInfo{
		Path:   r.WorkTree,
		IsBare: r.IsBare,
		IsMain: true,
	}
	if headRef, err := r.Refs.ReadHEAD(); err == nil {
		mainInfo.HeadOID = headRef.Hash
		if headRef.IsSymref {
			mainInfo.Branch = headRef.Target
		}
	}
	list = append(list, mainInfo)

	// 2. 检查 .git/worktrees/ 目录
	worktreesDir := filepath.Join(r.GitDir, "worktrees")
	entries, err := os.ReadDir(worktreesDir)
	if err != nil {
		return list, nil // 无额外附加工作区
	}

	for _, d := range entries {
		if !d.IsDir() {
			continue
		}
		wtDir := filepath.Join(worktreesDir, d.Name())
		gitdirFile := filepath.Join(wtDir, "gitdir")
		gitdirData, err := os.ReadFile(gitdirFile)
		if err != nil {
			continue
		}

		targetDotGit := strings.TrimSpace(string(gitdirData))
		wtPath := filepath.Dir(targetDotGit)

		// 读取该 worktree 的 HEAD
		var headOID object.Hash
		var branch string
		headData, err := os.ReadFile(filepath.Join(wtDir, "HEAD"))
		if err == nil {
			trimmed := strings.TrimSpace(string(headData))
			if strings.HasPrefix(trimmed, "ref:") {
				branch = strings.TrimSpace(trimmed[4:])
				if h, err := r.Refs.ResolveRef(branch); err == nil {
					headOID = h
				}
			} else {
				h, _ := object.NewHashFromHex(trimmed)
				headOID = h
			}
		}

		list = append(list, WorktreeInfo{
			Path:    wtPath,
			HeadOID: headOID,
			Branch:  branch,
			IsMain:  false,
		})
	}

	return list, nil
}

// AddWorktree 创建并检出一个新的附加工作区
func AddWorktree(r *repo.Repository, targetPath, branchName string) error {
	absPath, err := filepath.Abs(targetPath)
	if err != nil {
		return err
	}

	if _, err := os.Stat(absPath); err == nil {
		// 目录已存在，检查是否非空
		entries, _ := os.ReadDir(absPath)
		if len(entries) > 0 {
			return fmt.Errorf("目标路径 %s 已存在且非空", absPath)
		}
	} else {
		if err := os.MkdirAll(absPath, 0755); err != nil {
			return err
		}
	}

	wtName := filepath.Base(absPath)
	wtMetaDir := filepath.Join(r.GitDir, "worktrees", wtName)
	if err := os.MkdirAll(wtMetaDir, 0755); err != nil {
		return err
	}

	// 1. 确定分支与目标提交
	if branchName == "" {
		branchName = wtName
	}
	refName := "refs/heads/" + branchName
	var targetCommit object.Hash

	if h, err := r.Refs.ResolveRef(refName); err == nil {
		targetCommit = h
	} else {
		// 分支不存在，从当前 HEAD 创建新分支
		curHEAD, err := r.Refs.ResolveHEAD()
		if err != nil {
			return fmt.Errorf("无法获取当前 HEAD: %w", err)
		}
		targetCommit = curHEAD
		sig := r.AuthorSignature()
		if err := r.Refs.UpdateRef(refName, curHEAD, sig, "worktree: add "+wtName); err != nil {
			return err
		}
	}

	// 2. 写入 .git/worktrees/<name>/ 下的元数据文件
	dotGitFile := filepath.Join(absPath, ".git")
	_ = os.WriteFile(filepath.Join(wtMetaDir, "gitdir"), []byte(dotGitFile+"\n"), 0644)
	_ = os.WriteFile(filepath.Join(wtMetaDir, "commondir"), []byte("../..\n"), 0644)
	_ = os.WriteFile(filepath.Join(wtMetaDir, "HEAD"), []byte("ref: "+refName+"\n"), 0644)

	// 3. 在目标工作区写入指向主仓库 meta 目录的 .git 文件
	_ = os.WriteFile(dotGitFile, []byte("gitdir: "+wtMetaDir+"\n"), 0644)

	// 4. 检出目标提交的树对象到目标工作区
	raw, err := r.ReadObject(targetCommit)
	if err != nil {
		return fmt.Errorf("读取提交失败: %w", err)
	}
	commit, err := object.ParseCommit(raw.Payload())
	if err != nil {
		return err
	}

	// 将 tree 写入 index 并还原到目标工作区
	idx := index.NewIndex()
	if err := checkoutTreeRecursive(r, commit.Tree, absPath, "", idx); err != nil {
		return fmt.Errorf("检出工作树失败: %w", err)
	}

	// 保存 worktree 索引文件
	wtIndexPath := filepath.Join(wtMetaDir, "index")
	return idx.WriteIndex(wtIndexPath)
}

// RemoveWorktree 移除指定的附加工作区
func RemoveWorktree(r *repo.Repository, targetPath string, force bool) error {
	absPath, err := filepath.Abs(targetPath)
	if err != nil {
		return err
	}

	wts, err := ListWorktrees(r)
	if err != nil {
		return err
	}

	var found *WorktreeInfo
	for i := range wts {
		if wts[i].Path == absPath {
			found = &wts[i]
			break
		}
	}

	if found == nil {
		return fmt.Errorf("未找到工作区: %s", targetPath)
	}
	if found.IsMain {
		return fmt.Errorf("无法移除主工作区")
	}

	// 删除工作区目录
	_ = os.RemoveAll(absPath)

	// 清理 .git/worktrees/<name>
	wtName := filepath.Base(absPath)
	wtMetaDir := filepath.Join(r.GitDir, "worktrees", wtName)
	_ = os.RemoveAll(wtMetaDir)

	return nil
}

// PruneWorktrees 清理指向已不存在路径的陈旧 worktrees 元数据
func PruneWorktrees(r *repo.Repository) (int, error) {
	worktreesDir := filepath.Join(r.GitDir, "worktrees")
	entries, err := os.ReadDir(worktreesDir)
	if err != nil {
		return 0, nil
	}

	pruned := 0
	for _, d := range entries {
		if !d.IsDir() {
			continue
		}
		wtDir := filepath.Join(worktreesDir, d.Name())
		gitdirFile := filepath.Join(wtDir, "gitdir")
		gitdirData, err := os.ReadFile(gitdirFile)
		if err != nil {
			_ = os.RemoveAll(wtDir)
			pruned++
			continue
		}

		targetDotGit := strings.TrimSpace(string(gitdirData))
		wtPath := filepath.Dir(targetDotGit)
		if _, err := os.Stat(wtPath); os.IsNotExist(err) {
			_ = os.RemoveAll(wtDir)
			pruned++
		}
	}

	return pruned, nil
}

func checkoutTreeRecursive(r *repo.Repository, treeHash object.Hash, rootDir, curDir string, idx *index.Index) error {
	raw, err := r.ReadObject(treeHash)
	if err != nil {
		return err
	}
	tree, err := object.ParseTree(raw.Payload())
	if err != nil {
		return err
	}

	for _, entry := range tree.Entries {
		relPath := filepath.Join(curDir, entry.Name)
		diskPath := filepath.Join(rootDir, relPath)

		if entry.Mode == object.ModeDirectory {
			if err := os.MkdirAll(diskPath, 0755); err != nil {
				return err
			}
			if err := checkoutTreeRecursive(r, entry.OID, rootDir, relPath, idx); err != nil {
				return err
			}
		} else {
			blobRaw, err := r.ReadObject(entry.OID)
			if err != nil {
				return err
			}
			perm := os.FileMode(0644)
			if entry.Mode == object.ModeExec {
				perm = 0755
			}
			_ = os.MkdirAll(filepath.Dir(diskPath), 0755)
			if err := os.WriteFile(diskPath, blobRaw.Payload(), perm); err != nil {
				return err
			}

			fi, _ := os.Stat(diskPath)
			if fi != nil {
				idx.AddOrReplaceEntry(index.EntryFromOSFileInfo(relPath, fi, entry.OID))
			}
		}
	}
	return nil
}
