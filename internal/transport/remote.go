package transport

import (
	"fmt"
	"gogit/internal/repo"
	"os"
	"path/filepath"
	"strings"
)

// RemoteConfig 记录远端在 config 中的配置
type RemoteConfig struct {
	Name  string
	URL   string
	Fetch string
}

// ListRemotes 列出仓库中配置的所有远端信息
func ListRemotes(r *repo.Repository) ([]RemoteConfig, error) {
	cfgPath := filepath.Join(r.GitDir, "config")
	cfg, err := repo.ParseConfigFile(cfgPath, r.GitDir)
	if err != nil {
		return nil, err
	}

	var results []RemoteConfig
	for _, sec := range cfg.Sections() {
		if strings.HasPrefix(sec, "remote.") {
			name := strings.TrimPrefix(sec, "remote.")
			url := cfg.Get(sec, "url")
			fetch := cfg.Get(sec, "fetch")
			results = append(results, RemoteConfig{
				Name:  name,
				URL:   url,
				Fetch: fetch,
			})
		}
	}
	return results, nil
}

// GetRemote 获取指定名称的远端配置
func GetRemote(r *repo.Repository, name string) (*RemoteConfig, error) {
	remotes, err := ListRemotes(r)
	if err != nil {
		return nil, err
	}
	for _, rm := range remotes {
		if rm.Name == name {
			return &rm, nil
		}
	}
	return nil, fmt.Errorf("remote %s not found", name)
}

// AddRemote 向仓库中添加新的远端配置
func AddRemote(r *repo.Repository, name string, rawURL string) error {
	cfgPath := filepath.Join(r.GitDir, "config")
	cfg, err := repo.ParseConfigFile(cfgPath, r.GitDir)
	if err != nil {
		cfg = repo.NewConfig()
	}

	secName := fmt.Sprintf("remote.%s", name)
	if cfg.Get(secName, "url") != "" {
		return fmt.Errorf("remote %s already exists", name)
	}

	cfg.Set(secName, "url", rawURL)
	cfg.Set(secName, "fetch", fmt.Sprintf("+refs/heads/*:refs/remotes/%s/*", name))

	return os.WriteFile(cfgPath, cfg.Serialize(), 0644)
}

// RemoveRemote 删除指定远端及其配置
func RemoveRemote(r *repo.Repository, name string) error {
	cfgPath := filepath.Join(r.GitDir, "config")
	cfg, err := repo.ParseConfigFile(cfgPath, r.GitDir)
	if err != nil {
		return err
	}

	secName := fmt.Sprintf("remote.%s", name)
	if cfg.Get(secName, "url") == "" {
		return fmt.Errorf("remote %s does not exist", name)
	}

	cfg.RemoveSection(secName)
	return os.WriteFile(cfgPath, cfg.Serialize(), 0644)
}

// RenameRemote 重命名已存在的远端配置
func RenameRemote(r *repo.Repository, oldName string, newName string) error {
	oldSec := fmt.Sprintf("remote.%s", oldName)
	newSec := fmt.Sprintf("remote.%s", newName)

	cfgPath := filepath.Join(r.GitDir, "config")
	cfg, err := repo.ParseConfigFile(cfgPath, r.GitDir)
	if err != nil {
		return err
	}

	url := cfg.Get(oldSec, "url")
	if url == "" {
		return fmt.Errorf("remote %s does not exist", oldName)
	}

	cfg.RemoveSection(oldSec)
	cfg.Set(newSec, "url", url)
	cfg.Set(newSec, "fetch", fmt.Sprintf("+refs/heads/*:refs/remotes/%s/*", newName))

	return os.WriteFile(cfgPath, cfg.Serialize(), 0644)
}
