package rev

import (
	"gogit/internal/object"
	"gogit/internal/repo"
	"sort"
	"strings"
	"time"
)

// RevListOptions 控制提交历史遍历的过滤与排序选项。
type RevListOptions struct {
	MaxCount  int
	Since     time.Time
	Until     time.Time
	Author    string
	Grep      string
	DateOrder bool
	TopoOrder bool
	All       bool
}

// CommitItem 包含提交哈希及其对应的结构体数据。
type CommitItem struct {
	Hash   object.Hash
	Commit *object.Commit
}

// RevList 按照 Git 标准拓扑/时间规则遍历提交历史，支持 include 集合与 exclude 集合（A..B 范围）。
func RevList(r *repo.Repository, includes []object.Hash, excludes []object.Hash, opts RevListOptions) ([]*CommitItem, error) {
	// 1. 计算排除集合（所有从 excludes 可达的提交）
	excludedSet := make(map[object.Hash]bool)
	queue := append([]object.Hash(nil), excludes...)
	for len(queue) > 0 {
		curr := queue[0]
		queue = queue[1:]
		if excludedSet[curr] {
			continue
		}
		excludedSet[curr] = true

		c, err := r.ReadCommit(curr)
		if err == nil {
			for _, p := range c.Parents {
				if !excludedSet[p] {
					queue = append(queue, p)
				}
			}
		}
	}

	// 2. 遍历包含集合
	visited := make(map[object.Hash]bool)
	var commits []*CommitItem
	inQueue := append([]object.Hash(nil), includes...)

	for len(inQueue) > 0 {
		curr := inQueue[0]
		inQueue = inQueue[1:]

		if visited[curr] || excludedSet[curr] {
			continue
		}
		visited[curr] = true

		c, err := r.ReadCommit(curr)
		if err != nil {
			continue
		}

		commits = append(commits, &CommitItem{Hash: curr, Commit: c})

		for _, p := range c.Parents {
			if !visited[p] && !excludedSet[p] {
				inQueue = append(inQueue, p)
			}
		}
	}

	// 3. 排序（默认按提交者时间戳倒序排列）
	sort.SliceStable(commits, func(i, j int) bool {
		return commits[i].Commit.Committer.When.After(commits[j].Commit.Committer.When)
	})

	// 4. 应用过滤规则（Author, Grep, Since, Until）
	var filtered []*CommitItem
	for _, item := range commits {
		c := item.Commit
		if !opts.Since.IsZero() && c.Committer.When.Before(opts.Since) {
			continue
		}
		if !opts.Until.IsZero() && c.Committer.When.After(opts.Until) {
			continue
		}
		if opts.Author != "" {
			authorStr := c.Author.String()
			if !strings.Contains(strings.ToLower(authorStr), strings.ToLower(opts.Author)) {
				continue
			}
		}
		if opts.Grep != "" {
			if !strings.Contains(strings.ToLower(c.Message), strings.ToLower(opts.Grep)) {
				continue
			}
		}

		filtered = append(filtered, item)
		if opts.MaxCount > 0 && len(filtered) >= opts.MaxCount {
			break
		}
	}

	return filtered, nil
}
