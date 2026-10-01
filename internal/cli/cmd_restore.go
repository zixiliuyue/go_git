package cli

import (
	"fmt"
	"gogit/internal/repo"
	"gogit/internal/worktree"
	"strings"
)

func init() {
	Register("restore", cmdRestore)
}

func cmdRestore(ctx *Context) int {
	staged := false
	worktreeFlag := false
	var paths []string

	for _, arg := range ctx.Args {
		switch arg {
		case "--staged":
			staged = true
		case "--worktree":
			worktreeFlag = true
		default:
			if !strings.HasPrefix(arg, "-") {
				paths = append(paths, arg)
			}
		}
	}

	if !staged && !worktreeFlag {
		worktreeFlag = true
	}

	if len(paths) == 0 {
		fmt.Fprintf(ctx.Stderr, "fatal: you must specify path(s) to restore\n")
		return ExitFatal
	}

	r, err := repo.FindRepository(".")
	if err != nil {
		fmt.Fprintf(ctx.Stderr, "fatal: %v\n", err)
		return ExitFatal
	}

	if err := worktree.RestoreFiles(r, paths, staged, worktreeFlag); err != nil {
		fmt.Fprintf(ctx.Stderr, "error: %v\n", err)
		return ExitError
	}

	return ExitSuccess
}
