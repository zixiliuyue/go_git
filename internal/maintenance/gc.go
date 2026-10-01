package maintenance

import (
	"fmt"
	"gogit/internal/object"
	"gogit/internal/repo"
	"time"
)

// GCOptions 控制垃圾回收与仓库优化的参数
type GCOptions struct {
	Aggressive bool          // 是否激进重打包
	Prune      bool          // 是否清理过期不可达对象
	PruneAfter time.Duration // 默认 14 天 (336 小时)
}

// GCResult 记录 gc 过程的操作统计
type GCResult struct {
	PackedRefs   bool
	PackChecksum *object.Hash
	ObjectsCount int
	PrunedCount  int
	FreedBytes   int64
}

// RunGC 执行完整的仓库垃圾回收维护流程：
// 1. 打包松散引用到 packed-refs (pack-refs)
// 2. 将全库对象压缩重打包为单个 packfile (repack -a -d)
// 3. 清理不可达松散对象 (prune)
func RunGC(r *repo.Repository, opts GCOptions) (*GCResult, error) {
	res := &GCResult{}

	// 1. 打包引用
	// 为附注标签提取剥离哈希
	peeler := func(h object.Hash) (object.Hash, bool) {
		raw, err := r.ReadObject(h)
		if err == nil && raw.ObjType == object.TypeTag {
			tag, err := object.ParseTag(raw.Payload())
			if err == nil {
				return tag.Object, true
			}
		}
		return object.ZeroHash, false
	}

	if err := r.Refs.PackRefsWithPeeler(true, true, peeler); err == nil {
		res.PackedRefs = true
	}

	// 2. 重打包全库对象为单个 packfile
	chk, count, err := Repack(r, RepackOptions{
		All:             true,
		DeleteRedundant: true,
	})
	if err != nil {
		return nil, fmt.Errorf("repack 失败: %w", err)
	}
	res.PackChecksum = chk
	res.ObjectsCount = count

	// 3. 清理过期不可达松散对象
	if opts.Prune {
		var expireThreshold time.Time
		if opts.PruneAfter > 0 {
			expireThreshold = time.Now().Add(-opts.PruneAfter)
		}
		delCount, freed, err := PruneLooseObjects(r, expireThreshold, false)
		if err == nil {
			res.PrunedCount = delCount
			res.FreedBytes = freed
		}
	}

	return res, nil
}
