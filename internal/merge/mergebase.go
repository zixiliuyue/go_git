package merge

import (
	"fmt"
	"gogit/internal/object"
	"gogit/internal/repo"
)

// FindMergeBase 寻找两个提交的最低公共祖先 (Lowest Common Ancestor / merge-base)。
// 遵循 Git 的 commit graph 拓扑可达性算法：
// 1. 遍历 A 的所有可达祖先集合；
// 2. 遍历 B 的所有可达祖先集合；
// 3. 计算交集，并过滤掉已被其他公共祖先包含的较早提交，得到最近的公共祖先。
func FindMergeBase(r *repo.Repository, hashA, hashB object.Hash) (object.Hash, error) {
	if hashA == hashB {
		return hashA, nil
	}

	ancestorsA, err := getReachableSet(r, hashA)
	if err != nil {
		return object.ZeroHash, fmt.Errorf("遍历 A 的祖先失败: %w", err)
	}

	ancestorsB, err := getReachableSet(r, hashB)
	if err != nil {
		return object.ZeroHash, fmt.Errorf("遍历 B 的祖先失败: %w", err)
	}

	// 计算公共祖先交集
	common := make(map[object.Hash]bool)
	for h := range ancestorsA {
		if ancestorsB[h] {
			common[h] = true
		}
	}

	if len(common) == 0 {
		return object.ZeroHash, fmt.Errorf("未找到公共祖先（无关联历史）")
	}

	// 从公共祖先中筛选极大元（即不是任何其他公共祖先的真祖先）
	var mergeBases []object.Hash
	for candidate := range common {
		isStrictlyAncestorOfAnother := false
		for other := range common {
			if candidate == other {
				continue
			}
			// 检查 candidate 是否在 other 的真祖先集中
			otherAncestors, _ := getStrictAncestors(r, other)
			if otherAncestors[candidate] {
				isStrictlyAncestorOfAnother = true
				break
			}
		}
		if !isStrictlyAncestorOfAnother {
			mergeBases = append(mergeBases, candidate)
		}
	}

	if len(mergeBases) == 0 {
		return object.ZeroHash, fmt.Errorf("无法确定最佳公共祖先")
	}

	// 优先返回第一个最优祖先
	return mergeBases[0], nil
}

// getReachableSet 获取指定提交本身及所有可达祖先集合
func getReachableSet(r *repo.Repository, start object.Hash) (map[object.Hash]bool, error) {
	visited := make(map[object.Hash]bool)
	queue := []object.Hash{start}

	for len(queue) > 0 {
		curr := queue[0]
		queue = queue[1:]

		if visited[curr] {
			continue
		}
		visited[curr] = true

		c, err := r.ReadCommit(curr)
		if err != nil {
			return nil, err
		}

		for _, p := range c.Parents {
			if !visited[p] {
				queue = append(queue, p)
			}
		}
	}

	return visited, nil
}

// getStrictAncestors 获取指定提交的所有严格真祖先集合（不含自身）
func getStrictAncestors(r *repo.Repository, start object.Hash) (map[object.Hash]bool, error) {
	c, err := r.ReadCommit(start)
	if err != nil {
		return nil, err
	}

	visited := make(map[object.Hash]bool)
	var queue []object.Hash
	for _, p := range c.Parents {
		queue = append(queue, p)
	}

	for len(queue) > 0 {
		curr := queue[0]
		queue = queue[1:]

		if visited[curr] {
			continue
		}
		visited[curr] = true

		commitObj, err := r.ReadCommit(curr)
		if err != nil {
			continue
		}

		for _, p := range commitObj.Parents {
			if !visited[p] {
				queue = append(queue, p)
			}
		}
	}

	return visited, nil
}
