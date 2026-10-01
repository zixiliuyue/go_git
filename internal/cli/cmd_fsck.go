package cli

import (
	"fmt"
	"gogit/internal/object"
	"gogit/internal/repo"
	"os"
	"path/filepath"
	"strings"
)

func init() {
	Register("fsck", cmdFsck)
}

func cmdFsck(ctx *Context) int {
	strict := false
	full := false

	for _, arg := range ctx.Args {
		if arg == "--strict" {
			strict = true
		} else if arg == "--full" {
			full = true
		}
	}
	_ = strict
	_ = full

	r, err := repo.FindRepository(".")
	if err != nil {
		fmt.Fprintf(ctx.Stderr, "fatal: %v\n", err)
		return ExitFatal
	}

	hasError := false
	objectsFound := make(map[object.Hash]object.ObjectType)

	// 1. 扫描所有 loose 对象，检查 SHA-1 完整性
	err = filepath.Walk(r.ObjectsDir, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(r.ObjectsDir, path)
		if err != nil {
			return nil
		}
		// 排除 info 和 pack 目录
		if strings.HasPrefix(rel, "info") || strings.HasPrefix(rel, "pack") {
			return nil
		}

		parts := strings.Split(rel, string(os.PathSeparator))
		if len(parts) != 2 || len(parts[0]) != 2 || len(parts[1]) != 38 {
			return nil // 非 40 字符 loose 对象文件
		}

		rawObj, err := object.ReadLooseObject(path)
		if err != nil {
			fmt.Fprintf(ctx.Stderr, "error: 读取 loose 对象 %s 失败: %v\n", rel, err)
			hasError = true
			return nil
		}

		expectedHex := parts[0] + parts[1]
		if rawObj.Hash().String() != expectedHex {
			fmt.Fprintf(ctx.Stderr, "error: 对象 %s 校验和不匹配 (期望 %s, 实际 %s)\n", rel, expectedHex, rawObj.Hash().String())
			hasError = true
			return nil
		}

		objectsFound[rawObj.Hash()] = rawObj.ObjType
		return nil
	})

	if err != nil {
		fmt.Fprintf(ctx.Stderr, "fatal: 遍历 objects 目录失败: %v\n", err)
		return ExitFatal
	}

	// 2. 检查所有引用的目标对象是否存在
	refMap, err := r.Refs.ListRefs("")
	if err == nil {
		for refName, h := range refMap {
			if _, ok := objectsFound[h]; !ok {
				fmt.Fprintf(ctx.Stderr, "error: 引用 %s 指向不存在的对象 %s\n", refName, h.String())
				hasError = true
			}
		}
	}

	// 3. 递归验证 Tree 与 Commit 的引用完整性
	for h, objType := range objectsFound {
		switch objType {
		case object.TypeCommit:
			c, err := r.ReadCommit(h)
			if err != nil {
				fmt.Fprintf(ctx.Stderr, "error: 解析 commit %s 失败: %v\n", h.String(), err)
				hasError = true
				continue
			}
			if _, ok := objectsFound[c.Tree]; !ok {
				fmt.Fprintf(ctx.Stderr, "error: commit %s 指向不存在的 tree %s\n", h.String(), c.Tree.String())
				hasError = true
			}
			for _, p := range c.Parents {
				if _, ok := objectsFound[p]; !ok {
					fmt.Fprintf(ctx.Stderr, "error: commit %s 指向不存在的 parent %s\n", h.String(), p.String())
					hasError = true
				}
			}
		case object.TypeTree:
			t, err := r.ReadTree(h)
			if err != nil {
				fmt.Fprintf(ctx.Stderr, "error: 解析 tree %s 失败: %v\n", h.String(), err)
				hasError = true
				continue
			}
			for _, e := range t.Entries {
				if e.Mode == object.ModeSubmodule {
					continue // submodule 目标无需在本地对象库存在
				}
				if _, ok := objectsFound[e.OID]; !ok {
					fmt.Fprintf(ctx.Stderr, "error: tree %s 条目 %s 指向不存在的对象 %s\n", h.String(), e.Name, e.OID.String())
					hasError = true
				}
			}
		}
	}

	if hasError {
		return ExitGeneral
	}
	return ExitSuccess
}
