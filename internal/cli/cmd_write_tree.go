package cli

import (
	"fmt"
	"gogit/internal/repo"
)

func init() {
	Register("write-tree", cmdWriteTree)
}

func cmdWriteTree(ctx *Context) int {
	r, err := repo.FindRepository(".")
	if err != nil {
		fmt.Fprintf(ctx.Stderr, "fatal: %v\n", err)
		return ExitFatal
	}

	idx, err := r.GetIndex()
	if err != nil {
		fmt.Fprintf(ctx.Stderr, "fatal: 读取索引失败: %v\n", err)
		return ExitFatal
	}

	rootTreeHash, err := idx.WriteTree(r.ObjectsDir)
	if err != nil {
		fmt.Fprintf(ctx.Stderr, "fatal: 构建 Tree 失败: %v\n", err)
		return ExitFatal
	}

	fmt.Fprintln(ctx.Stdout, rootTreeHash.String())
	return ExitSuccess
}
