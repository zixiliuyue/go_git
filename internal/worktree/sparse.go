package worktree

import (
	"bufio"
	"gogit/internal/index"
	"gogit/internal/object"
	"gogit/internal/repo"
	"os"
	"path/filepath"
	"strings"
)

// SparseCheckoutConfig 管理稀疏检出状态
type SparseCheckoutConfig struct {
	Patterns    []string
	Enabled     bool
	Cone        bool
	SparseIndex bool
}

// ReadSparseCheckout 读取当前仓库的稀疏检出规则
func ReadSparseCheckout(r *repo.Repository) (*SparseCheckoutConfig, error) {
	sparsePath := filepath.Join(r.GitDir, "info", "sparse-checkout")
	cfg := &SparseCheckoutConfig{}

	enabled := r.Config.Get("core", "sparsecheckout") == "true"
	cfg.Enabled = enabled
	cfg.Cone = r.Config.Get("core", "sparsecheckoutcone") == "true"
	cfg.SparseIndex = r.Config.Get("index", "sparse") == "true"

	f, err := os.Open(sparsePath)
	if err != nil {
		if os.IsNotExist(err) {
			return cfg, nil
		}
		return nil, err
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line != "" && !strings.HasPrefix(line, "#") {
			cfg.Patterns = append(cfg.Patterns, line)
		}
	}
	return cfg, nil
}

// InitSparseCheckout 初始化稀疏检出配置（支持 --cone 与 --sparse-index）
func InitSparseCheckout(r *repo.Repository, cone bool, sparseIndex bool) error {
	r.Config.Set("core", "sparsecheckout", "true")
	if cone {
		r.Config.Set("core", "sparsecheckoutcone", "true")
	}
	if sparseIndex {
		r.Config.Set("index", "sparse", "true")
	}
	_ = os.WriteFile(filepath.Join(r.GitDir, "config"), r.Config.Serialize(), 0644)

	sparsePath := filepath.Join(r.GitDir, "info", "sparse-checkout")
	_ = os.MkdirAll(filepath.Dir(sparsePath), 0755)

	initialContent := "/*\n!/*/\n"
	if cone {
		initialContent = "/*\n"
	}
	return os.WriteFile(sparsePath, []byte(initialContent), 0644)
}

// SetSparseCheckout 设置稀疏检出目录规则并立即应用到工作区和暂存区
func SetSparseCheckout(r *repo.Repository, paths []string, sparseIndex bool) error {
	if sparseIndex {
		r.Config.Set("index", "sparse", "true")
		_ = os.WriteFile(filepath.Join(r.GitDir, "config"), r.Config.Serialize(), 0644)
	}

	cone := r.Config.Get("core", "sparsecheckoutcone") == "true"
	sparsePath := filepath.Join(r.GitDir, "info", "sparse-checkout")
	_ = os.MkdirAll(filepath.Dir(sparsePath), 0755)

	var sb strings.Builder
	if cone {
		sb.WriteString("/*\n!/*/\n")
		for _, p := range paths {
			cleaned := strings.Trim(p, "/")
			if cleaned != "" {
				sb.WriteString("/" + cleaned + "/\n")
			}
		}
	} else {
		for _, p := range paths {
			cleaned := strings.Trim(p, "/")
			if cleaned != "" {
				sb.WriteString(cleaned + "/**\n")
			}
		}
	}

	if err := os.WriteFile(sparsePath, []byte(sb.String()), 0644); err != nil {
		return err
	}

	return ApplySparseCheckout(r)
}

// IsSparseIndexEnabled 检查是否开启了稀疏索引扩展（index.sparse = true）
func IsSparseIndexEnabled(r *repo.Repository) bool {
	return r.Config.Get("index", "sparse") == "true"
}

// CollapseToSparseIndex 将 cone 范围之外且全部标记为 skip-worktree 的子目录在 index 中折叠为稀疏目录项 (Mode 0040000, "dir/")
func CollapseToSparseIndex(r *repo.Repository, idx *index.Index) error {
	// 获取 HEAD 树，用于获取各子目录的 Tree OID
	var headTreeHash object.Hash
	headRef, err := r.Refs.ReadHEAD()
	if err == nil && !headRef.Hash.IsZero() {
		commit, err := r.ReadCommit(headRef.Hash)
		if err == nil {
			headTreeHash = commit.Tree
		}
	}

	type dirGroup struct {
		entries []*index.IndexEntry
		allSkip bool
	}
	groups := make(map[string]*dirGroup)
	var rootEntries []*index.IndexEntry

	for _, entry := range idx.Entries {
		if entry.IsSparseDirectory() {
			rootEntries = append(rootEntries, entry)
			continue
		}
		slashIdx := strings.IndexByte(entry.Path, '/')
		if slashIdx == -1 {
			rootEntries = append(rootEntries, entry)
			continue
		}
		topDir := entry.Path[:slashIdx]
		if _, ok := groups[topDir]; !ok {
			groups[topDir] = &dirGroup{allSkip: true}
		}
		g := groups[topDir]
		g.entries = append(g.entries, entry)
		if !entry.IsSkipWorktree() {
			g.allSkip = false
		}
	}

	// 读取 HEAD 树下的一级子树 OID
	headSubtrees := make(map[string]object.Hash)
	if !headTreeHash.IsZero() {
		headTree, err := r.ReadTree(headTreeHash)
		if err == nil {
			for _, te := range headTree.Entries {
				if te.Mode == object.ModeDirectory {
					headSubtrees[te.Name] = te.OID
				}
			}
		}
	}

	for topDir, g := range groups {
		if g.allSkip {
			treeOID, hasTree := headSubtrees[topDir]
			if !hasTree {
				subIdx := index.NewIndex()
				prefix := topDir + "/"
				for _, e := range g.entries {
					subEntry := *e
					subEntry.Path = strings.TrimPrefix(e.Path, prefix)
					subIdx.AddOrReplaceEntry(&subEntry)
				}
				treeOID, _ = subIdx.WriteTree(r.ObjectsDir)
			}

			if !treeOID.IsZero() {
				sparseEntry := &index.IndexEntry{
					Mode: uint32(object.ModeDirectory),
					Path: topDir + "/",
					OID:  treeOID,
				}
				sparseEntry.SetSkipWorktree(true)
				rootEntries = append(rootEntries, sparseEntry)
				_ = os.RemoveAll(filepath.Join(r.WorkTree, topDir))
				continue
			}
		}
		rootEntries = append(rootEntries, g.entries...)
	}

	idx.Entries = rootEntries
	idx.Sort()

	hasSparseDir := false
	for _, e := range idx.Entries {
		if e.IsSparseDirectory() {
			hasSparseDir = true
			break
		}
	}

	if hasSparseDir {
		if idx.Version < 3 {
			idx.Version = 3
		}
		hasSdir := false
		for _, ext := range idx.Extensions {
			if ext.Signature == [4]byte{'s', 'd', 'i', 'r'} {
				hasSdir = true
				break
			}
		}
		if !hasSdir {
			idx.Extensions = append(idx.Extensions, index.IndexExtension{
				Signature: [4]byte{'s', 'd', 'i', 'r'},
				Data:      []byte{},
			})
		}
	}

	return nil
}

// ExpandSparseDirectory 将指定的稀疏目录条目展开为其下的所有具体文件条目
func ExpandSparseDirectory(r *repo.Repository, idx *index.Index, dirPrefix string) error {
	dirPrefix = strings.TrimSuffix(dirPrefix, "/") + "/"
	var newEntries []*index.IndexEntry
	found := false

	for _, entry := range idx.Entries {
		if entry.IsSparseDirectory() && entry.Path == dirPrefix {
			found = true
			expanded, err := expandTreeToEntries(r, entry.OID, dirPrefix)
			if err != nil {
				return err
			}
			newEntries = append(newEntries, expanded...)
		} else {
			newEntries = append(newEntries, entry)
		}
	}

	if !found {
		return nil
	}

	idx.Entries = newEntries
	idx.Sort()

	hasSparseDir := false
	for _, e := range idx.Entries {
		if e.IsSparseDirectory() {
			hasSparseDir = true
			break
		}
	}
	if !hasSparseDir {
		filteredExts := idx.Extensions[:0]
		for _, ext := range idx.Extensions {
			if ext.Signature != [4]byte{'s', 'd', 'i', 'r'} {
				filteredExts = append(filteredExts, ext)
			}
		}
		idx.Extensions = filteredExts
	}

	return nil
}

// expandTreeToEntries 递归展开 Tree 对象为 IndexEntry 列表（均标记为 skip-worktree）
func expandTreeToEntries(r *repo.Repository, treeHash object.Hash, prefix string) ([]*index.IndexEntry, error) {
	t, err := r.ReadTree(treeHash)
	if err != nil {
		return nil, err
	}

	var results []*index.IndexEntry
	for _, te := range t.Entries {
		curPath := prefix + te.Name
		if te.Mode == object.ModeDirectory {
			sub, err := expandTreeToEntries(r, te.OID, curPath+"/")
			if err != nil {
				return nil, err
			}
			results = append(results, sub...)
		} else {
			e := &index.IndexEntry{
				Mode: uint32(te.Mode),
				OID:  te.OID,
				Path: curPath,
			}
			e.SetSkipWorktree(true)
			results = append(results, e)
		}
	}
	return results, nil
}

// ExpandSparseIndex 将索引中所有的稀疏目录展开为标准平面文件索引
func ExpandSparseIndex(r *repo.Repository, idx *index.Index) error {
	var sparseDirs []string
	for _, e := range idx.Entries {
		if e.IsSparseDirectory() {
			sparseDirs = append(sparseDirs, e.Path)
		}
	}
	for _, d := range sparseDirs {
		if err := ExpandSparseDirectory(r, idx, d); err != nil {
			return err
		}
	}
	return nil
}

// ApplySparseCheckout 根据稀疏规则更新 index 中的 skip-worktree 标记并增删工作区文件
func ApplySparseCheckout(r *repo.Repository) error {
	cfg, err := ReadSparseCheckout(r)
	if err != nil || !cfg.Enabled {
		return nil
	}

	idx, err := index.ReadIndex(r.IndexPath)
	if err != nil {
		return nil
	}

	// 若处于稀疏索引状态，先全量展开以准确比对规则
	_ = ExpandSparseIndex(r, idx)

	matcher := NewIgnoreMatcher()
	matcher.ParseRules([]byte(strings.Join(cfg.Patterns, "\n")), "")

	var coneDirs []string
	if cfg.Cone {
		for _, p := range cfg.Patterns {
			trimmed := strings.Trim(p, "/")
			if trimmed != "" && trimmed != "*" && !strings.HasPrefix(trimmed, "!") {
				coneDirs = append(coneDirs, trimmed)
			}
		}
	}

	isMatch := func(p string) bool {
		if cfg.Cone {
			// Cone 模式：根目录文件默认检出
			if !strings.Contains(p, "/") {
				return true
			}
			for _, dir := range coneDirs {
				if p == dir || strings.HasPrefix(p, dir+"/") {
					return true
				}
			}
			return false
		}
		return matcher.Match(p, false)
	}

	for _, entry := range idx.Entries {
		match := isMatch(entry.Path)
		diskPath := filepath.Join(r.WorkTree, entry.Path)

		if match {
			// 符合稀疏规则，检出该文件
			entry.SetSkipWorktree(false)
			if _, err := os.Stat(diskPath); os.IsNotExist(err) {
				raw, err := r.ReadObject(entry.OID)
				if err == nil {
					_ = os.MkdirAll(filepath.Dir(diskPath), 0755)
					perm := os.FileMode(0644)
					if entry.Mode == uint32(object.ModeExec) {
						perm = 0755
					}
					_ = os.WriteFile(diskPath, raw.Payload(), perm)
				}
			}
		} else {
			// 不在规则范围内，设置 skip-worktree 并在磁盘上隐藏/删除该文件
			entry.SetSkipWorktree(true)
			_ = os.Remove(diskPath)
		}
	}

	// 若开启了稀疏索引扩展，折叠非 cone 目录
	if IsSparseIndexEnabled(r) {
		_ = CollapseToSparseIndex(r, idx)
	}

	return idx.WriteIndex(r.IndexPath)
}

// DisableSparseCheckout 禁用稀疏检出，恢复工作区所有文件
func DisableSparseCheckout(r *repo.Repository) error {
	r.Config.Set("core", "sparsecheckout", "false")
	_ = os.WriteFile(filepath.Join(r.GitDir, "config"), r.Config.Serialize(), 0644)

	idx, err := index.ReadIndex(r.IndexPath)
	if err != nil {
		return nil
	}

	// 展开所有稀疏目录
	_ = ExpandSparseIndex(r, idx)

	for _, entry := range idx.Entries {
		entry.SetSkipWorktree(false)
		diskPath := filepath.Join(r.WorkTree, entry.Path)
		if _, err := os.Stat(diskPath); os.IsNotExist(err) {
			raw, err := r.ReadObject(entry.OID)
			if err == nil {
				_ = os.MkdirAll(filepath.Dir(diskPath), 0755)
				perm := os.FileMode(0644)
				if entry.Mode == uint32(object.ModeExec) {
					perm = 0755
				}
				_ = os.WriteFile(diskPath, raw.Payload(), perm)
			}
		}
	}

	return idx.WriteIndex(r.IndexPath)
}
