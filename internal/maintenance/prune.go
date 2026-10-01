package maintenance

import (
	"bufio"
	"fmt"
	"gogit/internal/index"
	"gogit/internal/object"
	"gogit/internal/repo"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// CollectReachableObjects 递归遍历所有根引用、HEAD、索引及 reflog，收集所有可达对象的哈希集合。
func CollectReachableObjects(r *repo.Repository) (map[object.Hash]bool, error) {
	reachable := make(map[object.Hash]bool)
	var queue []object.Hash

	enqueue := func(h object.Hash) {
		if h.IsZero() || reachable[h] {
			return
		}
		reachable[h] = true
		queue = append(queue, h)
	}

	// 1. 收集 HEAD 目标
	if headHash, err := r.Refs.ResolveHEAD(); err == nil {
		enqueue(headHash)
	}

	// 2. 收集所有分支、标签、远程分支、notes 和 replace 引用
	allRefs, err := r.Refs.ListRefs("")
	if err == nil {
		for _, h := range allRefs {
			enqueue(h)
		}
	}

	// 3. 收集暂存区索引中的所有条目哈希
	if _, err := os.Stat(r.IndexPath); err == nil {
		idx, err := index.ReadIndex(r.IndexPath)
		if err == nil {
			for _, entry := range idx.Entries {
				enqueue(entry.OID)
			}
		}
	}

	// 4. 收集 reflogs 中的所有历史提交哈希
	logsDir := filepath.Join(r.GitDir, "logs")
	if _, err := os.Stat(logsDir); err == nil {
		_ = filepath.Walk(logsDir, func(path string, info os.FileInfo, err error) error {
			if err != nil || info.IsDir() {
				return nil
			}
			f, openErr := os.Open(path)
			if openErr != nil {
				return nil
			}
			defer f.Close()

			scanner := bufio.NewScanner(f)
			for scanner.Scan() {
				fields := strings.Fields(scanner.Text())
				if len(fields) >= 2 {
					if oldH, err := object.NewHashFromHex(fields[0]); err == nil {
						enqueue(oldH)
					}
					if newH, err := object.NewHashFromHex(fields[1]); err == nil {
						enqueue(newH)
					}
				}
			}
			return nil
		})
	}

	// 5. 广度优先遍历有向无环图（DAG），追踪提交、树对象及标签
	for len(queue) > 0 {
		curr := queue[0]
		queue = queue[1:]

		raw, err := r.ReadObject(curr)
		if err != nil {
			// 若对象在远端或已被打包但缺失，继续遍历其他可达节点
			continue
		}

		switch raw.ObjType {
		case object.TypeCommit:
			commit, err := object.ParseCommit(raw.Payload())
			if err == nil {
				enqueue(commit.Tree)
				for _, parent := range commit.Parents {
					enqueue(parent)
				}
			}
		case object.TypeTree:
			tree, err := object.ParseTree(raw.Payload())
			if err == nil {
				for _, entry := range tree.Entries {
					enqueue(entry.OID)
				}
			}
		case object.TypeTag:
			tag, err := object.ParseTag(raw.Payload())
			if err == nil {
				enqueue(tag.Object)
			}
		case object.TypeBlob:
			// 叶子节点，无需继续追踪
		}
	}

	return reachable, nil
}

// PruneLooseObjects 清理未被任何引用引用的松散对象。
// expire 指定过期时间阈值，早于该修改时间的对象才会被清理；若 expire 为零值或未来时间，则全部不可达松散对象立即清理。
func PruneLooseObjects(r *repo.Repository, expire time.Time, dryRun bool) (int, int64, error) {
	reachable, err := CollectReachableObjects(r)
	if err != nil {
		return 0, 0, fmt.Errorf("收集可达对象失败: %w", err)
	}

	deletedCount := 0
	var freedBytes int64

	// 遍历 objects/ 目录下的 2 字符 16 进制子目录
	entries, err := os.ReadDir(r.ObjectsDir)
	if err != nil {
		return 0, 0, err
	}

	for _, d := range entries {
		if !d.IsDir() || len(d.Name()) != 2 {
			continue
		}
		prefix := d.Name()
		subDir := filepath.Join(r.ObjectsDir, prefix)
		files, err := os.ReadDir(subDir)
		if err != nil {
			continue
		}

		for _, f := range files {
			if f.IsDir() || len(f.Name()) != 38 {
				continue
			}
			fullHex := prefix + f.Name()
			h, err := object.NewHashFromHex(fullHex)
			if err != nil {
				continue
			}

			// 如果可达，安全保留
			if reachable[h] {
				continue
			}

			fullPath := filepath.Join(subDir, f.Name())
			fi, err := os.Stat(fullPath)
			if err != nil {
				continue
			}

			// 检查过期时间
			if !expire.IsZero() && fi.ModTime().After(expire) {
				continue
			}

			freedBytes += fi.Size()
			deletedCount++

			if !dryRun {
				_ = os.Remove(fullPath)
			}
		}

		// 如果子目录已空，清理该目录
		if !dryRun {
			rem, _ := os.ReadDir(subDir)
			if len(rem) == 0 {
				_ = os.Remove(subDir)
			}
		}
	}

	return deletedCount, freedBytes, nil
}
