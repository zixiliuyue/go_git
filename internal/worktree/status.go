package worktree

import (
	"fmt"
	"gogit/internal/diff"
	"gogit/internal/index"
	"gogit/internal/object"
	"gogit/internal/repo"
	"gogit/internal/telemetry"
	"gogit/internal/trace2"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// StatusItem 描述单个文件在状态机中的双通道状态（X 为暂存区 vs HEAD，Y 为工作区 vs 暂存区）
type StatusItem struct {
	Path        string
	OrigPath    string // 用于 rename 场景
	Staged      byte   // ' ', 'M', 'A', 'D', 'R', 'C'
	Unstaged    byte   // ' ', 'M', 'D'
	IsUntracked bool
	IsIgnored   bool
}

// StatusResult 保存完整的仓库状态检查结果
type StatusResult struct {
	BranchName string
	IsDetached bool
	HeadOID    object.Hash
	Items      []*StatusItem
}

// UntrackedMode 控制未跟踪文件的展示策略
type UntrackedMode string

const (
	// UntrackedNo 不展示任何未跟踪文件
	UntrackedNo UntrackedMode = "no"
	// UntrackedNormal 默认模式：对完全未跟踪的子目录自动折叠为 dir/
	UntrackedNormal UntrackedMode = "normal"
	// UntrackedAll 展开列出所有层级的未跟踪文件
	UntrackedAll UntrackedMode = "all"
)

// StatusOptions 状态计算选项
type StatusOptions struct {
	Untracked UntrackedMode
}

// ComputeStatus 计算当前仓库完整状态（遵循 Git status --porcelain 规范，默认折叠未跟踪目录）
func ComputeStatus(r *repo.Repository) (*StatusResult, error) {
	return ComputeStatusWithOptions(r, StatusOptions{Untracked: UntrackedNormal})
}

// ComputeStatusWithOptions 根据给定的 UntrackedMode 等选项计算工作区状态
func ComputeStatusWithOptions(r *repo.Repository, opts StatusOptions) (*StatusResult, error) {
	if opts.Untracked == "" {
		opts.Untracked = UntrackedNormal
	}
	defer trace2.Region("status", "compute_status")()
	telemetry.ForSubsystem("worktree").Debug("开始计算仓库工作区状态", "worktree", r.WorkTree)
	// 1. 初始化 IgnoreMatcher 并加载各级 ignore 文件
	ignorer := NewIgnoreMatcher()
	// 加载仓库根目录 .gitignore
	_ = ignorer.LoadIgnoreFile(filepath.Join(r.WorkTree, ".gitignore"), "")
	// 加载 .git/info/exclude
	_ = ignorer.LoadIgnoreFile(filepath.Join(r.GitDir, "info", "exclude"), "")
	// 加载用户全局 excludesfile
	if globalExclude := r.Config.Get("core", "excludesfile"); globalExclude != "" {
		_ = ignorer.LoadIgnoreFile(globalExclude, "")
	} else if xdg := os.Getenv("XDG_CONFIG_HOME"); xdg != "" {
		_ = ignorer.LoadIgnoreFile(filepath.Join(xdg, "git", "ignore"), "")
	} else if home, err := os.UserHomeDir(); err == nil {
		_ = ignorer.LoadIgnoreFile(filepath.Join(home, ".config", "git", "ignore"), "")
	}

	// 2. 读取 HEAD
	var headTreeHash object.Hash
	var branchName string
	isDetached := false

	headRef, err := r.Refs.ReadHEAD()
	if err == nil {
		if headRef.IsSymref {
			branchName = strings.TrimPrefix(headRef.Target, "refs/heads/")
		} else {
			isDetached = true
			branchName = headRef.Hash.String()[:7]
		}
		if !headRef.Hash.IsZero() {
			commit, err := r.ReadCommit(headRef.Hash)
			if err == nil {
				headTreeHash = commit.Tree
			}
		}
	}

	// 3. 读取暂存区索引
	idx, err := r.GetIndex()
	if err != nil {
		return nil, fmt.Errorf("读取暂存区索引失败: %w", err)
	}

	// 3.1 若配置了 core.fsmonitor，执行事件查询并更新条目有效性
	fsmonUpdated, _ := ApplyFSMonitorUpdate(r, idx)

	itemMap := make(map[string]*StatusItem)
	getOrCreate := func(p string) *StatusItem {
		if item, ok := itemMap[p]; ok {
			return item
		}
		item := &StatusItem{Path: p, Staged: ' ', Unstaged: ' '}
		itemMap[p] = item
		return item
	}

	// 4. 计算 Staged (暂存区 vs HEAD 树)
	stagedDiffs, _ := diff.DiffIndexWithTree(r, idx, headTreeHash, diff.DiffOptions{DetectRenames: true})
	for _, d := range stagedDiffs {
		p := d.NewPath
		if p == "" {
			p = d.OldPath
		}
		item := getOrCreate(p)
		item.Staged = byte(d.Status)
		if d.Status == diff.StatusRenamed {
			item.OrigPath = d.OldPath
		}
	}

	// 5. 建立 Index 条目映射
	indexMap := make(map[string]*index.IndexEntry)
	for _, entry := range idx.Entries {
		indexMap[entry.Path] = entry
	}

	// 6. 扫描工作区文件与子目录（判定 Unstaged 与 Untracked）
	worktreePaths := make(map[string]bool)

	if !r.IsBare && r.WorkTree != "" {
		err = filepath.Walk(r.WorkTree, func(path string, info os.FileInfo, err error) error {
			if err != nil {
				return nil
			}
			relPath, relErr := filepath.Rel(r.WorkTree, path)
			if relErr != nil || relPath == "." {
				return nil
			}
			relPath = filepath.ToSlash(relPath)

			// 跳过 .git
			if info.IsDir() && (info.Name() == ".git" || strings.HasPrefix(relPath, ".git/")) {
				return filepath.SkipDir
			}

			if info.IsDir() {
				// 支持目录内局部 .gitignore 加载
				localIgnore := filepath.Join(path, ".gitignore")
				if _, statErr := os.Stat(localIgnore); statErr == nil {
					_ = ignorer.LoadIgnoreFile(localIgnore, relPath)
				}
				if ignorer.Match(relPath, true) {
					// 仅在目录下没有任何已跟踪文件时整体跳过目录
					hasTracked := false
					prefix := relPath + "/"
					for p := range indexMap {
						if strings.HasPrefix(p, prefix) {
							hasTracked = true
							break
						}
					}
					if !hasTracked {
						return filepath.SkipDir
					}
				} else if opts.Untracked == UntrackedNormal {
					// 默认 normal 模式：若当前目录下完全不存在已跟踪文件，且包含非忽略文件，则折叠为 dir/
					hasTracked := false
					prefix := relPath + "/"
					for p := range indexMap {
						if strings.HasPrefix(p, prefix) {
							hasTracked = true
							break
						}
					}
					if !hasTracked {
						if dirHasNonIgnored(path, relPath, ignorer) {
							item := getOrCreate(prefix)
							item.IsUntracked = true
							item.Staged = '?'
							item.Unstaged = '?'
						}
						return filepath.SkipDir
					}
				}
				return nil
			}

			// 检查未跟踪文件是否被忽略（已跟踪文件永远不受 .gitignore 影响）
			entry, inIndex := indexMap[relPath]
			if !inIndex && ignorer.Match(relPath, false) {
				return nil
			}

			worktreePaths[relPath] = true
			if !inIndex {
				if opts.Untracked != UntrackedNo {
					item := getOrCreate(relPath)
					item.IsUntracked = true
					item.Staged = '?'
					item.Unstaged = '?'
				}
			} else {
				// 若 FSMonitor 已验证该文件自上次快照未发生变更，直接跳过磁盘读取与比对
				if entry.IsFSMonitorValid() {
					return nil
				}

				// 已跟踪文件：比对 stat 缓存与内容哈希
				if !entry.MatchStat(info) {
					data, readErr := os.ReadFile(path)
					if readErr == nil {
						curOID := object.HashObject(object.TypeBlob, data)
						if curOID != entry.OID {
							item := getOrCreate(relPath)
							item.Unstaged = 'M'
						}
					}
				}
			}

			return nil
		})
	}

	// 7. 检查工作区被删除的文件 (在 index 中但不在工作区)
	for p, entry := range indexMap {
		if entry.IsSkipWorktree() || entry.IsSparseDirectory() {
			continue
		}
		if !worktreePaths[p] {
			item := getOrCreate(p)
			item.Unstaged = 'D'
		}
	}

	// 8. 过滤无变动的条目并按路径排序
	var items []*StatusItem
	for _, item := range itemMap {
		if item.Staged != ' ' || item.Unstaged != ' ' || item.IsUntracked {
			items = append(items, item)
		}
	}

	sort.Slice(items, func(i, j int) bool {
		return items[i].Path < items[j].Path
	})

	headOID := object.ZeroHash
	if headRef != nil {
		headOID = headRef.Hash
	}

	if fsmonUpdated {
		_ = idx.WriteIndex(r.IndexPath)
	}

	return &StatusResult{
		BranchName: branchName,
		IsDetached: isDetached,
		HeadOID:    headOID,
		Items:      items,
	}, nil
}

// FormatPorcelain 格式化为与 Git porcelain v1 逐行对齐的标准输出（先已跟踪条目，后未跟踪条目）
func (s *StatusResult) FormatPorcelain() string {
	var sb strings.Builder
	for _, item := range s.Items {
		if !item.IsUntracked {
			if item.Staged == 'R' {
				unstagedChar := item.Unstaged
				if unstagedChar == 0 {
					unstagedChar = ' '
				}
				sb.WriteString(fmt.Sprintf("R%c %s -> %s\n", unstagedChar, item.OrigPath, item.Path))
			} else {
				sb.WriteString(fmt.Sprintf("%c%c %s\n", item.Staged, item.Unstaged, item.Path))
			}
		}
	}
	for _, item := range s.Items {
		if item.IsUntracked {
			sb.WriteString(fmt.Sprintf("?? %s\n", item.Path))
		}
	}
	return sb.String()
}

// dirHasNonIgnored 递归检测目录下是否包含任何未被忽略的文件，用于未跟踪目录的智能折叠
func dirHasNonIgnored(absDir, relDir string, ignorer *IgnoreMatcher) bool {
	entries, err := os.ReadDir(absDir)
	if err != nil {
		return false
	}
	for _, e := range entries {
		subRel := filepath.ToSlash(filepath.Join(relDir, e.Name()))
		if e.IsDir() {
			if e.Name() == ".git" {
				continue
			}
			if ignorer.Match(subRel, true) {
				continue
			}
			if dirHasNonIgnored(filepath.Join(absDir, e.Name()), subRel, ignorer) {
				return true
			}
		} else {
			if !ignorer.Match(subRel, false) {
				return true
			}
		}
	}
	return false
}
