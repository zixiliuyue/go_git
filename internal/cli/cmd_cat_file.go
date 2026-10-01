package cli

import (
	"fmt"
	"gogit/internal/object"
	"gogit/internal/repo"
	"gogit/internal/rev"
)

func init() {
	Register("cat-file", cmdCatFile)
}

func cmdCatFile(ctx *Context) int {
	if len(ctx.Args) < 2 {
		fmt.Fprintln(ctx.Stderr, "用法: gogit cat-file (-p | -t | -s) <object>")
		return ExitFatal
	}

	mode := ctx.Args[0]
	revExpr := ctx.Args[1]

	r, err := repo.FindRepository(".")
	if err != nil {
		fmt.Fprintf(ctx.Stderr, "fatal: %v\n", err)
		return ExitFatal
	}

	h, err := rev.ParseRevision(r, revExpr)
	if err != nil {
		fmt.Fprintf(ctx.Stderr, "fatal: 未知对象: %s\n", revExpr)
		return ExitFatal
	}

	raw, err := r.ReadObject(h)
	if err != nil {
		fmt.Fprintf(ctx.Stderr, "fatal: 读取对象 %s 失败: %v\n", h.String(), err)
		return ExitFatal
	}

	switch mode {
	case "-t":
		fmt.Fprintln(ctx.Stdout, raw.ObjType)
		return ExitSuccess
	case "-s":
		fmt.Fprintln(ctx.Stdout, len(raw.Content))
		return ExitSuccess
	case "-p":
		switch raw.ObjType {
		case object.TypeBlob:
			ctx.Stdout.Write(raw.Content)
			return ExitSuccess
		case object.TypeTree:
			tree, err := object.ParseTree(raw.Content)
			if err != nil {
				fmt.Fprintf(ctx.Stderr, "fatal: 解析 tree 失败: %v\n", err)
				return ExitFatal
			}
			for _, entry := range tree.Entries {
				var typeStr string
				var modeStr string
				if entry.Mode == object.ModeDirectory {
					typeStr = "tree"
					modeStr = "040000"
				} else if entry.Mode == object.ModeSubmodule {
					typeStr = "commit"
					modeStr = "160000"
				} else {
					typeStr = "blob"
					modeStr = fmt.Sprintf("%06o", entry.Mode)
				}
				fmt.Fprintf(ctx.Stdout, "%s %s %s\t%s\n", modeStr, typeStr, entry.OID.String(), entry.Name)
			}
			return ExitSuccess
		case object.TypeCommit, object.TypeTag:
			ctx.Stdout.Write(raw.Content)
			return ExitSuccess
		default:
			fmt.Fprintf(ctx.Stderr, "fatal: 不支持的对象类型: %s\n", raw.ObjType)
			return ExitFatal
		}
	default:
		fmt.Fprintf(ctx.Stderr, "fatal: 无效的选项: %s\n", mode)
		return ExitFatal
	}
}
