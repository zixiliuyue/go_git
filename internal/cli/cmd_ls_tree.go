package cli

import (
	"fmt"
	"gogit/internal/object"
	"gogit/internal/repo"
	"gogit/internal/rev"
	"path/filepath"
	"strings"
)

func init() {
	Register("ls-tree", cmdLsTree)
}

func cmdLsTree(ctx *Context) int {
	recursive := false
	nameOnly := false
	var treeIsh string

	for _, arg := range ctx.Args {
		if arg == "-r" {
			recursive = true
		} else if arg == "--name-only" {
			nameOnly = true
		} else if !strings.HasPrefix(arg, "-") && treeIsh == "" {
			treeIsh = arg
		}
	}

	if treeIsh == "" {
		fmt.Fprintln(ctx.Stderr, "用法: gogit ls-tree [-r] [--name-only] <tree-ish>")
		return ExitFatal
	}

	r, err := repo.FindRepository(".")
	if err != nil {
		fmt.Fprintf(ctx.Stderr, "fatal: %v\n", err)
		return ExitFatal
	}

	h, err := rev.ParseRevision(r, treeIsh)
	if err != nil {
		fmt.Fprintf(ctx.Stderr, "fatal: 未知树对象: %s\n", treeIsh)
		return ExitFatal
	}

	// 如果指向的是 commit，解出其 root tree
	treeHash := h
	raw, err := r.ReadObject(h)
	if err != nil {
		fmt.Fprintf(ctx.Stderr, "fatal: 读取对象失败: %v\n", err)
		return ExitFatal
	}
	if raw.ObjType == object.TypeCommit {
		c, err := object.ParseCommit(raw.Content)
		if err != nil {
			fmt.Fprintf(ctx.Stderr, "fatal: 解析 commit 失败: %v\n", err)
			return ExitFatal
		}
		treeHash = c.Tree
	}

	var walkTree func(th object.Hash, prefix string) error
	walkTree = func(th object.Hash, prefix string) error {
		t, err := r.ReadTree(th)
		if err != nil {
			return err
		}

		for _, entry := range t.Entries {
			entryPath := filepath.Join(prefix, entry.Name)
			if entry.Mode == object.ModeDirectory {
				if recursive {
					if err := walkTree(entry.OID, entryPath); err != nil {
						return err
					}
				} else {
					if nameOnly {
						fmt.Fprintln(ctx.Stdout, entryPath)
					} else {
						fmt.Fprintf(ctx.Stdout, "040000 tree %s\t%s\n", entry.OID.String(), entryPath)
					}
				}
			} else {
				if nameOnly {
					fmt.Fprintln(ctx.Stdout, entryPath)
				} else {
					var typeStr string
					var modeStr string
					if entry.Mode == object.ModeSubmodule {
						typeStr = "commit"
						modeStr = "160000"
					} else {
						typeStr = "blob"
						modeStr = fmt.Sprintf("%06o", entry.Mode)
					}
					fmt.Fprintf(ctx.Stdout, "%s %s %s\t%s\n", modeStr, typeStr, entry.OID.String(), entryPath)
				}
			}
		}
		return nil
	}

	if err := walkTree(treeHash, ""); err != nil {
		fmt.Fprintf(ctx.Stderr, "fatal: 遍历 tree 失败: %v\n", err)
		return ExitFatal
	}

	return ExitSuccess
}
