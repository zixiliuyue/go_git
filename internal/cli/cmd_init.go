package cli

import (
	"fmt"
	"gogit/internal/repo"
	"strings"
)

func init() {
	Register("init", cmdInit)
}

func cmdInit(ctx *Context) int {
	bare := false
	initialBranch := "master"
	targetDir := "."

	for i := 0; i < len(ctx.Args); i++ {
		arg := ctx.Args[i]
		if arg == "--bare" {
			bare = true
		} else if arg == "-b" && i+1 < len(ctx.Args) {
			initialBranch = ctx.Args[i+1]
			i++
		} else if strings.HasPrefix(arg, "--initial-branch=") {
			initialBranch = strings.TrimPrefix(arg, "--initial-branch=")
		} else if !strings.HasPrefix(arg, "-") {
			targetDir = arg
		}
	}

	r, err := repo.InitRepository(targetDir, bare, initialBranch)
	if err != nil {
		fmt.Fprintf(ctx.Stderr, "fatal: 初始化仓库失败: %v\n", err)
		return ExitFatal
	}

	if r.IsBare {
		fmt.Fprintf(ctx.Stdout, "Initialized empty Git repository in %s\n", r.GitDir)
	} else {
		fmt.Fprintf(ctx.Stdout, "Initialized empty Git repository in %s/\n", r.GitDir)
	}
	return ExitSuccess
}
