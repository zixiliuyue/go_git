package cli

import (
	"fmt"
	"gogit/internal/object"
	"gogit/internal/repo"
	"gogit/internal/rev"
	"os"
	"strconv"
	"strings"
)

func init() {
	Register("rev-list", cmdRevList)
}

// cmdRevList 实现 git rev-list 核心能力（提交遍历与计数）。
func cmdRevList(ctx *Context) int {
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

	var countMode bool
	var opts rev.RevListOptions
	var revisions []string

	for i := 0; i < len(ctx.Args); i++ {
		arg := ctx.Args[i]
		switch {
		case arg == "--count":
			countMode = true
		case arg == "--all":
			opts.All = true
		case arg == "--topo-order":
			opts.TopoOrder = true
		case arg == "--date-order":
			opts.DateOrder = true
		case strings.HasPrefix(arg, "--max-count="):
			n, _ := strconv.Atoi(strings.TrimPrefix(arg, "--max-count="))
			opts.MaxCount = n
		case arg == "-n" && i+1 < len(ctx.Args):
			i++
			n, _ := strconv.Atoi(ctx.Args[i])
			opts.MaxCount = n
		case strings.HasPrefix(arg, "-"):
			// 忽略不支持的扩展选项或提示
		default:
			revisions = append(revisions, arg)
		}
	}

	var includes []object.Hash
	var excludes []object.Hash

	if opts.All {
		refMap, err := r.Refs.ListRefs("refs/")
		if err == nil {
			for _, h := range refMap {
				if !h.IsZero() {
					includes = append(includes, h)
				}
			}
		}
	}

	if len(revisions) == 0 && !opts.All {
		revisions = append(revisions, "HEAD")
	}

	for _, spec := range revisions {
		if strings.Contains(spec, "..") {
			parts := strings.SplitN(spec, "..", 2)
			if parts[0] != "" {
				if h, err := rev.ParseRevision(r, parts[0]); err == nil {
					excludes = append(excludes, h)
				}
			}
			if parts[1] != "" {
				if h, err := rev.ParseRevision(r, parts[1]); err == nil {
					includes = append(includes, h)
				}
			}
		} else if strings.HasPrefix(spec, "^") {
			if h, err := rev.ParseRevision(r, spec[1:]); err == nil {
				excludes = append(excludes, h)
			}
		} else {
			if h, err := rev.ParseRevision(r, spec); err == nil {
				includes = append(includes, h)
			} else {
				fmt.Fprintf(ctx.Stderr, "fatal: 无法解析引用 '%s': %v\n", spec, err)
				return ExitFatal
			}
		}
	}

	items, err := rev.RevList(r, includes, excludes, opts)
	if err != nil {
		fmt.Fprintf(ctx.Stderr, "fatal: 遍历提交失败: %v\n", err)
		return ExitFatal
	}

	if countMode {
		fmt.Fprintf(ctx.Stdout, "%d\n", len(items))
	} else {
		for _, item := range items {
			fmt.Fprintln(ctx.Stdout, item.Hash.String())
		}
	}

	return ExitSuccess
}
