package cli

import (
	"fmt"
	"gogit/internal/repo"
	"gogit/internal/transport"
	"os"
)

func init() {
	Register("remote", cmdRemote)
}

func cmdRemote(ctx *Context) int {
	cwd, err := os.Getwd()
	if err != nil {
		fmt.Fprintf(ctx.Stderr, "fatal: 无法获取当前工作目录: %v\n", err)
		return ExitFatal
	}

	r, err := repo.FindRepository(cwd)
	if err != nil {
		fmt.Fprintf(ctx.Stderr, "fatal: 不是 git 仓库: %v\n", err)
		return ExitFatal
	}

	if len(ctx.Args) == 0 {
		remotes, err := transport.ListRemotes(r)
		if err != nil {
			fmt.Fprintf(ctx.Stderr, "error: %v\n", err)
			return ExitGeneral
		}
		for _, rm := range remotes {
			fmt.Fprintln(ctx.Stdout, rm.Name)
		}
		return ExitSuccess
	}

	sub := ctx.Args[0]
	switch sub {
	case "-v", "--verbose":
		remotes, err := transport.ListRemotes(r)
		if err != nil {
			fmt.Fprintf(ctx.Stderr, "error: %v\n", err)
			return ExitGeneral
		}
		for _, rm := range remotes {
			fmt.Fprintf(ctx.Stdout, "%s\t%s (fetch)\n", rm.Name, rm.URL)
			fmt.Fprintf(ctx.Stdout, "%s\t%s (push)\n", rm.Name, rm.URL)
		}
		return ExitSuccess

	case "add":
		if len(ctx.Args) < 3 {
			fmt.Fprintln(ctx.Stderr, "用法: gogit remote add <名称> <URL>")
			return ExitGeneral
		}
		name := ctx.Args[1]
		url := ctx.Args[2]
		if err := transport.AddRemote(r, name, url); err != nil {
			fmt.Fprintf(ctx.Stderr, "fatal: %v\n", err)
			return ExitFatal
		}
		return ExitSuccess

	case "remove", "rm":
		if len(ctx.Args) < 2 {
			fmt.Fprintln(ctx.Stderr, "用法: gogit remote remove <名称>")
			return ExitGeneral
		}
		name := ctx.Args[1]
		if err := transport.RemoveRemote(r, name); err != nil {
			fmt.Fprintf(ctx.Stderr, "fatal: %v\n", err)
			return ExitFatal
		}
		return ExitSuccess

	case "rename":
		if len(ctx.Args) < 3 {
			fmt.Fprintln(ctx.Stderr, "用法: gogit remote rename <旧名称> <新名称>")
			return ExitGeneral
		}
		oldName := ctx.Args[1]
		newName := ctx.Args[2]
		if err := transport.RenameRemote(r, oldName, newName); err != nil {
			fmt.Fprintf(ctx.Stderr, "fatal: %v\n", err)
			return ExitFatal
		}
		return ExitSuccess

	case "get-url":
		if len(ctx.Args) < 2 {
			fmt.Fprintln(ctx.Stderr, "用法: gogit remote get-url <名称>")
			return ExitGeneral
		}
		name := ctx.Args[1]
		rm, err := transport.GetRemote(r, name)
		if err != nil {
			fmt.Fprintf(ctx.Stderr, "fatal: %v\n", err)
			return ExitFatal
		}
		fmt.Fprintln(ctx.Stdout, rm.URL)
		return ExitSuccess

	default:
		fmt.Fprintf(ctx.Stderr, "error: 未知的 remote 子命令 '%s'\n", sub)
		return ExitGeneral
	}
}
