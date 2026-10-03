package history

import (
	"errors"
	"fmt"
	"gogit/internal/object"
	"gogit/internal/pack"
	"gogit/internal/repo"
	"gogit/internal/rev"
	"os"
	"path/filepath"
)

// WriteCommitGraph 扫描仓库所有可达分支与引用，构建标准 Git commit-graph 并原子写入 .git/objects/info/commit-graph
func WriteCommitGraph(r *repo.Repository, reachableOnly bool) (string, error) {
	// 收集仓库中所有分支与标签的引用
	refsMap, err := r.Refs.ListRefs("refs/")
	if err != nil {
		return "", fmt.Errorf("列出引用失败: %w", err)
	}

	var rootCommits []object.Hash
	for _, h := range refsMap {
		if !h.IsZero() {
			rootCommits = append(rootCommits, h)
		}
	}

	// 加入 HEAD 对应提交
	headHash, err := r.Refs.ResolveHEAD()
	if err == nil && !headHash.IsZero() {
		rootCommits = append(rootCommits, headHash)
	}

	if len(rootCommits) == 0 {
		return "", errors.New("仓库中没有发现任何有效可达引用")
	}

	// 遍历出全部可达提交
	items, err := rev.RevList(r, rootCommits, nil, rev.RevListOptions{All: true})
	if err != nil {
		return "", fmt.Errorf("遍历提交历史失败: %w", err)
	}

	if len(items) == 0 {
		return "", errors.New("没有找到任何提交记录")
	}

	// 转换为 pack.CommitGraphInput 结构
	inputs := make([]pack.CommitGraphInput, len(items))
	for i, item := range items {
		ts := uint64(item.Commit.Committer.When.Unix())
		inputs[i] = pack.CommitGraphInput{
			OID:        item.Hash,
			Tree:       item.Commit.Tree,
			Parents:    item.Commit.Parents,
			CommitTime: ts,
		}
	}

	// 二进制编码
	graphData, err := pack.EncodeCommitGraph(inputs)
	if err != nil {
		return "", fmt.Errorf("编码 commit-graph 失败: %w", err)
	}

	infoDir := filepath.Join(r.ObjectsDir, "info")
	if err := os.MkdirAll(infoDir, 0755); err != nil {
		return "", fmt.Errorf("创建 info 目录失败: %w", err)
	}

	targetPath := filepath.Join(infoDir, "commit-graph")
	tmpPath := targetPath + ".tmp"

	if err := os.WriteFile(tmpPath, graphData, 0444); err != nil {
		return "", fmt.Errorf("写入临时 commit-graph 失败: %w", err)
	}

	_ = os.Chmod(targetPath, 0644)
	if err := os.Rename(tmpPath, targetPath); err != nil {
		_ = os.Remove(tmpPath)
		return "", fmt.Errorf("原子替换 commit-graph 失败: %w", err)
	}

	return targetPath, nil
}

// VerifyCommitGraph 对 commit-graph 进行严格数据与拓扑一致性校验
func VerifyCommitGraph(path string, r *repo.Repository) error {
	cg, err := pack.ReadCommitGraph(path)
	if err != nil {
		return fmt.Errorf("读取 commit-graph 失败: %w", err)
	}

	for _, entry := range cg.Entries {
		c, err := r.ReadCommit(entry.OID)
		if err != nil {
			return fmt.Errorf("提交 %s 在仓库中不存在: %w", entry.OID, err)
		}

		if c.Tree != entry.Tree {
			return fmt.Errorf("提交 %s 的 Tree OID 不匹配: 实际 %s != 记录 %s", entry.OID, c.Tree, entry.Tree)
		}

		if len(c.Parents) != len(entry.Parents) {
			return fmt.Errorf("提交 %s 的父提交数量不匹配: 实际 %d != 记录 %d", entry.OID, len(c.Parents), len(entry.Parents))
		}
		for i := range c.Parents {
			if c.Parents[i] != entry.Parents[i] {
				return fmt.Errorf("提交 %s 的第 %d 个父提交不匹配: 实际 %s != 记录 %s", entry.OID, i, c.Parents[i], entry.Parents[i])
			}
		}

		ts := uint64(c.Committer.When.Unix())
		if (ts & 0x00000003ffffffff) != entry.CommitTime {
			return fmt.Errorf("提交 %s 的时间戳不匹配: 实际 %d != 记录 %d", entry.OID, ts, entry.CommitTime)
		}
	}

	return nil
}
