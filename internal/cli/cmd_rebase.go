package cli

import (
	"fmt"
	"gogit/internal/history"
	"gogit/internal/merge"
	"gogit/internal/repo"
	"strings"
)

func init() {
	Register("rebase", cmdRebase)
}

func cmdRebase(ctx *Context) int {
	isContinue := false
	isAbort := false
	var onto string
	var upstream string

	for i := 0; i < len(ctx.Args); i++ {
		arg := ctx.Args[i]
		switch {
		case arg == "--continue":
			isContinue = true
		case arg == "--abort":
			isAbort = true
		case arg == "--onto" && i+1 < len(ctx.Args):
			onto = ctx.Args[i+1]
			i++
		case strings.HasPrefix(arg, "--onto="):
			onto = strings.TrimPrefix(arg, "--onto=")
		default:
			if !strings.HasPrefix(arg, "-") && upstream == "" {
				upstream = arg
			}
		}
	}

	r, err := repo.FindRepository(".")
	if err != nil {
		fmt.Fprintf(ctx.Stderr, "fatal: %v\n", err)
		return ExitFatal
	}

	if isAbort {
		if err := history.AbortRebase(r); err != nil {
			fmt.Fprintf(ctx.Stderr, "fatal: %v\n", err)
			return ExitFatal
		}
		return ExitSuccess
	}

	if isContinue {
		res, err := history.ContinueRebase(r)
		if err != nil {
			fmt.Fprintf(ctx.Stderr, "fatal: %v\n", err)
			return ExitFatal
		}
		if res.Status == merge.MergeConflictStatus {
			fmt.Fprintf(ctx.Stderr, "error: %s\nResolve all conflicts manually, mark them as resolved with\n\"gogit add/rm <conflicted_files>\", then run \"gogit rebase --continue\".\n", res.Message)
			return ExitGeneral
		}
		fmt.Fprintln(ctx.Stdout, res.Message)
		return ExitSuccess
	}

	if upstream == "" {
		fmt.Fprintln(ctx.Stderr, "usage: gogit rebase [--onto <newbase>] <upstream>")
		return ExitError
	}

	res, err := history.StartRebase(r, upstream, onto, nil)
	if err != nil {
		fmt.Fprintf(ctx.Stderr, "fatal: %v\n", err)
		return ExitFatal
	}

	if res.Status == merge.MergeConflictStatus {
		fmt.Fprintf(ctx.Stderr, "error: %s\nResolve all conflicts manually, mark them as resolved with\n\"gogit add/rm <conflicted_files>\", then run \"gogit rebase --continue\".\n", res.Message)
		return ExitGeneral
	}

	fmt.Fprintln(ctx.Stdout, res.Message)
	return ExitSuccess
}
