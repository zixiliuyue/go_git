package cli

import (
	"fmt"
	"gogit/internal/pack"
	"gogit/internal/repo"
	"os"
	"path/filepath"
)

func init() {
	Register("multi-pack-index", cmdMIDX)
}

func cmdMIDX(ctx *Context) int {
	if len(ctx.Args) == 0 {
		fmt.Fprintln(ctx.Stderr, "usage: gogit multi-pack-index [write|verify]")
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

	packDir := filepath.Join(r.ObjectsDir, "pack")
	action := ctx.Args[0]

	switch action {
	case "write":
		midxPath, err := pack.WriteMIDX(packDir)
		if err != nil {
			fmt.Fprintf(ctx.Stderr, "error: 写入 multi-pack-index 失败: %v\n", err)
			return ExitError
		}
		fmt.Fprintf(ctx.Stdout, "Written %s\n", midxPath)
		return ExitSuccess

	case "verify":
		midxPath := filepath.Join(packDir, "multi-pack-index")
		if err := pack.VerifyMIDX(midxPath); err != nil {
			fmt.Fprintf(ctx.Stderr, "error: 校验 multi-pack-index 失败: %v\n", err)
			return ExitError
		}
		fmt.Fprintln(ctx.Stdout, "Verifying multi-pack-index: ok")
		return ExitSuccess

	default:
		fmt.Fprintf(ctx.Stderr, "fatal: 未知动作 '%s'\n", action)
		return ExitUsage
	}
}
