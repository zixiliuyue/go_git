package cli

import (
	"fmt"
	"gogit/internal/diff"
	"gogit/internal/object"
	"gogit/internal/repo"
	"gogit/internal/rev"
	"strings"
)

func init() {
	Register("diff", cmdDiff)
}

func cmdDiff(ctx *Context) int {
	var opts diff.DiffOptions
	var revArgs []string

	for _, arg := range ctx.Args {
		switch {
		case arg == "--cached" || arg == "--staged":
			opts.Cached = true
		case arg == "--stat":
			opts.Stat = true
		case arg == "--numstat":
			opts.NumStat = true
		case arg == "--name-status":
			opts.NameStatus = true
		case arg == "--name-only":
			opts.NameOnly = true
		case arg == "-M" || arg == "--find-renames":
			opts.DetectRenames = true
		case strings.HasPrefix(arg, "--diff-filter="):
			opts.DiffFilter = strings.TrimPrefix(arg, "--diff-filter=")
		default:
			if !strings.HasPrefix(arg, "-") {
				revArgs = append(revArgs, arg)
			}
		}
	}

	r, err := repo.FindRepository(".")
	if err != nil {
		fmt.Fprintf(ctx.Stderr, "fatal: %v\n", err)
		return ExitFatal
	}

	idx, err := r.GetIndex()
	if err != nil {
		fmt.Fprintf(ctx.Stderr, "fatal: 获取索引失败: %v\n", err)
		return ExitFatal
	}

	var fileDiffs []*diff.FileDiff

	if opts.Cached {
		// 暂存区 vs HEAD 树
		var headTreeHash object.Hash
		headOID, err := r.Refs.ResolveHEAD()
		if err == nil && !headOID.IsZero() {
			c, err := r.ReadCommit(headOID)
			if err == nil {
				headTreeHash = c.Tree
			}
		}
		fileDiffs, err = diff.DiffIndexWithTree(r, idx, headTreeHash, opts)
		if err != nil {
			fmt.Fprintf(ctx.Stderr, "fatal: 计算暂存区 diff 失败: %v\n", err)
			return ExitFatal
		}
	} else if len(revArgs) > 0 {
		// 指定 commit/tree 比较
		targetOID, err := rev.ParseRevision(r, revArgs[0])
		if err != nil {
			fmt.Fprintf(ctx.Stderr, "fatal: 解析修订版本 %s 失败: %v\n", revArgs[0], err)
			return ExitFatal
		}
		c, err := r.ReadCommit(targetOID)
		if err != nil {
			fmt.Fprintf(ctx.Stderr, "fatal: 读取提交失败: %v\n", err)
			return ExitFatal
		}
		fileDiffs, err = diff.DiffIndexWithTree(r, idx, c.Tree, opts)
		if err != nil {
			fmt.Fprintf(ctx.Stderr, "fatal: 计算树间 diff 失败: %v\n", err)
			return ExitFatal
		}
	} else {
		// 工作区 vs 暂存区
		fileDiffs, err = diff.DiffWorktreeWithIndex(r, idx, opts)
		if err != nil {
			fmt.Fprintf(ctx.Stderr, "fatal: 计算工作区 diff 失败: %v\n", err)
			return ExitFatal
		}
	}

	output := diff.FormatDiff(fileDiffs, opts)
	if output != "" {
		fmt.Fprint(ctx.Stdout, output)
	}

	return ExitSuccess
}
