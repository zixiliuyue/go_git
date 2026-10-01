package cli

import (
	"fmt"
	"gogit/internal/repo"
	"gogit/internal/worktree"
	"strings"
)

func init() {
	Register("checkout", cmdCheckout)
}

func cmdCheckout(ctx *Context) int {
	if len(ctx.Args) == 0 {
		fmt.Fprintf(ctx.Stderr, "fatal: you must specify a branch or file to checkout\n")
		return ExitFatal
	}

	r, err := repo.FindRepository(".")
	if err != nil {
		fmt.Fprintf(ctx.Stderr, "fatal: %v\n", err)
		return ExitFatal
	}

	// 检查是否包含 "--" 文件恢复语法
	dashIdx := -1
	for i, a := range ctx.Args {
		if a == "--" {
			dashIdx = i
			break
		}
	}

	if dashIdx >= 0 {
		paths := ctx.Args[dashIdx+1:]
		if len(paths) == 0 {
			return ExitSuccess
		}
		if err := worktree.RestoreFiles(r, paths, false, true); err != nil {
			fmt.Fprintf(ctx.Stderr, "error: %v\n", err)
			return ExitError
		}
		return ExitSuccess
	}

	// 分支切换与创建语法
	opts := worktree.CheckoutOptions{}
	target := ""

	for i := 0; i < len(ctx.Args); i++ {
		arg := ctx.Args[i]
		if arg == "-b" {
			opts.CreateBranch = true
			if i+1 < len(ctx.Args) {
				target = ctx.Args[i+1]
				i++
			}
		} else if strings.HasPrefix(arg, "-b") {
			opts.CreateBranch = true
			target = strings.TrimPrefix(arg, "-b")
		} else if !strings.HasPrefix(arg, "-") {
			target = arg
		}
	}

	if target == "" {
		fmt.Fprintf(ctx.Stderr, "fatal: 目标分支或提交不能为空\n")
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
