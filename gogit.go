package gogit

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"gogit/internal/index"
	"gogit/internal/object"
	"gogit/internal/pack"
	"gogit/internal/repo"
	"gogit/internal/transport"
)

// Open 打开已存在的 Git 仓库（支持传入工作区根路径或 .git 目录路径）
func Open(ctx context.Context, path string) (*Repository, error) {
	r, err := repo.FindRepository(path)
	if err != nil {
		return nil, fmt.Errorf("打开 Git 仓库失败 '%s': %w", path, err)
	}
	return &Repository{inner: r}, nil
}

// Init 在指定路径初始化新的 Git 仓库（支持配置裸仓库、默认分支与 SHA-1/SHA-256 格式）
func Init(ctx context.Context, path string, opts ...InitOption) (*Repository, error) {
	o := &initOptions{
		bare:          false,
		defaultBranch: "main",
		objectFormat:  "sha1",
	}
	for _, opt := range opts {
		opt(o)
	}

	r, err := repo.InitRepositoryWithFormat(path, o.bare, o.defaultBranch, object.ObjectFormat(o.objectFormat))
	if err != nil {
		return nil, fmt.Errorf("初始化仓库失败 '%s': %w", path, err)
	}

	return &Repository{inner: r}, nil
}

// Clone 从远端 URL 克隆仓库到本地目标路径（支持 Smart HTTP、Local 等协议）
func Clone(ctx context.Context, urlStr string, targetPath string, opts ...CloneOption) (*Repository, error) {
	o := &cloneOptions{
		bare: false,
	}
	for _, opt := range opts {
		opt(o)
	}

	client, err := transport.NewClient(urlStr)
	if err != nil {
		return nil, fmt.Errorf("创建传输客户端失败: %w", err)
	}

	defaultBranch := o.branch
	if defaultBranch == "" {
		defaultBranch = "main"
	}

	r, err := repo.InitRepository(targetPath, o.bare, defaultBranch)
	if err != nil {
		return nil, fmt.Errorf("初始化目标仓库失败: %w", err)
	}

	remoteRefs, headSymref, err := client.Discover()
	if err != nil {
		return nil, fmt.Errorf("探测远端引用失败: %w", err)
	}

	if len(remoteRefs) == 0 {
		return &Repository{inner: r}, nil
	}

	var targetBranch string
	var targetCommit object.Hash

	if o.branch != "" {
		for _, rf := range remoteRefs {
			if rf.Name == "refs/heads/"+o.branch {
				targetBranch = o.branch
				targetCommit = rf.OID
				break
			}
		}
	} else if headSymref != "" && strings.HasPrefix(headSymref, "refs/heads/") {
		targetBranch = strings.TrimPrefix(headSymref, "refs/heads/")
		for _, rf := range remoteRefs {
			if rf.Name == headSymref {
				targetCommit = rf.OID
				break
			}
		}
	}

	if targetBranch == "" {
		for _, rf := range remoteRefs {
			if strings.HasPrefix(rf.Name, "refs/heads/") {
				targetBranch = strings.TrimPrefix(rf.Name, "refs/heads/")
				targetCommit = rf.OID
				break
			}
		}
	}

	var wants []object.Hash
	seenWants := make(map[object.Hash]bool)
	for _, rf := range remoteRefs {
		if (strings.HasPrefix(rf.Name, "refs/heads/") || strings.HasPrefix(rf.Name, "refs/tags/")) && !rf.OID.IsZero() {
			if !seenWants[rf.OID] {
				wants = append(wants, rf.OID)
				seenWants[rf.OID] = true
			}
		}
	}
	if len(wants) == 0 && !targetCommit.IsZero() {
		wants = append(wants, targetCommit)
	}

	if len(wants) > 0 {
		packData, err := client.Fetch(wants, nil)
		if err != nil {
			return nil, fmt.Errorf("拉取 pack 数据失败: %w", err)
		}

		if len(packData) > 0 {
			if _, err := pack.SavePackAndIndex(r.ObjectsDir, packData); err != nil {
				return nil, fmt.Errorf("解压并写入 pack 失败: %w", err)
			}
		}
	}

	// 记录远端引用与本地分支
	committer := r.CommitterSignature()
	for _, rf := range remoteRefs {
		if strings.HasPrefix(rf.Name, "refs/heads/") {
			branchName := strings.TrimPrefix(rf.Name, "refs/heads/")
			_ = r.Refs.UpdateRef("refs/remotes/origin/"+branchName, rf.OID, committer, "clone: remote ref")
			if branchName == targetBranch {
				_ = r.Refs.UpdateRef(rf.Name, rf.OID, committer, "clone: local ref")
			}
		} else if strings.HasPrefix(rf.Name, "refs/tags/") {
			_ = r.Refs.UpdateRef(rf.Name, rf.OID, committer, "clone: tag")
		}
	}

	if targetBranch != "" {
		_ = r.Refs.SetHEADSymbolic("refs/heads/" + targetBranch)
	}

	// 检出工作区文件
	if !o.bare && !targetCommit.IsZero() {
		rawCommit, err := r.ReadObject(targetCommit)
		if err == nil && rawCommit.Type() == object.TypeCommit {
			c, err := object.ParseCommit(rawCommit.Content)
			if err == nil {
				idx, _ := r.GetIndex()
				var writeTreeToDir func(treeOID object.Hash, dirPath string) error
				writeTreeToDir = func(treeOID object.Hash, dirPath string) error {
					treeRaw, err := r.ReadObject(treeOID)
					if err != nil {
						return err
					}
					treeObj, err := object.ParseTree(treeRaw.Content)
					if err != nil {
						return err
					}
					for _, e := range treeObj.Entries {
						entryPath := filepath.Join(dirPath, e.Name)
						if e.Mode == object.ModeDirectory {
							_ = os.MkdirAll(entryPath, 0755)
							if err := writeTreeToDir(e.OID, entryPath); err != nil {
								return err
							}
						} else {
							bRaw, err := r.ReadObject(e.OID)
							if err == nil {
								_ = os.WriteFile(entryPath, bRaw.Content, 0644)
								rel, _ := filepath.Rel(r.WorkTree, entryPath)
								if fi, err := os.Stat(entryPath); err == nil && idx != nil {
									entry := index.EntryFromOSFileInfo(rel, fi, e.OID)
									idx.AddOrReplaceEntry(entry)
								}
							}
						}
					}
					return nil
				}
				_ = writeTreeToDir(c.Tree, r.WorkTree)
				if idx != nil {
					_ = r.SaveIndex(idx)
				}
			}
		}
	}

	return &Repository{inner: r}, nil
}

// NewServer 创建基于 HTTP 的标准 Git 服务端处理器（兼容 git-http-backend 规范）
func NewServer(r *Repository, opts ...ServerOption) http.Handler {
	return r.HTTPHandler(opts...)
}
