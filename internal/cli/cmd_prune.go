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
	Register("prune", cmdPrune)
}

func cmdPrune(ctx *Context) int {
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

	dryRun := false
	verbose := false
	var expire time.Time

	for i := 0; i < len(ctx.Args); i++ {
		arg := ctx.Args[i]
		if arg == "-n" || arg == "--dry-run" {
			dryRun = true
		} else if arg == "-v" || arg == "--verbose" {
			verbose = true
		} else if arg == "--expire" && i+1 < len(ctx.Args) {
			expireVal := ctx.Args[i+1]
			i++
			if expireVal == "now" {
				expire = time.Now()
			}
		} else if strings.HasPrefix(arg, "--expire=") {
			expireVal := strings.TrimPrefix(arg, "--expire=")
			if expireVal == "now" {
				expire = time.Now()
			}
		}
	}

	deleted, freed, err := maintenance.PruneLooseObjects(r, expire, dryRun)
	if err != nil {
		fmt.Fprintf(ctx.Stderr, "fatal: 清理松散对象失败: %v\n", err)
		return ExitFatal
	}

	if verbose || dryRun {
		fmt.Fprintf(ctx.Stdout, "Pruned %d objects (freed %d bytes)\n", deleted, freed)
	}

	return ExitSuccess
}
