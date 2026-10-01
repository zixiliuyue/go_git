package cli

import (
	"fmt"
	"gogit/internal/merge"
	"gogit/internal/repo"
	"gogit/internal/rev"
)

func init() {
	Register("merge-base", cmdMergeBase)
}

func cmdMergeBase(ctx *Context) int {
	if len(ctx.Args) < 2 {
		fmt.Fprintln(ctx.Stderr, "usage: gogit merge-base <commit-1> <commit-2>")
		return ExitError
	}

	r, err := repo.FindRepository(".")
	if err != nil {
		fmt.Fprintf(ctx.Stderr, "fatal: %v\n", err)
		return ExitFatal
	}

	hashA, err := rev.ParseRevision(r, ctx.Args[0])
	if err != nil {
		fmt.Fprintf(ctx.Stderr, "fatal: 无法解析 %s: %v\n", ctx.Args[0], err)
		return ExitFatal
	}

	hashB, err := rev.ParseRevision(r, ctx.Args[1])
	if err != nil {
		fmt.Fprintf(ctx.Stderr, "fatal: 无法解析 %s: %v\n", ctx.Args[1], err)
		return ExitFatal
	}

	baseOID, err := merge.FindMergeBase(r, hashA, hashB)
	if err != nil {
		fmt.Fprintf(ctx.Stderr, "fatal: %v\n", err)
		return ExitGeneral
	}

	fmt.Fprintln(ctx.Stdout, baseOID.String())
	return ExitSuccess
}
