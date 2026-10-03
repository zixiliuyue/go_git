package gogit

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"gogit/internal/history"
	"gogit/internal/index"
	"gogit/internal/object"
	"gogit/internal/repo"
	"gogit/internal/rev"
	"gogit/internal/signature"
	"gogit/internal/transport"
	"gogit/internal/worktree"
)

// Repository 表示对 Git 仓库的公共操作句柄
type Repository struct {
	inner *repo.Repository
}

// Path 返回工作区根目录路径（裸仓库时为空）
func (r *Repository) Path() string {
	return r.inner.WorkTree
}

// GitDir 返回 .git 元数据目录路径
func (r *Repository) GitDir() string {
	return r.inner.GitDir
}

// IsBare 返回当前仓库是否为裸仓库
func (r *Repository) IsBare() bool {
	return r.inner.IsBare
}

// Inner 导出底层内部仓库指针，便于高级定制使用
func (r *Repository) Inner() *repo.Repository {
	return r.inner
}

// Head 获取当前 HEAD 引用的详细信息
func (r *Repository) Head(ctx context.Context) (*Reference, error) {
	headRef, err := r.inner.Refs.ReadHEAD()
	if err != nil {
		return nil, fmt.Errorf("读取 HEAD 失败: %w", err)
	}

	headOID, _ := r.inner.Refs.ResolveHEAD()
	return &Reference{
		Name:     "HEAD",
		Hash:     headOID.String(),
		Target:   headRef.Target,
		IsSymref: headRef.IsSymref,
	}, nil
}

// Branch 获取指定分支的引用信息
func (r *Repository) Branch(ctx context.Context, name string) (*Reference, error) {
	fullName := name
	if !strings.HasPrefix(fullName, "refs/heads/") {
		fullName = "refs/heads/" + name
	}
	oid, err := r.inner.Refs.ResolveRef(fullName)
	if err != nil {
		return nil, fmt.Errorf("分支不存在 '%s': %w", name, err)
	}
	return &Reference{
		Name: fullName,
		Hash: oid.String(),
	}, nil
}

// Branches 获取仓库中所有本地分支
func (r *Repository) Branches(ctx context.Context) ([]*Reference, error) {
	refMap, err := r.inner.Refs.ListRefs("refs/heads/")
	if err != nil {
		return nil, fmt.Errorf("列出分支失败: %w", err)
	}

	res := make([]*Reference, 0, len(refMap))
	for name, oid := range refMap {
		res = append(res, &Reference{
			Name: name,
			Hash: oid.String(),
		})
	}
	return res, nil
}

// Commit 解析并返回指定修订版本（如 HEAD, main, commit-hash）的提交对象
func (r *Repository) Commit(ctx context.Context, revStr string) (*Commit, error) {
	oid, err := rev.ParseRevision(r.inner, revStr)
	if err != nil {
		return nil, fmt.Errorf("解析提交版本失败 '%s': %w", revStr, err)
	}

	raw, err := r.inner.ReadObject(oid)
	if err != nil {
		return nil, fmt.Errorf("读取提交对象失败: %w", err)
	}

	if raw.Type() != object.TypeCommit {
		return nil, fmt.Errorf("对象 %s 不是提交类型 (实际为 %s)", oid.String(), string(raw.Type()))
	}

	commitObj, err := object.ParseCommit(raw.Content)
	if err != nil {
		return nil, fmt.Errorf("解析提交数据失败: %w", err)
	}

	parents := make([]string, len(commitObj.Parents))
	for i, p := range commitObj.Parents {
		parents[i] = p.String()
	}

	_, gpgSig, _, _ := signature.ExtractCommitSignature(raw.Content)

	return &Commit{
		Hash:     oid.String(),
		TreeHash: commitObj.Tree.String(),
		Parents:  parents,
		Author: Signature{
			Name:  commitObj.Author.Name,
			Email: commitObj.Author.Email,
			When:  commitObj.Author.When,
		},
		Committer: Signature{
			Name:  commitObj.Committer.Name,
			Email: commitObj.Committer.Email,
			When:  commitObj.Committer.When,
		},
		Message: commitObj.Message,
		GPGSig:  gpgSig,
	}, nil
}

// Commits 从指定起始版本开始按拓扑/时间序遍历历史提交
func (r *Repository) Commits(ctx context.Context, revStr string, maxCount int) ([]*Commit, error) {
	startOID, err := rev.ParseRevision(r.inner, revStr)
	if err != nil {
		return nil, fmt.Errorf("解析版本失败 '%s': %w", revStr, err)
	}

	revItems, err := rev.RevList(r.inner, []object.Hash{startOID}, nil, rev.RevListOptions{
		MaxCount: maxCount,
	})
	if err != nil {
		return nil, fmt.Errorf("遍历提交历史失败: %w", err)
	}

	list := make([]*Commit, len(revItems))
	for i, item := range revItems {
		c := item.Commit
		parents := make([]string, len(c.Parents))
		for pi, p := range c.Parents {
			parents[pi] = p.String()
		}
		list[i] = &Commit{
			Hash:     item.Hash.String(),
			TreeHash: c.Tree.String(),
			Parents:  parents,
			Author: Signature{
				Name:  c.Author.Name,
				Email: c.Author.Email,
				When:  c.Author.When,
			},
			Committer: Signature{
				Name:  c.Committer.Name,
				Email: c.Committer.Email,
				When:  c.Committer.When,
			},
			Message: c.Message,
			GPGSig:  c.GPGSig,
		}
	}
	return list, nil
}

// Tag 获取指定名称的标签对象
func (r *Repository) Tag(ctx context.Context, name string) (*Tag, error) {
	tags, err := r.Tags(ctx)
	if err != nil {
		return nil, err
	}
	for _, t := range tags {
		if t.Name == name || t.Name == "refs/tags/"+name {
			return t, nil
		}
	}
	return nil, fmt.Errorf("未找到标签: %s", name)
}

// Tags 获取仓库中所有的标签列表
func (r *Repository) Tags(ctx context.Context) ([]*Tag, error) {
	tagNames, err := history.ListTags(r.inner)
	if err != nil {
		return nil, fmt.Errorf("列出标签失败: %w", err)
	}

	res := make([]*Tag, 0, len(tagNames))
	for _, name := range tagNames {
		refPath := "refs/tags/" + name
		oid, err := r.inner.Refs.ResolveRef(refPath)
		if err != nil {
			continue
		}

		tagItem := &Tag{
			Name:       name,
			Hash:       oid.String(),
			TargetHash: oid.String(),
		}

		// 检查是否为附注标签对象
		if raw, err := r.inner.ReadObject(oid); err == nil && raw.Type() == object.TypeTag {
			if tagObj, err := object.ParseTag(raw.Content); err == nil {
				tagItem.TargetHash = tagObj.Object.String()
				tagItem.Message = tagObj.Message
				if tagObj.Tagger.Name != "" {
					tagItem.Tagger = Signature{
						Name:  tagObj.Tagger.Name,
						Email: tagObj.Tagger.Email,
						When:  tagObj.Tagger.When,
					}
				}
				// 检查是否包含数字签名
				if _, _, sigType, err := signature.ExtractTagSignature(raw.Content); err == nil && sigType != signature.SigTypeNone {
					tagItem.IsSigned = true
				}
			}
		}

		res = append(res, tagItem)
	}
	return res, nil
}

// CreateTag 在仓库中创建新标签（支持轻量、附注与密码学签名）
func (r *Repository) CreateTag(ctx context.Context, name string, opts ...TagOption) (*Tag, error) {
	o := &tagOptions{}
	for _, opt := range opts {
		opt(o)
	}

	_, err := history.CreateTag(r.inner, name, history.TagOptions{
		Annotated: o.annotated,
		Sign:      o.sign,
		Message:   o.message,
		Target:    o.target,
	})
	if err != nil {
		return nil, fmt.Errorf("创建标签失败: %w", err)
	}

	return r.Tag(ctx, name)
}

// Status 检查当前工作区与暂存区的状态
func (r *Repository) Status(ctx context.Context) (*Status, error) {
	st, err := worktree.ComputeStatus(r.inner)
	if err != nil {
		return nil, fmt.Errorf("计算状态失败: %w", err)
	}

	out := &Status{
		Branch:  st.BranchName,
		IsClean: len(st.Items) == 0,
	}

	for _, it := range st.Items {
		if it.IsUntracked {
			out.Untracked = append(out.Untracked, it.Path)
			continue
		}
		if it.Staged != ' ' && it.Staged != 0 {
			out.Staged = append(out.Staged, it.Path)
		}
		if it.Unstaged == 'M' {
			out.Modified = append(out.Modified, it.Path)
		} else if it.Unstaged == 'D' {
			out.Deleted = append(out.Deleted, it.Path)
		}
	}

	return out, nil
}

// Add 将指定文件模式添加到暂存区
func (r *Repository) Add(ctx context.Context, patterns ...string) error {
	if r.inner.IsBare {
		return fmt.Errorf("裸仓库无法执行 add 操作")
	}

	idx, err := r.inner.GetIndex()
	if err != nil {
		return fmt.Errorf("读取索引失败: %w", err)
	}

	for _, spec := range patterns {
		targetPath := filepath.Join(r.inner.WorkTree, spec)
		fi, err := os.Stat(targetPath)
		if err != nil {
			rel, relErr := filepath.Rel(r.inner.WorkTree, targetPath)
			if relErr == nil {
				idx.RemoveEntry(rel)
				continue
			}
			return fmt.Errorf("路径未找到: %s", spec)
		}

		if fi.IsDir() {
			dirRel, _ := filepath.Rel(r.inner.WorkTree, targetPath)
			dirRel = filepath.ToSlash(dirRel)

			err = filepath.Walk(targetPath, func(path string, info os.FileInfo, err error) error {
				if err != nil || info.IsDir() {
					return err
				}
				rel, _ := filepath.Rel(r.inner.WorkTree, path)
				if strings.HasPrefix(rel, ".git") {
					return nil
				}
				content, err := os.ReadFile(path)
				if err != nil {
					return err
				}
				blobOID, err := object.WriteLooseObjectToDir(r.inner.ObjectsDir, object.TypeBlob, content)
				if err != nil {
					return err
				}
				entry := index.EntryFromOSFileInfo(rel, info, blobOID)
				idx.AddOrReplaceEntry(entry)
				return nil
			})
			if err != nil {
				return err
			}

			// 同步移除已被物理删除的条目
			var toRemove []string
			for _, entry := range idx.Entries {
				match := false
				if dirRel == "." || dirRel == "" {
					match = true
				} else if strings.HasPrefix(entry.Path, dirRel+"/") || entry.Path == dirRel {
					match = true
				}
				if match {
					fullPath := filepath.Join(r.inner.WorkTree, entry.Path)
					if _, err := os.Stat(fullPath); os.IsNotExist(err) {
						toRemove = append(toRemove, entry.Path)
					}
				}
			}
			for _, p := range toRemove {
				idx.RemoveEntry(p)
			}
		} else {
			rel, _ := filepath.Rel(r.inner.WorkTree, targetPath)
			content, err := os.ReadFile(targetPath)
			if err != nil {
				return err
			}
			blobOID, err := object.WriteLooseObjectToDir(r.inner.ObjectsDir, object.TypeBlob, content)
			if err != nil {
				return err
			}
			entry := index.EntryFromOSFileInfo(rel, fi, blobOID)
			idx.AddOrReplaceEntry(entry)
		}
	}

	return r.inner.SaveIndex(idx)
}

// CreateCommit 提交暂存区变更，生成新的 Commit 并前移当前分支
func (r *Repository) CreateCommit(ctx context.Context, message string, opts ...CommitOption) (*Commit, error) {
	o := &commitOptions{}
	for _, opt := range opts {
		opt(o)
	}

	idx, err := r.inner.GetIndex()
	if err != nil {
		return nil, fmt.Errorf("读取索引失败: %w", err)
	}

	treeHash, err := idx.WriteTree(r.inner.ObjectsDir)
	if err != nil {
		return nil, fmt.Errorf("构建 Tree 失败: %w", err)
	}

	// 确定父提交
	var parents []object.Hash
	if len(o.parents) > 0 {
		for _, pStr := range o.parents {
			pH, err := object.NewHashFromHex(pStr)
			if err != nil {
				return nil, fmt.Errorf("无效的父提交哈希: %w", err)
			}
			parents = append(parents, pH)
		}
	} else {
		headOID, err := r.inner.Refs.ResolveHEAD()
		if err == nil && !headOID.IsZero() {
			parents = append(parents, headOID)
		}
	}

	author := r.inner.AuthorSignature()
	if o.author != nil {
		author = object.Signature{
			Name:  o.author.Name,
			Email: o.author.Email,
			When:  o.author.When,
		}
	}

	committer := r.inner.CommitterSignature()
	if o.committer != nil {
		committer = object.Signature{
			Name:  o.committer.Name,
			Email: o.committer.Email,
			When:  o.committer.When,
		}
	}

	if !strings.HasSuffix(message, "\n") {
		message += "\n"
	}

	commit := &object.Commit{
		Tree:      treeHash,
		Parents:   parents,
		Author:    author,
		Committer: committer,
		Message:   message,
	}

	// 密码学签名
	if o.sign {
		unsignedPayload := commit.Serialize()
		signer, err := signature.NewSSHSigner(author.Email)
		if err != nil {
			return nil, fmt.Errorf("初始化签名器失败: %w", err)
		}
		sigArmor, err := signer.Sign(unsignedPayload)
		if err != nil {
			return nil, fmt.Errorf("签名失败: %w", err)
		}
		commit.GPGSig = sigArmor
	}

	commitHash, err := r.inner.WriteObject(commit)
	if err != nil {
		return nil, fmt.Errorf("写入 commit 对象失败: %w", err)
	}

	// 更新 HEAD / 当前分支
	headRef, err := r.inner.Refs.ReadHEAD()
	if err == nil && headRef != nil && headRef.IsSymref {
		_ = r.inner.Refs.UpdateRef(headRef.Target, commitHash, committer, "commit: "+message)
	} else {
		_ = r.inner.Refs.SetHEADDetached(commitHash)
	}

	return r.Commit(ctx, commitHash.String())
}

// VerifyCommit 验证指定提交的数字签名有效性
func (r *Repository) VerifyCommit(ctx context.Context, revStr string) (*VerificationResult, error) {
	oid, err := rev.ParseRevision(r.inner, revStr)
	if err != nil {
		return nil, fmt.Errorf("解析版本失败 '%s': %w", revStr, err)
	}

	raw, err := r.inner.ReadObject(oid)
	if err != nil {
		return nil, err
	}
	if raw.Type() != object.TypeCommit {
		return nil, fmt.Errorf("对象不是 Commit 类型: %s", oid.String())
	}

	unsignedPayload, sigBlock, _, err := signature.ExtractCommitSignature(raw.Content)
	if err != nil {
		return nil, fmt.Errorf("未包含签名数据: %w", err)
	}

	vRes, err := signature.VerifySignature(unsignedPayload, sigBlock)
	if err != nil {
		return nil, err
	}

	return &VerificationResult{
		Valid:       vRes.Valid,
		Type:        string(vRes.Type),
		Signer:      vRes.Signer,
		KeyID:       vRes.KeyID,
		Fingerprint: vRes.Fingerprint,
		Message:     vRes.RawOutput,
	}, nil
}

// VerifyTag 验证指定标签的数字签名有效性
func (r *Repository) VerifyTag(ctx context.Context, name string) (*VerificationResult, error) {
	fullName := name
	if !strings.HasPrefix(fullName, "refs/tags/") {
		fullName = "refs/tags/" + name
	}

	tagOID, err := r.inner.Refs.ResolveRef(fullName)
	if err != nil {
		return nil, fmt.Errorf("标签未找到 '%s': %w", name, err)
	}

	raw, err := r.inner.ReadObject(tagOID)
	if err != nil {
		return nil, err
	}
	if raw.Type() != object.TypeTag {
		return nil, fmt.Errorf("标签不是附注 Tag 对象 (轻量标签无签名): %s", tagOID.String())
	}

	unsignedPayload, sigBlock, _, err := signature.ExtractTagSignature(raw.Content)
	if err != nil {
		return nil, fmt.Errorf("未包含签名: %w", err)
	}

	vRes, err := signature.VerifySignature(unsignedPayload, sigBlock)
	if err != nil {
		return nil, err
	}

	return &VerificationResult{
		Valid:       vRes.Valid,
		Type:        string(vRes.Type),
		Signer:      vRes.Signer,
		KeyID:       vRes.KeyID,
		Fingerprint: vRes.Fingerprint,
		Message:     vRes.RawOutput,
	}, nil
}

// Tree 读取指定版本对应的目录树对象
func (r *Repository) Tree(ctx context.Context, revStr string) (*Tree, error) {
	oid, err := rev.ParseRevision(r.inner, revStr)
	if err != nil {
		return nil, err
	}

	raw, err := r.inner.ReadObject(oid)
	if err != nil {
		return nil, err
	}

	var treeOID object.Hash
	if raw.Type() == object.TypeCommit {
		c, err := object.ParseCommit(raw.Content)
		if err != nil {
			return nil, err
		}
		treeOID = c.Tree
	} else if raw.Type() == object.TypeTree {
		treeOID = oid
	} else {
		return nil, fmt.Errorf("对象不能解析为 Tree: %s", raw.Type())
	}

	treeRaw, err := r.inner.ReadObject(treeOID)
	if err != nil {
		return nil, err
	}

	parsedTree, err := object.ParseTree(treeRaw.Content)
	if err != nil {
		return nil, err
	}

	entries := make([]TreeEntry, len(parsedTree.Entries))
	for i, e := range parsedTree.Entries {
		entries[i] = TreeEntry{
			Mode: fmt.Sprintf("%06o", e.Mode),
			Name: e.Name,
			Hash: e.OID.String(),
		}
	}

	return &Tree{
		Hash:    treeOID.String(),
		Entries: entries,
	}, nil
}

// ReadBlob 读取指定哈希的 Blob 原始内容
func (r *Repository) ReadBlob(ctx context.Context, hashStr string) ([]byte, error) {
	oid, err := object.NewHashFromHex(hashStr)
	if err != nil {
		return nil, err
	}
	raw, err := r.inner.ReadObject(oid)
	if err != nil {
		return nil, err
	}
	if raw.Type() != object.TypeBlob {
		return nil, fmt.Errorf("对象不是 Blob 类型")
	}
	return raw.Content, nil
}

// HTTPHandler 返回该仓库的标准 Smart HTTP 服务端 Handler（支持微服务内无依赖挂载）
func (r *Repository) HTTPHandler(opts ...ServerOption) http.Handler {
	server := transport.NewSmartHTTPServer(r.inner)
	so := &serverOptions{allowPush: true}
	for _, opt := range opts {
		opt(so)
	}
	server.AllowPush = so.allowPush
	return server
}
