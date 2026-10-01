package cli

import (
	"fmt"
	"gogit/internal/repo"
	"gogit/internal/worktree"
	"os"
)

func init() {
	Register("worktree", cmdWorktree)
}

func cmdWorktree(ctx *Context) int {
	if len(ctx.Args) == 0 {
		fmt.Fprintln(ctx.Stderr, "usage: gogit worktree [list|add|remove|prune]")
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
	case "list":
		wts, err := worktree.ListWorktrees(r)
		if err != nil {
			fmt.Fprintf(ctx.Stderr, "fatal: 列出工作区失败: %v\n", err)
			return ExitFatal
		}
		for _, wt := range wts {
			branchStr := ""
			if wt.Branch != "" {
				branchStr = fmt.Sprintf("[%s]", wt.Branch)
			} else if !wt.HeadOID.IsZero() {
				branchStr = fmt.Sprintf("(detached HEAD %s)", wt.HeadOID.Short())
			} else if wt.IsBare {
				branchStr = "(bare)"
			}
			fmt.Fprintf(ctx.Stdout, "%-40s %s %s\n", wt.Path, wt.HeadOID.Short(), branchStr)
		}
		return ExitSuccess

	case "add":
		if len(ctx.Args) < 2 {
			fmt.Fprintln(ctx.Stderr, "usage: gogit worktree add <path> [<branch>]")
			return ExitUsage
		}
		path := ctx.Args[1]
		branch := ""
		if len(ctx.Args) > 2 {
			branch = ctx.Args[2]
		}
		if err := worktree.AddWorktree(r, path, branch); err != nil {
			fmt.Fprintf(ctx.Stderr, "fatal: 添加工作区失败: %v\n", err)
			return ExitFatal
		}
		fmt.Fprintf(ctx.Stdout, "Preparing worktree (checking out '%s')\n", path)
		return ExitSuccess

	case "remove":
		if len(ctx.Args) < 2 {
			fmt.Fprintln(ctx.Stderr, "usage: gogit worktree remove <path>")
			return ExitUsage
		}
		path := ctx.Args[1]
		if err := worktree.RemoveWorktree(r, path, false); err != nil {
			fmt.Fprintf(ctx.Stderr, "fatal: 移除工作区失败: %v\n", err)
			return ExitFatal
		}
		return ExitSuccess

	case "prune":
		pruned, err := worktree.PruneWorktrees(r)
		if err != nil {
			fmt.Fprintf(ctx.Stderr, "fatal: prune 工作区失败: %v\n", err)
			return ExitFatal
		}
		if pruned > 0 {
			fmt.Fprintf(ctx.Stdout, "Pruned %d worktrees\n", pruned)
		}
		return ExitSuccess

	default:
		fmt.Fprintf(ctx.Stderr, "fatal: 未知子命令 '%s'\n", sub)
		return ExitUsage
	}
}
