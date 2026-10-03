package cli

import (
	"fmt"
	"gogit/internal/history"
	"gogit/internal/repo"
	"os"
	"path/filepath"
)

func init() {
	Register("commit-graph", cmdCommitGraph)
}

// cmdCommitGraph 实现 gogit commit-graph [write|verify]
func cmdCommitGraph(ctx *Context) int {
	if len(ctx.Args) == 0 {
		fmt.Fprintln(ctx.Stderr, "usage: gogit commit-graph [write|verify] [--reachable]")
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

	action := ctx.Args[0]
	graphPath := filepath.Join(r.ObjectsDir, "info", "commit-graph")

	switch action {
	case "write":
		reachable := true // 默认 --reachable
		cgPath, err := history.WriteCommitGraph(r, reachable)
		if err != nil {
			fmt.Fprintf(ctx.Stderr, "error: 写入 commit-graph 失败: %v\n", err)
			return ExitError
		}
		fmt.Fprintf(ctx.Stdout, "Written %s\n", cgPath)
		return ExitSuccess

	case "verify":
		if err := history.VerifyCommitGraph(graphPath, r); err != nil {
			fmt.Fprintf(ctx.Stderr, "error: 校验 commit-graph 失败: %v\n", err)
			return ExitError
		}
		fmt.Fprintln(ctx.Stdout, "Verifying commit-graph: ok")
		return ExitSuccess

	default:
		fmt.Fprintf(ctx.Stderr, "fatal: 未知动作 '%s'\n", action)
		return ExitUsage
	}
}
