package cli

import (
	"fmt"
	"gogit/internal/pack"
)

func init() {
	Register("verify-pack", cmdVerifyPack)
}

func cmdVerifyPack(ctx *Context) int {
	if len(ctx.Args) == 0 {
		fmt.Fprintln(ctx.Stderr, "usage: gogit verify-pack [-v] [-s] <packfile...>")
		return ExitUsage
	}

	verbose := false
	statOnly := false
	var targetFiles []string

	for _, arg := range ctx.Args {
		if arg == "-v" || arg == "--verbose" {
			verbose = true
		} else if arg == "-s" || arg == "--stat-only" {
			statOnly = true
		} else {
			targetFiles = append(targetFiles, arg)
		}
	}

	if len(targetFiles) == 0 {
		fmt.Fprintln(ctx.Stderr, "fatal: 未指定 pack 文件")
		return ExitUsage
	}

	hasError := false
	for _, f := range targetFiles {
		res, err := pack.VerifyPackFile(f, verbose)
		if err != nil {
			fmt.Fprintf(ctx.Stderr, "%s: 校验失败: %v\n", f, err)
			hasError = true
			continue
		}

		if verbose && !statOnly {
			pack.FormatVerboseStat(ctx.Stdout, res)
		} else if !statOnly {
			fmt.Fprintf(ctx.Stdout, "%s: ok\n", f)
		}
	}

	if hasError {
		return ExitError
	}
	return ExitSuccess
}
