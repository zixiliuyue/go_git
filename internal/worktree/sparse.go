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
	Patterns []string
	Enabled  bool
	Cone     bool
}

// ReadSparseCheckout 读取当前仓库的稀疏检出规则
func ReadSparseCheckout(r *repo.Repository) (*SparseCheckoutConfig, error) {
	sparsePath := filepath.Join(r.GitDir, "info", "sparse-checkout")
	cfg := &SparseCheckoutConfig{}

	enabled := r.Config.Get("core", "sparsecheckout") == "true"
	cfg.Enabled = enabled
	cfg.Cone = r.Config.Get("core", "sparsecheckoutcone") == "true"

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

// InitSparseCheckout 初始化稀疏检出配置
func InitSparseCheckout(r *repo.Repository, cone bool) error {
	r.Config.Set("core", "sparsecheckout", "true")
	if cone {
		r.Config.Set("core", "sparsecheckoutcone", "true")
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
func SetSparseCheckout(r *repo.Repository, paths []string) error {
	sparsePath := filepath.Join(r.GitDir, "info", "sparse-checkout")
	_ = os.MkdirAll(filepath.Dir(sparsePath), 0755)

	var sb strings.Builder
	for _, p := range paths {
		cleaned := strings.Trim(p, "/")
		if cleaned != "" {
			sb.WriteString(cleaned + "/**\n")
		}
	}

	if err := os.WriteFile(sparsePath, []byte(sb.String()), 0644); err != nil {
		return err
	}

	return ApplySparseCheckout(r)
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

	matcher := NewIgnoreMatcher()
	matcher.ParseRules([]byte(strings.Join(cfg.Patterns, "\n")), "")

	for _, entry := range idx.Entries {
		match := matcher.Match(entry.Path, false)
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
