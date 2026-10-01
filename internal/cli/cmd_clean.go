package cli

import (
	"fmt"
	"gogit/internal/repo"
	"gogit/internal/worktree"
)

func init() {
	Register("clean", cmdClean)
}

func cmdClean(ctx *Context) int {
	force := false
	removeDirs := false
	removeIgnored := false
	dryRun := false

	for _, arg := range ctx.Args {
		switch arg {
		case "-f", "--force":
			force = true
		case "-d":
			removeDirs = true
		case "-x":
			removeIgnored = true
		case "-n", "--dry-run":
			dryRun = true
		}
	}

	if !force && !dryRun {
		fmt.Fprintf(ctx.Stderr, "fatal: clean.requireForce defaults to true and neither -i, -n, nor -f given; refusing to clean\n")
		return ExitFatal
	}

	r, err := repo.FindRepository(".")
	if err != nil {
		fmt.Fprintf(ctx.Stderr, "fatal: %v\n", err)
		return ExitFatal
	}

	if dryRun {
		st, err := worktree.ComputeStatus(r)
		if err != nil {
			fmt.Fprintf(ctx.Stderr, "fatal: %v\n", err)
			return ExitFatal
		}
		for _, item := range st.Items {
			if item.IsUntracked || (removeIgnored && item.IsIgnored) {
				fmt.Fprintf(ctx.Stdout, "Would remove %s\n", item.Path)
			}
		}
		return ExitSuccess
	}

	removed, err := worktree.CleanWorktree(r, removeDirs, removeIgnored)
	if err != nil {
		fmt.Fprintf(ctx.Stderr, "fatal: %v\n", err)
		return ExitFatal
	}

	for _, p := range removed {
		fmt.Fprintf(ctx.Stdout, "Removing %s\n", p)
	}

	return ExitSuccess
}
