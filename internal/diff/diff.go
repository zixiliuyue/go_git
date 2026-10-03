package diff

import (
	"bytes"
	"gogit/internal/index"
	"gogit/internal/object"
	"gogit/internal/repo"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// FileDiffStatus 描述文件在版本差异中的变更状态
type FileDiffStatus byte

const (
	StatusModified FileDiffStatus = 'M'
	StatusAdded    FileDiffStatus = 'A'
	StatusDeleted  FileDiffStatus = 'D'
	StatusRenamed  FileDiffStatus = 'R'
	StatusCopied   FileDiffStatus = 'C'
)

// FileDiff 保存单个文件的差异详情
type FileDiff struct {
	OldPath      string
	NewPath      string
	OldMode      uint32
	NewMode      uint32
	OldOID       object.Hash
	NewOID       object.Hash
	Status       FileDiffStatus
	Similarity   int // 相似度百分比 (0-100)
	Hunks        []Hunk
	AddedLines   int
	DeletedLines int
	IsBinary     bool
}

// DiffOptions 控制 diff 计算与格式化的全部选项
type DiffOptions struct {
	Cached          bool
	Stat            bool
	NumStat         bool
	NameStatus      bool
	NameOnly        bool
	DetectRenames   bool
	RenameThreshold int // 相似度阈值 (默认为 50)
	DiffFilter      string
	ContextLines    int
}

// DiffWorktreeWithIndex 比较工作区文件与暂存区索引的差异
func DiffWorktreeWithIndex(r *repo.Repository, idx *index.Index, opts DiffOptions) ([]*FileDiff, error) {
	if opts.RenameThreshold == 0 {
		opts.RenameThreshold = 50
	}
	if opts.ContextLines == 0 {
		opts.ContextLines = 3
	}

	var diffs []*FileDiff

	for _, entry := range idx.Entries {
		// 跳过稀疏检出隐藏文件与稀疏目录折叠条目
		if entry.IsSkipWorktree() || entry.IsSparseDirectory() {
			continue
		}

		absPath := filepath.Join(r.WorkTree, entry.Path)
		fi, err := os.Lstat(absPath)
		if err != nil {
			if os.IsNotExist(err) {
				// 文件在工作区被删除
				rawObj, err := r.ReadObject(entry.OID)
				var oldLines []string
				if err == nil {
					oldLines = splitLines(string(rawObj.Content))
				}
				hunks := GenerateUnifiedHunks(MyersDiff(oldLines, nil), opts.ContextLines)
				diffs = append(diffs, &FileDiff{
					OldPath:      entry.Path,
					NewPath:      entry.Path,
					OldMode:      entry.Mode,
					OldOID:       entry.OID,
					Status:       StatusDeleted,
					Hunks:        hunks,
					DeletedLines: len(oldLines),
				})
			}
			continue
		}

		// 检查 stat 缓存是否完全命中
		if entry.MatchStat(fi) {
			continue
		}

		// 读取工作区内容
		var worktreeData []byte
		if fi.Mode()&os.ModeSymlink != 0 {
			target, _ := os.Readlink(absPath)
			worktreeData = []byte(target)
		} else {
			worktreeData, _ = os.ReadFile(absPath)
		}

		// 检查 SHA-1 是否变更
		currOID := object.HashObject(object.TypeBlob, worktreeData)
		if currOID == entry.OID {
			continue
		}

		rawObj, err := r.ReadObject(entry.OID)
		var oldContent []byte
		if err == nil {
			oldContent = rawObj.Content
		}

		fd := computeFileDiff(entry.Path, entry.Path, entry.Mode, uint32(fi.Mode().Perm()), entry.OID, currOID, oldContent, worktreeData, opts.ContextLines)
		diffs = append(diffs, fd)
	}

	return filterDiffs(diffs, opts.DiffFilter), nil
}

// DiffIndexWithTree 比较暂存区索引与树对象（通常为 HEAD）的差异
func DiffIndexWithTree(r *repo.Repository, idx *index.Index, treeHash object.Hash, opts DiffOptions) ([]*FileDiff, error) {
	if opts.RenameThreshold == 0 {
		opts.RenameThreshold = 50
	}
	if opts.ContextLines == 0 {
		opts.ContextLines = 3
	}

	// 收集索引中的稀疏目录及其预计算 Tree OID
	sparseDirs := make(map[string]object.Hash)
	indexEntries := make(map[string]*index.IndexEntry)
	for _, e := range idx.Entries {
		if e.IsSparseDirectory() {
			sparseDirs[e.Path] = e.OID
		} else {
			indexEntries[e.Path] = e
		}
	}

	treeEntries := make(map[string]object.TreeEntry)
	if !treeHash.IsZero() {
		flattenTree(r, treeHash, "", treeEntries, sparseDirs)
	}

	var allPaths []string
	pathSet := make(map[string]bool)
	for p := range treeEntries {
		if !pathSet[p] {
			pathSet[p] = true
			allPaths = append(allPaths, p)
		}
	}
	for p := range indexEntries {
		if !pathSet[p] {
			pathSet[p] = true
			allPaths = append(allPaths, p)
		}
	}
	sort.Strings(allPaths)

	var diffs []*FileDiff
	for _, p := range allPaths {
		te, inTree := treeEntries[p]
		ie, inIndex := indexEntries[p]

		if inTree && !inIndex {
			// 在 index 中被删除
			raw, _ := r.ReadObject(te.OID)
			var content []byte
			if raw != nil {
				content = raw.Content
			}
			lines := splitLines(string(content))
			hunks := GenerateUnifiedHunks(MyersDiff(lines, nil), opts.ContextLines)
			diffs = append(diffs, &FileDiff{
				OldPath:      p,
				NewPath:      p,
				OldMode:      uint32(te.Mode),
				OldOID:       te.OID,
				Status:       StatusDeleted,
				Hunks:        hunks,
				DeletedLines: len(lines),
			})
		} else if !inTree && inIndex {
			// 在 index 中新增
			raw, _ := r.ReadObject(ie.OID)
			var content []byte
			if raw != nil {
				content = raw.Content
			}
			lines := splitLines(string(content))
			hunks := GenerateUnifiedHunks(MyersDiff(nil, lines), opts.ContextLines)
			diffs = append(diffs, &FileDiff{
				OldPath:    p,
				NewPath:    p,
				NewMode:    ie.Mode,
				NewOID:     ie.OID,
				Status:     StatusAdded,
				Hunks:      hunks,
				AddedLines: len(lines),
			})
		} else if inTree && inIndex {
			if te.OID != ie.OID || uint32(te.Mode) != ie.Mode {
				rawOld, _ := r.ReadObject(te.OID)
				rawNew, _ := r.ReadObject(ie.OID)
				var oldBytes, newBytes []byte
				if rawOld != nil {
					oldBytes = rawOld.Content
				}
				if rawNew != nil {
					newBytes = rawNew.Content
				}
				fd := computeFileDiff(p, p, uint32(te.Mode), ie.Mode, te.OID, ie.OID, oldBytes, newBytes, opts.ContextLines)
				diffs = append(diffs, fd)
			}
		}
	}

	if opts.DetectRenames {
		diffs = detectRenames(r, diffs, opts.RenameThreshold)
	}

	return filterDiffs(diffs, opts.DiffFilter), nil
}

func computeFileDiff(oldPath, newPath string, oldMode, newMode uint32, oldOID, newOID object.Hash, oldBytes, newBytes []byte, contextLen int) *FileDiff {
	isBin := isBinary(oldBytes) || isBinary(newBytes)
	fd := &FileDiff{
		OldPath:  oldPath,
		NewPath:  newPath,
		OldMode:  oldMode,
		NewMode:  newMode,
		OldOID:   oldOID,
		NewOID:   newOID,
		Status:   StatusModified,
		IsBinary: isBin,
	}

	if !isBin {
		oldLines := splitLines(string(oldBytes))
		newLines := splitLines(string(newBytes))
		ops := MyersDiff(oldLines, newLines)
		fd.Hunks = GenerateUnifiedHunks(ops, contextLen)

		for _, op := range ops {
			if op.Type == OpInsert {
				fd.AddedLines++
			} else if op.Type == OpDelete {
				fd.DeletedLines++
			}
		}
	}

	return fd
}

func flattenTree(r *repo.Repository, treeHash object.Hash, prefix string, result map[string]object.TreeEntry, sparseDirs map[string]object.Hash) {
	t, err := r.ReadTree(treeHash)
	if err != nil {
		return
	}
	for _, e := range t.Entries {
		entryPath := filepath.Join(prefix, e.Name)
		entryPath = filepath.ToSlash(entryPath)
		if e.Mode == object.ModeDirectory {
			// 若当前子目录在索引中被稀疏折叠，且 Tree OID 完全一致，则该目录下无任何暂存变更，直接跳过递归展开
			if sparseDirs != nil {
				if expectedOID, isSparse := sparseDirs[entryPath+"/"]; isSparse && expectedOID == e.OID {
					continue
				}
			}
			flattenTree(r, e.OID, entryPath, result, sparseDirs)
		} else {
			result[entryPath] = e
		}
	}
}

func isBinary(data []byte) bool {
	// Git 规范：前 8000 字节内出现 null byte (\0) 即视作二进制文件
	limit := len(data)
	if limit > 8000 {
		limit = 8000
	}
	return bytes.IndexByte(data[:limit], 0) >= 0
}

func splitLines(s string) []string {
	if len(s) == 0 {
		return nil
	}
	lines := strings.Split(s, "\n")
	if len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	return lines
}

func detectRenames(r *repo.Repository, diffs []*FileDiff, threshold int) []*FileDiff {
	if threshold <= 0 {
		threshold = 50 // Git 默认 -M50% 相似度阈值
	}
	var deletes []*FileDiff
	var adds []*FileDiff
	var others []*FileDiff

	for _, d := range diffs {
		if d.Status == StatusDeleted {
			deletes = append(deletes, d)
		} else if d.Status == StatusAdded {
			adds = append(adds, d)
		} else {
			others = append(others, d)
		}
	}

	matchedAdds := make(map[int]bool)
	var finalRenames []*FileDiff

	for _, del := range deletes {
		bestSim := -1
		bestAddIdx := -1

		for j, add := range adds {
			if matchedAdds[j] {
				continue
			}
			sim := calculateSimilarity(r, del, add)
			if sim >= threshold && sim > bestSim {
				bestSim = sim
				bestAddIdx = j
			}
		}

		if bestAddIdx >= 0 {
			matchedAdds[bestAddIdx] = true
			add := adds[bestAddIdx]
			del.Status = StatusRenamed
			del.NewPath = add.NewPath
			del.NewMode = add.NewMode
			del.NewOID = add.NewOID
			del.Similarity = bestSim
			finalRenames = append(finalRenames, del)
		} else {
			finalRenames = append(finalRenames, del)
		}
	}

	for j, add := range adds {
		if !matchedAdds[j] {
			finalRenames = append(finalRenames, add)
		}
	}

	return append(others, finalRenames...)
}

// calculateSimilarity 计算删除与新增文件之间的相似度百分比 (0~100)
func calculateSimilarity(r *repo.Repository, del, add *FileDiff) int {
	if del.OldOID == add.NewOID {
		return 100
	}
	if r == nil {
		return 0
	}
	rawDel, err1 := r.ReadObject(del.OldOID)
	rawAdd, err2 := r.ReadObject(add.NewOID)
	if err1 != nil || err2 != nil || rawDel == nil || rawAdd == nil {
		return 0
	}

	contentDel := rawDel.Content
	contentAdd := rawAdd.Content

	if len(contentDel) == 0 && len(contentAdd) == 0 {
		return 100
	}
	if len(contentDel) == 0 || len(contentAdd) == 0 {
		return 0
	}

	if isBinary(contentDel) || isBinary(contentAdd) {
		if bytes.Equal(contentDel, contentAdd) {
			return 100
		}
		return 0
	}

	delLines := splitLines(string(contentDel))
	addLines := splitLines(string(contentAdd))
	if len(delLines) == 0 && len(addLines) == 0 {
		return 100
	}
	if len(delLines) == 0 || len(addLines) == 0 {
		return 0
	}

	freqDel := make(map[string]int)
	for _, l := range delLines {
		freqDel[l]++
	}
	common := 0
	for _, l := range addLines {
		if freqDel[l] > 0 {
			common++
			freqDel[l]--
		}
	}

	// 相似度公式：2 * common / (total_del + total_add) * 100
	return (common * 200) / (len(delLines) + len(addLines))
}

func filterDiffs(diffs []*FileDiff, filter string) []*FileDiff {
	if filter == "" {
		return diffs
	}
	var res []*FileDiff
	for _, d := range diffs {
		if strings.ContainsRune(filter, rune(d.Status)) {
			res = append(res, d)
		}
	}
	return res
}
