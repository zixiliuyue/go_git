package cli

import (
	"fmt"
	"gogit/internal/maintenance"
	"gogit/internal/repo"
	"os"
)

func init() {
	Register("repack", cmdRepack)
}

func cmdRepack(ctx *Context) int {
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

	opts := maintenance.RepackOptions{}
	for _, arg := range ctx.Args {
		if arg == "-a" || arg == "-A" {
			opts.All = true
		} else if arg == "-d" {
			opts.DeleteRedundant = true
		} else if arg == "-ad" || arg == "-a -d" {
			opts.All = true
			opts.DeleteRedundant = true
		}
	}

	chk, count, err := maintenance.Repack(r, opts)
	if err != nil {
		fmt.Fprintf(ctx.Stderr, "fatal: repack 失败: %v\n", err)
		return ExitFatal
	}

	if chk != nil {
		fmt.Fprintf(ctx.Stdout, "Repacked %d objects into pack-%s\n", count, chk.String())
	}

	return ExitSuccess
}
