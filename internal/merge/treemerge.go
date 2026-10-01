package merge

import (
	"fmt"
	"gogit/internal/index"
	"gogit/internal/object"
	"gogit/internal/repo"
	"os"
	"path/filepath"
	"sort"
)

// MergeConflictType 记录冲突类型
type MergeConflictType string

const (
	ConflictContent      MergeConflictType = "content"
	ConflictModifyDelete MergeConflictType = "modify/delete"
	ConflictDeleteModify MergeConflictType = "delete/modify"
	ConflictAddAdd       MergeConflictType = "both added"
)

// MergeConflict 记录单条冲突详情
type MergeConflict struct {
	Path string
	Type MergeConflictType
}

// TreeMergeResult 树级三路合并的结果
type TreeMergeResult struct {
	NewIndex  *index.Index
	Conflicts []MergeConflict
}

// MergeTrees 对 base, ours, theirs 三棵树执行三路合并。
// 自动更新工作区文件并在新 index 中写入 stage 0 (无冲突) 或 stage 1/2/3 (有冲突)。
func MergeTrees(r *repo.Repository, baseTree, oursTree, theirsTree object.Hash, oursLabel, theirsLabel string) (*TreeMergeResult, error) {
	baseFiles, err := flattenTree(r, baseTree)
	if err != nil {
		return nil, fmt.Errorf("读取 base 树失败: %w", err)
	}

	oursFiles, err := flattenTree(r, oursTree)
	if err != nil {
		return nil, fmt.Errorf("读取 ours 树失败: %w", err)
	}

	theirsFiles, err := flattenTree(r, theirsTree)
	if err != nil {
		return nil, fmt.Errorf("读取 theirs 树失败: %w", err)
	}

	// 收集三棵树涉及的所有唯一文件路径
	allPathsMap := make(map[string]bool)
	for p := range baseFiles {
		allPathsMap[p] = true
	}
	for p := range oursFiles {
		allPathsMap[p] = true
	}
	for p := range theirsFiles {
		allPathsMap[p] = true
	}

	allPaths := make([]string, 0, len(allPathsMap))
	for p := range allPathsMap {
		allPaths = append(allPaths, p)
	}
	sort.Strings(allPaths)

	newIdx := index.NewIndex()
	var conflicts []MergeConflict

	for _, path := range allPaths {
		b, hasB := baseFiles[path]
		o, hasO := oursFiles[path]
		t, hasT := theirsFiles[path]

		absPath := filepath.Join(r.WorkTree, path)

		// Case 1: 两端均未修改
		if hasB && hasO && hasT && b.OID == o.OID && b.OID == t.OID && b.Mode == o.Mode && b.Mode == t.Mode {
			addEntry(newIdx, path, o.OID, o.Mode, 0)
			continue
		}

		// Case 2: 仅 theirs 修改，ours 保持 base
		if (!hasB && !hasO) || (hasB && hasO && b.OID == o.OID && b.Mode == o.Mode) {
			if hasT {
				// 采纳 theirs
				if err := writeBlobToWorkTree(r, absPath, t.OID, t.Mode); err != nil {
					return nil, err
				}
				addEntry(newIdx, path, t.OID, t.Mode, 0)
			} else {
				// theirs 删除了该文件
				_ = os.Remove(absPath)
			}
			continue
		}

		// Case 3: 仅 ours 修改，theirs 保持 base
		if (!hasB && !hasT) || (hasB && hasT && b.OID == t.OID && b.Mode == t.Mode) {
			if hasO {
				// 保持 ours
				if err := writeBlobToWorkTree(r, absPath, o.OID, o.Mode); err != nil {
					return nil, err
				}
				addEntry(newIdx, path, o.OID, o.Mode, 0)
			} else {
				// ours 删除了该文件
				_ = os.Remove(absPath)
			}
			continue
		}

		// Case 4: 两端均修改
		if !hasO && !hasT {
			// 两端均删除，干净合并
			_ = os.Remove(absPath)
			continue
		}

		if hasO && hasT && o.OID == t.OID && o.Mode == t.Mode {
			// 两端改动完全相同
			if err := writeBlobToWorkTree(r, absPath, o.OID, o.Mode); err != nil {
				return nil, err
			}
			addEntry(newIdx, path, o.OID, o.Mode, 0)
			continue
		}

		// Modify/Delete 冲突
		if hasO && !hasT {
			conflicts = append(conflicts, MergeConflict{Path: path, Type: ConflictModifyDelete})
			if hasB {
				addEntry(newIdx, path, b.OID, b.Mode, 1)
			}
			addEntry(newIdx, path, o.OID, o.Mode, 2)
			continue
		}

		// Delete/Modify 冲突
		if !hasO && hasT {
			conflicts = append(conflicts, MergeConflict{Path: path, Type: ConflictDeleteModify})
			if hasB {
				addEntry(newIdx, path, b.OID, b.Mode, 1)
			}
			addEntry(newIdx, path, t.OID, t.Mode, 3)
			if err := writeBlobToWorkTree(r, absPath, t.OID, t.Mode); err != nil {
				return nil, err
			}
			continue
		}

		// 两端均有内容但内容不同：执行文本三路合并
		var baseContent, oursContent, theirsContent []byte
		if hasB {
			raw, _ := r.ReadObject(b.OID)
			if raw != nil {
				baseContent = raw.Content
			}
		}
		if hasO {
			raw, _ := r.ReadObject(o.OID)
			if raw != nil {
				oursContent = raw.Content
			}
		}
		if hasT {
			raw, _ := r.ReadObject(t.OID)
			if raw != nil {
				theirsContent = raw.Content
			}
		}

		mRes := Merge3Way(baseContent, oursContent, theirsContent, oursLabel, theirsLabel)
		if mRes.HasConflict {
			// 写入带冲突标记的工作区文件
			_ = os.MkdirAll(filepath.Dir(absPath), 0755)
			_ = os.WriteFile(absPath, mRes.Content, 0644)

			confType := ConflictContent
			if !hasB {
				confType = ConflictAddAdd
			}
			conflicts = append(conflicts, MergeConflict{Path: path, Type: confType})

			// 写入 stage 1 (base), 2 (ours), 3 (theirs)
			if hasB {
				addEntry(newIdx, path, b.OID, b.Mode, 1)
			}
			addEntry(newIdx, path, o.OID, o.Mode, 2)
			addEntry(newIdx, path, t.OID, t.Mode, 3)
		} else {
			// 无冲突三路合并，写入新 blob
			newOID, err := r.WriteBlob(mRes.Content)
			if err != nil {
				return nil, fmt.Errorf("写入合并 blob 失败: %w", err)
			}
			_ = os.MkdirAll(filepath.Dir(absPath), 0755)
			fileMode := os.FileMode(0644)
			if o.Mode == object.ModeExec || t.Mode == object.ModeExec {
				fileMode = 0755
			}
			_ = os.WriteFile(absPath, mRes.Content, fileMode)

			mode := o.Mode
			if t.Mode == object.ModeExec {
				mode = object.ModeExec
			}
			addEntry(newIdx, path, newOID, mode, 0)
		}
	}

	return &TreeMergeResult{
		NewIndex:  newIdx,
		Conflicts: conflicts,
	}, nil
}

func addEntry(idx *index.Index, path string, oid object.Hash, mode object.FileMode, stage int) {
	entry := &index.IndexEntry{
		Path: path,
		OID:  oid,
		Mode: uint32(mode),
	}
	entry.SetStage(stage)
	idx.AddOrReplaceEntry(entry)
}

func writeBlobToWorkTree(r *repo.Repository, absPath string, oid object.Hash, mode object.FileMode) error {
	raw, err := r.ReadObject(oid)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(absPath), 0755); err != nil {
		return err
	}
	fileMode := os.FileMode(0644)
	if mode == object.ModeExec {
		fileMode = 0755
	}
	return os.WriteFile(absPath, raw.Content, fileMode)
}

func flattenTree(r *repo.Repository, treeHash object.Hash) (map[string]object.TreeEntry, error) {
	result := make(map[string]object.TreeEntry)
	if treeHash.IsZero() {
		return result, nil
	}
	if err := flattenTreeRec(r, treeHash, "", result); err != nil {
		return nil, err
	}
	return result, nil
}

func flattenTreeRec(r *repo.Repository, treeHash object.Hash, prefix string, result map[string]object.TreeEntry) error {
	tree, err := r.ReadTree(treeHash)
	if err != nil {
		return err
	}

	for _, entry := range tree.Entries {
		path := entry.Name
		if prefix != "" {
			path = prefix + "/" + entry.Name
		}

		if entry.Mode == object.ModeDirectory {
			if err := flattenTreeRec(r, entry.OID, path, result); err != nil {
				return err
			}
		} else {
			result[path] = entry
		}
	}

	return nil
}
