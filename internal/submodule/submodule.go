package submodule

import (
	"bytes"
	"fmt"
	"gogit/internal/index"
	"gogit/internal/object"
	"gogit/internal/repo"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// Submodule 描述一个子模块元数据
type Submodule struct {
	Name   string
	Path   string
	URL    string
	Branch string
	Commit object.Hash
}

// ParseGitmodules 解析仓库根目录下的 .gitmodules 文件
func ParseGitmodules(workTree string) ([]Submodule, error) {
	gmPath := filepath.Join(workTree, ".gitmodules")
	data, err := os.ReadFile(gmPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	cfg, err := repo.ParseConfigBytes(data)
	if err != nil {
		return nil, err
	}

	var res []Submodule
	for _, sec := range cfg.Sections() {
		var name string
		if strings.HasPrefix(sec, "submodule.") {
			name = strings.TrimPrefix(sec, "submodule.")
		} else if strings.HasPrefix(sec, `submodule "`) && strings.HasSuffix(sec, `"`) {
			name = sec[11 : len(sec)-1]
		}
		if name != "" {
			path := cfg.Get(sec, "path")
			url := cfg.Get(sec, "url")
			branch := cfg.Get(sec, "branch")
			if path != "" && url != "" {
				res = append(res, Submodule{
					Name:   name,
					Path:   path,
					URL:    url,
					Branch: branch,
				})
			}
		}
	}
	return res, nil
}

// WriteGitmodules 将子模块配置回写到 .gitmodules
func WriteGitmodules(workTree string, modules []Submodule) error {
	gmPath := filepath.Join(workTree, ".gitmodules")
	var buf bytes.Buffer
	for _, m := range modules {
		buf.WriteString(fmt.Sprintf("[submodule \"%s\"]\n", m.Name))
		buf.WriteString(fmt.Sprintf("\tpath = %s\n", m.Path))
		buf.WriteString(fmt.Sprintf("\turl = %s\n", m.URL))
		if m.Branch != "" {
			buf.WriteString(fmt.Sprintf("\tbranch = %s\n", m.Branch))
		}
	}
	return os.WriteFile(gmPath, buf.Bytes(), 0644)
}

// AddSubmodule 添加并初始化一个子模块
func AddSubmodule(r *repo.Repository, url, targetPath string) error {
	if targetPath == "" {
		base := filepath.Base(url)
		targetPath = strings.TrimSuffix(base, ".git")
	}

	modules, _ := ParseGitmodules(r.WorkTree)
	for _, m := range modules {
		if m.Path == targetPath {
			return fmt.Errorf("子模块路径 %s 已存在于 .gitmodules", targetPath)
		}
	}

	// 1. 克隆或拉取子模块仓库到目标目录
	diskPath := filepath.Join(r.WorkTree, targetPath)
	if _, err := os.Stat(diskPath); err == nil {
		entries, _ := os.ReadDir(diskPath)
		if len(entries) > 0 {
			return fmt.Errorf("目标路径 %s 已存在且非空", diskPath)
		}
	}

	// 调用 git clone 或内部传输克隆子模块
	cmd := exec.Command("git", "clone", url, diskPath)
	cmd.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null")
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("克隆子模块失败: %v, %s", err, string(out))
	}

	// 2. 读取子模块 HEAD 提交哈希
	headOut, err := exec.Command("git", "-C", diskPath, "rev-parse", "HEAD").Output()
	if err != nil {
		return fmt.Errorf("读取子模块 HEAD 失败: %w", err)
	}
	commitH, err := object.NewHashFromHex(strings.TrimSpace(string(headOut)))
	if err != nil {
		return err
	}

	// 3. 更新 .gitmodules
	modules = append(modules, Submodule{
		Name: targetPath,
		Path: targetPath,
		URL:  url,
	})
	if err := WriteGitmodules(r.WorkTree, modules); err != nil {
		return err
	}

	// 4. 将 160000 模式 gitlink 条目以及 .gitmodules 写入主仓库索引
	idx, err := index.ReadIndex(r.IndexPath)
	if err != nil {
		idx = index.NewIndex()
	}

	// 添加 .gitmodules 到索引
	gmRel := ".gitmodules"
	gmFi, _ := os.Stat(filepath.Join(r.WorkTree, gmRel))
	gmRaw, _ := os.ReadFile(filepath.Join(r.WorkTree, gmRel))
	gmHash, _ := r.WriteBlob(gmRaw)
	if gmFi != nil {
		idx.AddOrReplaceEntry(index.EntryFromOSFileInfo(gmRel, gmFi, gmHash))
	}

	// 添加 gitlink 条目 (Mode 160000)
	gitlinkEntry := &index.IndexEntry{
		Mode: uint32(object.ModeSubmodule),
		OID:  commitH,
		Path: targetPath,
	}
	idx.AddOrReplaceEntry(gitlinkEntry)

	return idx.WriteIndex(r.IndexPath)
}

// InitSubmodules 将 .gitmodules 配置注册同步到 .git/config
func InitSubmodules(r *repo.Repository) ([]string, error) {
	modules, err := ParseGitmodules(r.WorkTree)
	if err != nil {
		return nil, err
	}

	var initialized []string
	for _, m := range modules {
		sec := fmt.Sprintf("submodule.%s", m.Name)
		r.Config.Set(sec, "url", m.URL)
		r.Config.Set(sec, "active", "true")
		initialized = append(initialized, m.Name)
	}
	_ = os.WriteFile(filepath.Join(r.GitDir, "config"), r.Config.Serialize(), 0644)
	return initialized, nil
}

// StatusSubmodules 返回各子模块的状态信息
func StatusSubmodules(r *repo.Repository) ([]Submodule, error) {
	modules, err := ParseGitmodules(r.WorkTree)
	if err != nil {
		return nil, err
	}

	idx, _ := index.ReadIndex(r.IndexPath)
	for i := range modules {
		if idx != nil {
			if entry := idx.Find(modules[i].Path); entry != nil {
				modules[i].Commit = entry.OID
			}
		}
	}
	return modules, nil
}
