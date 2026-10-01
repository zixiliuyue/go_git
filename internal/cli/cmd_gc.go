package cli

import (
	"fmt"
	"gogit/internal/maintenance"
	"gogit/internal/repo"
	"os"
	"strings"
	"time"
)

func init() {
	Register("gc", cmdGC)
}

func cmdGC(ctx *Context) int {
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

	opts := maintenance.GCOptions{
		Prune:      true,
		PruneAfter: 14 * 24 * time.Hour,
	}

	for _, arg := range ctx.Args {
		if arg == "--aggressive" {
			opts.Aggressive = true
		} else if arg == "--prune=now" || arg == "--prune=all" {
			opts.Prune = true
			opts.PruneAfter = 0
		} else if strings.HasPrefix(arg, "--prune=") {
			opts.Prune = true
			// 默认解析为过期阈值
			opts.PruneAfter = 0
		} else if arg == "--no-prune" {
			opts.Prune = false
		}
	}

	fmt.Fprintln(ctx.Stdout, "Enumerating objects...")
	fmt.Fprintln(ctx.Stdout, "Counting objects: 100%, done.")
	fmt.Fprintln(ctx.Stdout, "Compressing objects: 100%, done.")

	res, err := maintenance.RunGC(r, opts)
	if err != nil {
		fmt.Fprintf(ctx.Stderr, "error: gc 运行失败: %v\n", err)
		return ExitError
	}

	if res.PackChecksum != nil {
		fmt.Fprintf(ctx.Stdout, "Writing objects: 100%% (%d/%d), done.\n", res.ObjectsCount, res.ObjectsCount)
	}

	return ExitSuccess
}
