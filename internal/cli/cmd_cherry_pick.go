package cli

import (
	"fmt"
	"gogit/internal/history"
	"gogit/internal/merge"
	"gogit/internal/repo"
	"gogit/internal/rev"
)

func init() {
	Register("cherry-pick", cmdCherryPick)
}

func cmdCherryPick(ctx *Context) int {
	if len(ctx.Args) == 0 {
		fmt.Fprintln(ctx.Stderr, "usage: gogit cherry-pick <commit>")
		return ExitError
	}

	r, err := repo.FindRepository(".")
	if err != nil {
		fmt.Fprintf(ctx.Stderr, "fatal: %v\n", err)
		return ExitFatal
	}

	targetOID, err := rev.ParseRevision(r, ctx.Args[0])
	if err != nil {
		fmt.Fprintf(ctx.Stderr, "fatal: 无法解析提交 %s: %v\n", ctx.Args[0], err)
		return ExitFatal
	}

	res, err := history.CherryPick(r, targetOID)
	if err != nil {
		fmt.Fprintf(ctx.Stderr, "fatal: %v\n", err)
		return ExitFatal
	}

	if res.Status == merge.MergeConflictStatus {
		for _, c := range res.Conflicts {
			fmt.Fprintf(ctx.Stdout, "CONFLICT (%s): Merge conflict in %s\n", c.Type, c.Path)
		}
		fmt.Fprintf(ctx.Stderr, "error: %s\nhint: after resolving the conflicts, mark the corrected paths\nhint: with 'gogit add <paths>' or 'gogit rm <paths>'\nhint: and commit the result with 'gogit commit'\n", res.Message)
		return ExitGeneral
	}

	return ExitSuccess
}
