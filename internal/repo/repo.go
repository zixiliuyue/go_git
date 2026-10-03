package repo

import (
	"fmt"
	"gogit/internal/giterr"
	"gogit/internal/index"
	"gogit/internal/object"
	"gogit/internal/pack"
	"gogit/internal/refs"
	"os"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

var (
	// ErrRepositoryNotFound 兼容旧代码，别名指向统一领域错误 ErrNotAGitRepository
	ErrRepositoryNotFound = giterr.ErrNotAGitRepository
)

// Repository 表示一个 Git 仓库实例（包含工作区、对象库、索引与引用系统）。
type Repository struct {
	GitDir       string
	WorkTree     string
	IsBare       bool
	ObjectsDir   string
	IndexPath    string
	Refs         *refs.Manager
	Config       *Config
	ObjectFormat object.ObjectFormat // 密码学哈希格式：sha1 或 sha256
}

// FindRepository 从指定起始目录向上递归查找最近的 Git 仓库（支持环境变量 GIT_DIR/GIT_WORK_TREE）。
func FindRepository(startDir string) (*Repository, error) {
	// 优先检查环境变量
	envGitDir := os.Getenv("GIT_DIR")
	envWorkTree := os.Getenv("GIT_WORK_TREE")
	if envGitDir != "" {
		absGitDir, err := filepath.Abs(envGitDir)
		if err != nil {
			return nil, err
		}
		var absWorkTree string
		if envWorkTree != "" {
			absWorkTree, _ = filepath.Abs(envWorkTree)
		}
		return OpenRepository(absGitDir, absWorkTree)
	}

	curr, err := filepath.Abs(startDir)
	if err != nil {
		return nil, err
	}

	for {
		gitCandidate := filepath.Join(curr, ".git")
		fi, err := os.Stat(gitCandidate)
		if err == nil {
			if fi.IsDir() {
				// 标准工作区 .git 目录
				return OpenRepository(gitCandidate, curr)
			}
			// 处理 submodule 等 gitdir 文件指引形式: "gitdir: <path>"
			data, readErr := os.ReadFile(gitCandidate)
			if readErr == nil {
				text := strings.TrimSpace(string(data))
				if strings.HasPrefix(text, "gitdir:") {
					targetDir := strings.TrimSpace(text[7:])
					if !filepath.IsAbs(targetDir) {
						targetDir = filepath.Join(curr, targetDir)
					}
					return OpenRepository(targetDir, curr)
				}
			}
		}

		// 检查当前目录本身是否就是 bare 仓库
		headCandidate := filepath.Join(curr, "HEAD")
		objsCandidate := filepath.Join(curr, "objects")
		if _, err1 := os.Stat(headCandidate); err1 == nil {
			if _, err2 := os.Stat(objsCandidate); err2 == nil {
				return OpenRepository(curr, "")
			}
		}

		parent := filepath.Dir(curr)
		if parent == curr {
			// 到达根目录仍未找到
			break
		}
		curr = parent
	}

	return nil, ErrRepositoryNotFound
}

// OpenRepository 打开已知路径的 Git 仓库。
func OpenRepository(gitDir, workTree string) (*Repository, error) {
	cfgPath := filepath.Join(gitDir, "config")
	cfg, err := ParseConfigFile(cfgPath, gitDir)
	if err != nil {
		cfg = NewConfig()
	}

	isBare := (workTree == "")
	if bareCfg := cfg.Get("core", "bare"); bareCfg == "true" {
		isBare = true
		workTree = ""
	}

	objFormat := object.FormatSHA1
	if extFormat := cfg.Get("extensions", "objectformat"); extFormat != "" {
		objFormat = object.ObjectFormat(extFormat)
	}

	return &Repository{
		GitDir:       gitDir,
		WorkTree:     workTree,
		IsBare:       isBare,
		ObjectsDir:   filepath.Join(gitDir, "objects"),
		IndexPath:    filepath.Join(gitDir, "index"),
		Refs:         refs.NewManager(gitDir),
		Config:       cfg,
		ObjectFormat: objFormat,
	}, nil
}

// InitRepository 在目标路径初始化一个全新的 Git 仓库（默认使用 SHA-1）。
func InitRepository(targetDir string, bare bool, initialBranch string) (*Repository, error) {
	return InitRepositoryWithFormat(targetDir, bare, initialBranch, object.FormatSHA1)
}

// InitRepositoryWithFormat 支持指定密码学哈希格式（SHA-1 或 SHA-256）初始化仓库（对齐 Git 2.29+ extensions.objectFormat 规范）。
func InitRepositoryWithFormat(targetDir string, bare bool, initialBranch string, format object.ObjectFormat) (*Repository, error) {
	if initialBranch == "" {
		initialBranch = "master"
	}

	absTarget, err := filepath.Abs(targetDir)
	if err != nil {
		return nil, err
	}

	var gitDir, workTree string
	if bare {
		gitDir = absTarget
		workTree = ""
	} else {
		gitDir = filepath.Join(absTarget, ".git")
		workTree = absTarget
	}

	// 创建基础目录层级
	dirs := []string{
		gitDir,
		filepath.Join(gitDir, "objects", "info"),
		filepath.Join(gitDir, "objects", "pack"),
		filepath.Join(gitDir, "refs", "heads"),
		filepath.Join(gitDir, "refs", "tags"),
	}
	for _, d := range dirs {
		if err := os.MkdirAll(d, 0755); err != nil {
			return nil, fmt.Errorf("创建目录 %s 失败: %w", d, err)
		}
	}

	// 写入 HEAD 文件
	headContent := fmt.Sprintf("ref: refs/heads/%s\n", initialBranch)
	if err := os.WriteFile(filepath.Join(gitDir, "HEAD"), []byte(headContent), 0644); err != nil {
		return nil, fmt.Errorf("写入 HEAD 失败: %w", err)
	}

	// 写入 description 文件
	descContent := "Unnamed repository; edit this file 'description' to name the repository.\n"
	_ = os.WriteFile(filepath.Join(gitDir, "description"), []byte(descContent), 0644)

	// 写入默认 config 文件
	cfg := NewConfig()
	if format == object.FormatSHA256 {
		// SHA-256 仓库在 Git 规范中必须启用 repositoryformatversion = 1 与 extensions.objectFormat = sha256
		cfg.Set("core", "repositoryformatversion", "1")
		cfg.Set("extensions", "objectformat", "sha256")
	} else {
		cfg.Set("core", "repositoryformatversion", "0")
	}
	cfg.Set("core", "filemode", "true")
	if bare {
		cfg.Set("core", "bare", "true")
	} else {
		cfg.Set("core", "bare", "false")
		cfg.Set("core", "logallrefupdates", "true")
	}
	_ = os.WriteFile(filepath.Join(gitDir, "config"), cfg.Serialize(), 0644)

	return OpenRepository(gitDir, workTree)
}

// ReadObject 根据哈希读取原始 Git 对象。
// 优先检索 loose 对象，若未命中则检索 objects/pack 目录下的所有 pack 文件。
func (r *Repository) ReadObject(h object.Hash) (*object.RawObject, error) {
	// Git replace 机制：若存在 refs/replace/<hash> 引用，透明重定向至替换目标
	if r.Refs != nil {
		if repHash, err := r.Refs.ResolveRef("refs/replace/" + h.String()); err == nil && !repHash.IsZero() {
			h = repHash
		}
	}

	hStr := h.String()
	loosePath := filepath.Join(r.ObjectsDir, hStr[:2], hStr[2:])
	if _, err := os.Stat(loosePath); err == nil {
		return object.ReadLooseObject(loosePath)
	}

	// 检索 pack 存储
	if raw, err := pack.ReadObjectFromPacks(r.ObjectsDir, h); err == nil {
		return raw, nil
	}

	return nil, &giterr.ObjectNotFoundError{OID: h}
}

// HasObject 检查指定哈希的对象在仓库中是否存在（松散对象或打包对象）
func (r *Repository) HasObject(h object.Hash) bool {
	_, err := r.ReadObject(h)
	return err == nil
}

// WriteObject 将对象序列化并写入仓库的 loose 存储。
func (r *Repository) WriteObject(obj object.Object) (object.Hash, error) {
	return object.WriteLooseObjectToDir(r.ObjectsDir, obj.Type(), obj.Payload())
}

// WriteBlob 便捷方法：将字节数据写入为 blob 对象。
func (r *Repository) WriteBlob(data []byte) (object.Hash, error) {
	return object.WriteLooseObjectToDir(r.ObjectsDir, object.TypeBlob, data)
}

// WriteCommit 便捷方法：写入 commit 对象。
func (r *Repository) WriteCommit(c *object.Commit) (object.Hash, error) {
	return r.WriteObject(c)
}

// WriteTree 便捷方法：写入 tree 对象。
func (r *Repository) WriteTree(t *object.Tree) (object.Hash, error) {
	return r.WriteObject(t)
}

// WriteTreeFromIndex 便捷方法：从 index 递归构建并写入 tree 对象。
func (r *Repository) WriteTreeFromIndex(idx *index.Index) (object.Hash, error) {
	return idx.WriteTree(r.ObjectsDir)
}

// ReadCommit 读取并解析 commit 对象。
func (r *Repository) ReadCommit(h object.Hash) (*object.Commit, error) {
	raw, err := r.ReadObject(h)
	if err != nil {
		return nil, err
	}
	if raw.ObjType != object.TypeCommit {
		return nil, fmt.Errorf("对象 %s 类型为 %s，非 commit", h.String(), raw.ObjType)
	}
	return object.ParseCommit(raw.Content)
}

// ReadTree 读取并解析 tree 对象。
func (r *Repository) ReadTree(h object.Hash) (*object.Tree, error) {
	raw, err := r.ReadObject(h)
	if err != nil {
		return nil, err
	}
	if raw.ObjType != object.TypeTree {
		return nil, fmt.Errorf("对象 %s 类型为 %s，非 tree", h.String(), raw.ObjType)
	}
	return object.ParseTree(raw.Content)
}

// ReadTag 读取并解析 tag 对象。
func (r *Repository) ReadTag(h object.Hash) (*object.Tag, error) {
	raw, err := r.ReadObject(h)
	if err != nil {
		return nil, err
	}
	if raw.ObjType != object.TypeTag {
		return nil, fmt.Errorf("对象 %s 类型为 %s，非 tag", h.String(), raw.ObjType)
	}
	return object.ParseTag(raw.Content)
}

// GetIndex 获取或读取当前仓库的暂存区索引。
func (r *Repository) GetIndex() (*index.Index, error) {
	return index.ReadIndex(r.IndexPath)
}

// SaveIndex 将索引写入当前仓库的 .git/index 文件。
func (r *Repository) SaveIndex(idx *index.Index) error {
	return idx.WriteIndex(r.IndexPath)
}

// AuthorSignature 解析并获取当前操作的 Author 签名（遵循 Git 全套环境变量与配置规则）。
func (r *Repository) AuthorSignature() object.Signature {
	name := os.Getenv("GIT_AUTHOR_NAME")
	email := os.Getenv("GIT_AUTHOR_EMAIL")
	dateStr := os.Getenv("GIT_AUTHOR_DATE")

	return r.resolveSignature(name, email, dateStr)
}

// CommitterSignature 解析并获取当前操作的 Committer 签名（遵循 Git 全套环境变量与配置规则）。
func (r *Repository) CommitterSignature() object.Signature {
	name := os.Getenv("GIT_COMMITTER_NAME")
	email := os.Getenv("GIT_COMMITTER_EMAIL")
	dateStr := os.Getenv("GIT_COMMITTER_DATE")

	return r.resolveSignature(name, email, dateStr)
}

func (r *Repository) resolveSignature(envName, envEmail, envDate string) object.Signature {
	name := envName
	email := envEmail

	if name == "" {
		name = r.Config.Get("user", "name")
	}
	if email == "" {
		email = r.Config.Get("user", "email")
	}

	// 尝试全局 gitconfig
	if name == "" || email == "" {
		if home, err := os.UserHomeDir(); err == nil {
			globalCfg, _ := ParseConfigFile(filepath.Join(home, ".gitconfig"), "")
			if name == "" {
				name = globalCfg.Get("user", "name")
			}
			if email == "" {
				email = globalCfg.Get("user", "email")
			}
		}
	}

	// 回退系统当前用户
	if name == "" {
		if u, err := user.Current(); err == nil && u.Username != "" {
			name = u.Username
		} else {
			name = "Unknown"
		}
	}
	if email == "" {
		email = name + "@localhost"
	}

	when := time.Now()
	tz := object.FormatTimezone(when)

	if envDate != "" {
		trimmed := strings.TrimPrefix(strings.TrimSpace(envDate), "@")
		parts := strings.Fields(trimmed)
		if len(parts) >= 1 {
			if ts, err := strconv.ParseInt(parts[0], 10, 64); err == nil {
				when = time.Unix(ts, 0)
				if len(parts) >= 2 {
					tz = parts[1]
				}
			} else if parsed, err := time.Parse(time.RFC3339, envDate); err == nil {
				when = parsed
				tz = object.FormatTimezone(when)
			}
		}
	}

	return object.Signature{
		Name:  name,
		Email: email,
		When:  when,
		TZ:    tz,
	}
}
