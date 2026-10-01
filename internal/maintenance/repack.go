package maintenance

import (
	"fmt"
	"gogit/internal/object"
	"gogit/internal/pack"
	"gogit/internal/repo"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// RepackOptions repack 配置选项
type RepackOptions struct {
	All             bool // 是否重新打包所有对象（包括已存在于旧 pack 中的对象）
	DeleteRedundant bool // 是否删除已被打入 pack 的松散对象及旧 pack 文件 (-d)
}

// Repack 将仓库中的松散对象及旧 packfile 对象重新打包合并为单个 pack 和 idx。
func Repack(r *repo.Repository, opts RepackOptions) (*object.Hash, int, error) {
	packDir := filepath.Join(r.ObjectsDir, "pack")
	if err := os.MkdirAll(packDir, 0755); err != nil {
		return nil, 0, err
	}

	oidsMap := make(map[object.Hash]bool)

	// 1. 收集松散对象
	loose, err := ListLooseObjects(r)
	if err == nil {
		for _, h := range loose {
			oidsMap[h] = true
		}
	}

	// 2. 如果开启 -a (--all)，收集所有现有 pack 中的对象及 DAG 可达对象
	var oldPacks []string
	if opts.All {
		files, _ := os.ReadDir(packDir)
		for _, f := range files {
			if strings.HasSuffix(f.Name(), ".idx") {
				base := strings.TrimSuffix(f.Name(), ".idx")
				oldPacks = append(oldPacks, base)
				idxPath := filepath.Join(packDir, f.Name())
				file, err := os.Open(idxPath)
				if err == nil {
					idx, err := pack.ParseIndexV2(file)
					_ = file.Close()
					if err == nil {
						for _, oid := range idx.OIDs {
							oidsMap[oid] = true
						}
					}
				}
			}
		}

		// 同时补充所有从引用可达的对象
		reachable, err := CollectReachableObjects(r)
		if err == nil {
			for h := range reachable {
				oidsMap[h] = true
			}
		}
	}

	if len(oidsMap) == 0 {
		return nil, 0, nil
	}

	// 3. 读取待打包对象的原始数据
	var packables []pack.PackableObject
	for oid := range oidsMap {
		raw, err := r.ReadObject(oid)
		if err != nil {
			continue
		}
		packables = append(packables, pack.PackableObject{
			OID:     oid,
			Type:    raw.ObjType,
			Content: raw.Payload(),
		})
	}

	if len(packables) == 0 {
		return nil, 0, nil
	}

	// 4. 构建 pack 与 idx 数据流（内部包含滑动窗口增量压缩）
	packBytes, idxBytes, checksum, err := pack.BuildPack(packables)
	if err != nil {
		return nil, 0, fmt.Errorf("构建 packfile 失败: %w", err)
	}

	// 5. 写入 pack 目标文件与 idx 文件
	packName := fmt.Sprintf("pack-%s.pack", checksum.String())
	idxName := fmt.Sprintf("pack-%s.idx", checksum.String())
	packPath := filepath.Join(packDir, packName)
	idxPath := filepath.Join(packDir, idxName)

	writeAtomic := func(dest string, data []byte) error {
		tmp := fmt.Sprintf("%s.tmp.%d", dest, time.Now().UnixNano())
		if err := os.WriteFile(tmp, data, 0444); err != nil {
			return err
		}
		return os.Rename(tmp, dest)
	}

	if err := writeAtomic(packPath, packBytes); err != nil {
		return nil, 0, fmt.Errorf("写入 %s 失败: %w", packName, err)
	}
	if err := writeAtomic(idxPath, idxBytes); err != nil {
		return nil, 0, fmt.Errorf("写入 %s 失败: %w", idxName, err)
	}

	// 6. 若配置了清理冗余 (-d)
	if opts.DeleteRedundant {
		// 删除被打入 pack 的松散文件
		for _, p := range packables {
			hStr := p.OID.String()
			loosePath := filepath.Join(r.ObjectsDir, hStr[:2], hStr[2:])
			_ = os.Remove(loosePath)
		}

		// 若开启了 -a，删除已被完全取代的旧 pack 与 idx
		if opts.All {
			newBase := fmt.Sprintf("pack-%s", checksum.String())
			for _, oldBase := range oldPacks {
				if oldBase != newBase {
					_ = os.Remove(filepath.Join(packDir, oldBase+".pack"))
					_ = os.Remove(filepath.Join(packDir, oldBase+".idx"))
				}
			}
		}
	}

	return &checksum, len(packables), nil
}

// ListLooseObjects 列出当前仓库 objects/ 目录下的所有松散对象哈希。
func ListLooseObjects(r *repo.Repository) ([]object.Hash, error) {
	var res []object.Hash
	entries, err := os.ReadDir(r.ObjectsDir)
	if err != nil {
		return nil, err
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
			h, err := object.NewHashFromHex(prefix + f.Name())
			if err == nil {
				res = append(res, h)
			}
		}
	}
	return res, nil
}
