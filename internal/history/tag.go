package history

import (
	"fmt"
	"gogit/internal/object"
	"gogit/internal/repo"
	"gogit/internal/rev"
	"gogit/internal/signature"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

type TagOptions struct {
	Annotated bool
	Sign      bool // 是否进行数字签名
	Message   string
	Target    string
}

// CreateTag 创建轻量或附注标签（支持数字签名）
func CreateTag(r *repo.Repository, tagName string, opts TagOptions) (object.Hash, error) {
	targetOID, err := r.Refs.ResolveHEAD()
	if opts.Target != "" {
		targetOID, err = rev.ParseRevision(r, opts.Target)
	}
	if err != nil {
		return object.ZeroHash, fmt.Errorf("解析目标对象失败: %w", err)
	}

	tagRefPath := filepath.Join(r.GitDir, "refs", "tags", tagName)
	if _, err := os.Stat(tagRefPath); err == nil {
		return object.ZeroHash, fmt.Errorf("fatal: tag '%s' already exists", tagName)
	}

	refTargetOID := targetOID

	if opts.Sign {
		opts.Annotated = true
	}

	if opts.Annotated {
		// 创建附注 Tag 对象
		tagObj := &object.Tag{
			Object:     targetOID,
			ObjectType: object.TypeCommit,
			Name:       tagName,
			Tagger:     r.CommitterSignature(),
			Message:    opts.Message,
		}
		if tagObj.Message == "" {
			tagObj.Message = tagName
		}

		if opts.Sign {
			signer, err := signature.NewSSHSigner(tagObj.Tagger.Email)
			if err != nil {
				return object.ZeroHash, fmt.Errorf("初始化标签签名器失败: %w", err)
			}
			rawPayload := tagObj.Serialize()
			sigArmor, err := signer.Sign(rawPayload)
			if err != nil {
				return object.ZeroHash, fmt.Errorf("签署标签失败: %w", err)
			}
			if !strings.HasSuffix(tagObj.Message, "\n") {
				tagObj.Message += "\n"
			}
			tagObj.Message = tagObj.Message + sigArmor + "\n"
		}

		tagHash, err := r.WriteObject(tagObj)
		if err != nil {
			return object.ZeroHash, fmt.Errorf("写入 Tag 对象失败: %w", err)
		}
		refTargetOID = tagHash
	}

	// 写入 refs/tags/<tagName>
	if err := os.MkdirAll(filepath.Dir(tagRefPath), 0755); err != nil {
		return object.ZeroHash, err
	}
	if err := os.WriteFile(tagRefPath, []byte(refTargetOID.String()+"\n"), 0644); err != nil {
		return object.ZeroHash, err
	}

	return refTargetOID, nil
}

// ListTags 列出所有标签
func ListTags(r *repo.Repository) ([]string, error) {
	tagsDir := filepath.Join(r.GitDir, "refs", "tags")
	var tags []string

	err := filepath.Walk(tagsDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			if os.IsNotExist(err) {
				return nil
			}
			return err
		}
		if !info.IsDir() {
			rel, _ := filepath.Rel(tagsDir, path)
			tags = append(tags, rel)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}

	sort.Strings(tags)
	return tags, nil
}

// DeleteTag 删除标签
func DeleteTag(r *repo.Repository, tagName string) error {
	tagRefPath := filepath.Join(r.GitDir, "refs", "tags", tagName)
	if _, err := os.Stat(tagRefPath); os.IsNotExist(err) {
		return fmt.Errorf("tag '%s' not found.", tagName)
	}
	return os.Remove(tagRefPath)
}

// PeelTag 将标签对象递归剥离至底层的最终 Commit 或 Blob
func PeelTag(r *repo.Repository, tagOID object.Hash) (object.Hash, error) {
	curr := tagOID
	for {
		obj, err := r.ReadObject(curr)
		if err != nil {
			return object.ZeroHash, err
		}
		if obj.ObjType != object.TypeTag {
			return curr, nil
		}
		tag, err := object.ParseTag(obj.Content)
		if err != nil {
			return object.ZeroHash, err
		}
		curr = tag.Object
	}
}
