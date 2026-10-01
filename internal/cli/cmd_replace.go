package cli

import (
	"fmt"
	"gogit/internal/object"
	"gogit/internal/repo"
	"gogit/internal/rev"
	"os"
	"strings"
)

func init() {
	Register("replace", cmdReplace)
}

func cmdReplace(ctx *Context) int {
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

	// 1. 无参数或 -l: 列出所有 replace 引用
	if len(ctx.Args) == 0 || (len(ctx.Args) == 1 && ctx.Args[0] == "-l") {
		allRefs, err := r.Refs.ListRefs("refs/replace/")
		if err != nil {
			return ExitSuccess
		}
		for refName := range allRefs {
			orig := strings.TrimPrefix(refName, "refs/replace/")
			fmt.Fprintln(ctx.Stdout, orig)
		}
		return ExitSuccess
	}

	// 2. 删除 replace 引用: -d <object>
	if ctx.Args[0] == "-d" || ctx.Args[0] == "--delete" {
		if len(ctx.Args) < 2 {
			fmt.Fprintln(ctx.Stderr, "fatal: 请指定待删除的 replace 目标对象")
			return ExitUsage
		}
		orig := ctx.Args[1]
		refName := "refs/replace/" + orig
		if err := r.Refs.DeleteRef(refName); err != nil {
			fmt.Fprintf(ctx.Stderr, "fatal: 删除 replace 引用失败: %v\n", err)
			return ExitFatal
		}
		fmt.Fprintf(ctx.Stdout, "Deleted replace ref '%s'\n", orig)
		return ExitSuccess
	}

	// 3. 创建 replace 引用: <object> <replacement>
	if len(ctx.Args) >= 2 {
		origStr := ctx.Args[0]
		repStr := ctx.Args[1]

		origH, err := rev.ParseRevision(r, origStr)
		if err != nil {
			origH, err = object.NewHashFromHex(origStr)
			if err != nil {
				fmt.Fprintf(ctx.Stderr, "fatal: 无效的原对象: %s\n", origStr)
				return ExitFatal
			}
		}

		repH, err := rev.ParseRevision(r, repStr)
		if err != nil {
			repH, err = object.NewHashFromHex(repStr)
			if err != nil {
				fmt.Fprintf(ctx.Stderr, "fatal: 无效的替换对象: %s\n", repStr)
				return ExitFatal
			}
		}

		refName := "refs/replace/" + origH.String()
		sig := r.AuthorSignature()
		if err := r.Refs.UpdateRef(refName, repH, sig, "replace: "+origH.String()); err != nil {
			fmt.Fprintf(ctx.Stderr, "fatal: 创建 replace 引用失败: %v\n", err)
			return ExitFatal
		}
		return ExitSuccess
	}

	fmt.Fprintln(ctx.Stderr, "usage: gogit replace [-l | -d <object> | <object> <replacement>]")
	return ExitUsage
}
