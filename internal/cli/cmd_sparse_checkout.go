package cli

import (
	"fmt"
	"gogit/internal/repo"
	"gogit/internal/worktree"
	"os"
)

func init() {
	Register("sparse-checkout", cmdSparseCheckout)
}

func cmdSparseCheckout(ctx *Context) int {
	if len(ctx.Args) == 0 {
		fmt.Fprintln(ctx.Stderr, "usage: gogit sparse-checkout [init|set|list|disable]")
		return ExitUsage
	}

	cwd, err := os.Getwd()
	if err != nil {
		fmt.Fprintf(ctx.Stderr, "fatal: %v\n", err)
		return ExitFatal
	}

	r, err := repo.FindRepository(cwd)
	if err != nil {
		fmt.Fprintf(ctx.Stderr, "fatal: 不是 git 仓库: %v\n", err)
		return ExitFatal
	}

	sub := ctx.Args[0]
	switch sub {
	case "init":
		cone := false
		for _, arg := range ctx.Args[1:] {
			if arg == "--cone" {
				cone = true
			}
		}
		if err := worktree.InitSparseCheckout(r, cone); err != nil {
			fmt.Fprintf(ctx.Stderr, "fatal: 初始化 sparse-checkout 失败: %v\n", err)
			return ExitFatal
		}
		return ExitSuccess

	case "set":
		paths := ctx.Args[1:]
		if err := worktree.SetSparseCheckout(r, paths); err != nil {
			fmt.Fprintf(ctx.Stderr, "fatal: 设置 sparse-checkout 失败: %v\n", err)
			return ExitFatal
		}
		return ExitSuccess

	case "list":
		cfg, err := worktree.ReadSparseCheckout(r)
		if err != nil {
			fmt.Fprintf(ctx.Stderr, "fatal: 读取 sparse-checkout 失败: %v\n", err)
			return ExitFatal
		}
		for _, p := range cfg.Patterns {
			fmt.Fprintln(ctx.Stdout, p)
		}
		return ExitSuccess

	case "disable":
		if err := worktree.DisableSparseCheckout(r); err != nil {
			fmt.Fprintf(ctx.Stderr, "fatal: 禁用 sparse-checkout 失败: %v\n", err)
			return ExitFatal
		}
		return ExitSuccess

	default:
		fmt.Fprintf(ctx.Stderr, "fatal: 未知子命令 '%s'\n", sub)
		return ExitUsage
	}
}
