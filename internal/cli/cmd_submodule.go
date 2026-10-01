package cli

import (
	"fmt"
	"gogit/internal/repo"
	"gogit/internal/submodule"
	"os"
)

func init() {
	Register("submodule", cmdSubmodule)
}

func cmdSubmodule(ctx *Context) int {
	if len(ctx.Args) == 0 {
		fmt.Fprintln(ctx.Stderr, "usage: gogit submodule [add|init|status]")
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
	case "add":
		if len(ctx.Args) < 2 {
			fmt.Fprintln(ctx.Stderr, "usage: gogit submodule add <url> [<path>]")
			return ExitUsage
		}
		url := ctx.Args[1]
		path := ""
		if len(ctx.Args) > 2 {
			path = ctx.Args[2]
		}
		if err := submodule.AddSubmodule(r, url, path); err != nil {
			fmt.Fprintf(ctx.Stderr, "fatal: 添加子模块失败: %v\n", err)
			return ExitFatal
		}
		fmt.Fprintf(ctx.Stdout, "Added submodule '%s'\n", url)
		return ExitSuccess

	case "init":
		inited, err := submodule.InitSubmodules(r)
		if err != nil {
			fmt.Fprintf(ctx.Stderr, "fatal: 初始化子模块失败: %v\n", err)
			return ExitFatal
		}
		for _, name := range inited {
			fmt.Fprintf(ctx.Stdout, "Submodule '%s' registered for path '%s'\n", name, name)
		}
		return ExitSuccess

	case "status":
		statusList, err := submodule.StatusSubmodules(r)
		if err != nil {
			fmt.Fprintf(ctx.Stderr, "fatal: 获取子模块状态失败: %v\n", err)
			return ExitFatal
		}
		for _, s := range statusList {
			fmt.Fprintf(ctx.Stdout, " %s %s\n", s.Commit.String(), s.Path)
		}
		return ExitSuccess

	default:
		fmt.Fprintf(ctx.Stderr, "fatal: 未知子命令 '%s'\n", sub)
		return ExitUsage
	}
}
