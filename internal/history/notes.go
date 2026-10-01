package history

import (
	"fmt"
	"gogit/internal/object"
	"gogit/internal/repo"
	"gogit/internal/rev"
	"strings"
)

const DefaultNotesRef = "refs/notes/commits"

// AddNote 为指定提交添加注释 (Note)
func AddNote(r *repo.Repository, commitStr, message string) error {
	targetOID, err := rev.ParseRevision(r, commitStr)
	if err != nil {
		return fmt.Errorf("解析提交 %s 失败: %w", commitStr, err)
	}

	// 1. 写入包含注释内容的 Blob
	if !strings.HasSuffix(message, "\n") {
		message += "\n"
	}
	blobOID, err := r.WriteBlob([]byte(message))
	if err != nil {
		return err
	}

	// 2. 读取现有 notes 树或新建
	var rootTree *object.Tree
	notesHead, err := r.Refs.ResolveRef(DefaultNotesRef)
	var parentCommits []object.Hash

	if err == nil && !notesHead.IsZero() {
		parentCommits = append(parentCommits, notesHead)
		notesCommit, err := r.ReadCommit(notesHead)
		if err == nil {
			rootTree, _ = r.ReadTree(notesCommit.Tree)
		}
	}

	if rootTree == nil {
		rootTree = &object.Tree{}
	}

	// 3. 更新 Tree 条目（以 40 字符哈希为文件名）
	entryName := targetOID.String()
	replaced := false
	for i, e := range rootTree.Entries {
		if e.Name == entryName {
			rootTree.Entries[i].OID = blobOID
			replaced = true
			break
		}
	}
	if !replaced {
		rootTree.Entries = append(rootTree.Entries, object.TreeEntry{
			Mode: object.ModeRegular,
			Name: entryName,
			OID:  blobOID,
		})
	}

	newTreeOID, err := r.WriteTree(rootTree)
	if err != nil {
		return err
	}

	// 4. 创建 notes 提交
	committer := r.CommitterSignature()
	notesCommit := &object.Commit{
		Tree:      newTreeOID,
		Parents:   parentCommits,
		Author:    committer,
		Committer: committer,
		Message:   fmt.Sprintf("Notes added by 'gogit notes add' for %s\n", targetOID.String()),
	}

	newNotesCommitOID, err := r.WriteCommit(notesCommit)
	if err != nil {
		return err
	}

	return r.Refs.UpdateRef(DefaultNotesRef, newNotesCommitOID, committer, "notes: add")
}

// ShowNote 获取指定提交的注释内容
func ShowNote(r *repo.Repository, commitStr string) (string, error) {
	targetOID, err := rev.ParseRevision(r, commitStr)
	if err != nil {
		return "", fmt.Errorf("解析提交 %s 失败: %w", commitStr, err)
	}

	notesHead, err := r.Refs.ResolveRef(DefaultNotesRef)
	if err != nil || notesHead.IsZero() {
		return "", fmt.Errorf("未找到任何 notes")
	}

	notesCommit, err := r.ReadCommit(notesHead)
	if err != nil {
		return "", err
	}

	tree, err := r.ReadTree(notesCommit.Tree)
	if err != nil {
		return "", err
	}

	entryName := targetOID.String()
	for _, e := range tree.Entries {
		if e.Name == entryName {
			raw, err := r.ReadObject(e.OID)
			if err != nil {
				return "", err
			}
			return string(raw.Content), nil
		}
	}

	return "", fmt.Errorf("提交 %s 没有注释", targetOID.String())
}

// ListNotes 列出当前所有带注释的提交及对应的 note blob
func ListNotes(r *repo.Repository) (map[object.Hash]object.Hash, error) {
	notesHead, err := r.Refs.ResolveRef(DefaultNotesRef)
	if err != nil || notesHead.IsZero() {
		return nil, nil
	}

	notesCommit, err := r.ReadCommit(notesHead)
	if err != nil {
		return nil, err
	}

	tree, err := r.ReadTree(notesCommit.Tree)
	if err != nil {
		return nil, err
	}

	result := make(map[object.Hash]object.Hash)
	for _, e := range tree.Entries {
		if h, err := object.NewHashFromHex(e.Name); err == nil {
			result[h] = e.OID
		}
	}

	return result, nil
}
