package history

import (
	"fmt"
	"gogit/internal/object"
	"gogit/internal/repo"
	"gogit/internal/rev"
)

type DescribeOptions struct {
	Tags   bool
	Always bool
	Abbrev int
}

// Describe 寻找距离指定提交最近的标签并格式化（<tag>-<numCommits>-g<shortHash>）
func Describe(r *repo.Repository, commitStr string, opts DescribeOptions) (string, error) {
	if opts.Abbrev == 0 {
		opts.Abbrev = 7
	}

	targetOID, err := r.Refs.ResolveHEAD()
	if commitStr != "" {
		targetOID, err = rev.ParseRevision(r, commitStr)
	}
	if err != nil {
		return "", fmt.Errorf("解析目标提交失败: %w", err)
	}

	// 建立 commitOID -> 对应 tag 名称映射
	tagList, err := ListTags(r)
	if err != nil {
		return "", err
	}

	commitToTag := make(map[object.Hash]string)
	for _, tagName := range tagList {
		tagRefPath := "refs/tags/" + tagName
		rawOID, err := r.Refs.ResolveRef(tagRefPath)
		if err != nil {
			continue
		}
		peeledOID, err := PeelTag(r, rawOID)
		if err != nil {
			peeledOID = rawOID
		}
		commitToTag[peeledOID] = tagName
	}

	// 1. 若目标提交本身即有 tag，直接返回标签名
	if tagName, ok := commitToTag[targetOID]; ok {
		return tagName, nil
	}

	// 2. BFS 搜索最近的包含 tag 的祖先
	type queueItem struct {
		hash  object.Hash
		depth int
	}

	visited := make(map[object.Hash]bool)
	queue := []queueItem{{hash: targetOID, depth: 0}}

	for len(queue) > 0 {
		curr := queue[0]
		queue = queue[1:]

		if visited[curr.hash] {
			continue
		}
		visited[curr.hash] = true

		if curr.depth > 0 {
			if tagName, ok := commitToTag[curr.hash]; ok {
				shortHash := targetOID.String()
				if len(shortHash) > opts.Abbrev {
					shortHash = shortHash[:opts.Abbrev]
				}
				return fmt.Sprintf("%s-%d-g%s", tagName, curr.depth, shortHash), nil
			}
		}

		c, err := r.ReadCommit(curr.hash)
		if err != nil {
			continue
		}

		for _, p := range c.Parents {
			if !visited[p] {
				queue = append(queue, queueItem{hash: p, depth: curr.depth + 1})
			}
		}
	}

	if opts.Always {
		shortHash := targetOID.String()
		if len(shortHash) > opts.Abbrev {
			shortHash = shortHash[:opts.Abbrev]
		}
		return shortHash, nil
	}

	return "", fmt.Errorf("fatal: No names found, cannot describe anything.")
}
