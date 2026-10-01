package cli

import (
	"fmt"
	"gogit/internal/repo"
	"gogit/internal/worktree"
	"strings"
)

func init() {
	Register("switch", cmdSwitch)
}

func cmdSwitch(ctx *Context) int {
	if len(ctx.Args) == 0 {
		fmt.Fprintf(ctx.Stderr, "fatal: missing branch or commit argument\n")
		return ExitFatal
	}

	r, err := repo.FindRepository(".")
	if err != nil {
		fmt.Fprintf(ctx.Stderr, "fatal: %v\n", err)
		return ExitFatal
	}

	opts := worktree.CheckoutOptions{}
	target := ""

	for i := 0; i < len(ctx.Args); i++ {
		arg := ctx.Args[i]
		if arg == "-c" || arg == "-C" {
			opts.CreateBranch = true
			if i+1 < len(ctx.Args) {
				target = ctx.Args[i+1]
				i++
			}
		} else if strings.HasPrefix(arg, "-c") || strings.HasPrefix(arg, "-C") {
			opts.CreateBranch = true
			target = arg[2:]
		} else if !strings.HasPrefix(arg, "-") {
			target = arg
		}
	}

	if target == "" {
		fmt.Fprintf(ctx.Stderr, "fatal: missing branch name\n")
		return ExitFatal
	}

	if err := worktree.CheckoutSwitch(r, target, opts); err != nil {
		fmt.Fprintf(ctx.Stderr, "%v\n", err)
		return ExitFatal
	}

	if opts.CreateBranch {
		fmt.Fprintf(ctx.Stderr, "Switched to a new branch '%s'\n", target)
	} else {
		fmt.Fprintf(ctx.Stderr, "Switched to branch '%s'\n", target)
	}

	return ExitSuccess
}
