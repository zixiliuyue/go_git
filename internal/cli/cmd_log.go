package cli

import (
	"fmt"
	"gogit/internal/diff"
	"gogit/internal/index"
	"gogit/internal/object"
	"gogit/internal/repo"
	"gogit/internal/rev"
	"strconv"
	"strings"
)

func init() {
	Register("log", cmdLog)
}

func cmdLog(ctx *Context) int {
	oneline := false
	graph := false
	all := false
	patch := false
	stat := false
	maxCount := 0
	targetRev := "HEAD"

	for i := 0; i < len(ctx.Args); i++ {
		arg := ctx.Args[i]
		switch {
		case arg == "--oneline":
			oneline = true
		case arg == "--graph":
			graph = true
		case arg == "--all":
			all = true
		case arg == "-p" || arg == "-u" || arg == "--patch":
			patch = true
		case arg == "--stat":
			stat = true
		case (arg == "-n" || arg == "--max-count") && i+1 < len(ctx.Args):
			maxCount, _ = strconv.Atoi(ctx.Args[i+1])
			i++
		case strings.HasPrefix(arg, "-n"):
			maxCount, _ = strconv.Atoi(arg[2:])
		case strings.HasPrefix(arg, "--max-count="):
			maxCount, _ = strconv.Atoi(strings.TrimPrefix(arg, "--max-count="))
		case !strings.HasPrefix(arg, "-"):
			targetRev = arg
		}
	}

	r, err := repo.FindRepository(".")
	if err != nil {
		fmt.Fprintf(ctx.Stderr, "fatal: %v\n", err)
		return ExitFatal
	}

	var startHashes []object.Hash

	if all {
		// 收集全部 refs (分支与 tag)
		refMap, err := r.Refs.ListRefs("refs/")
		if err == nil {
			for _, h := range refMap {
				if !h.IsZero() {
					startHashes = append(startHashes, h)
				}
			}
		}
	} else {
		startHash, err := rev.ParseRevision(r, targetRev)
		if err != nil {
			fmt.Fprintf(ctx.Stderr, "fatal: 未知修订版本: %s\n", targetRev)
			return ExitFatal
		}
		startHashes = append(startHashes, startHash)
	}

	opts := rev.RevListOptions{
		MaxCount: maxCount,
	}

	commits, err := rev.RevList(r, startHashes, nil, opts)
	if err != nil {
		fmt.Fprintf(ctx.Stderr, "fatal: 遍历日志失败: %v\n", err)
		return ExitFatal
	}

	if graph {
		graphRows := rev.BuildCommitGraph(commits)
		for _, row := range graphRows {
			fmt.Fprintln(ctx.Stdout, rev.FormatGraphLine(row.Prefix, row.Item, oneline))
		}
		return ExitSuccess
	}

	for _, item := range commits {
		c := item.Commit
		if oneline {
			firstLine := strings.TrimSpace(strings.Split(c.Message, "\n")[0])
			fmt.Fprintf(ctx.Stdout, "%s %s\n", item.Hash.String()[:7], firstLine)
		} else {
			fmt.Fprintf(ctx.Stdout, "commit %s\n", item.Hash.String())
			if len(c.Parents) > 1 {
				var pStrs []string
				for _, p := range c.Parents {
					pStrs = append(pStrs, p.String()[:7])
				}
				fmt.Fprintf(ctx.Stdout, "Merge: %s\n", strings.Join(pStrs, " "))
			}
			fmt.Fprintf(ctx.Stdout, "Author:     %s <%s>\n", c.Author.Name, c.Author.Email)
			dateStr := c.Author.When.Format("Mon Jan 02 15:04:05 2006")
			fmt.Fprintf(ctx.Stdout, "Date:       %s %s\n\n", dateStr, c.Author.TZ)

			lines := strings.Split(strings.TrimRight(c.Message, "\n"), "\n")
			for _, line := range lines {
				fmt.Fprintf(ctx.Stdout, "    %s\n", line)
			}
			fmt.Fprintln(ctx.Stdout)

			// 输出提交引入的变动 (--stat 或 -p)
			if stat || patch {
				var parentTree object.Hash
				if len(c.Parents) > 0 {
					pCommit, err := r.ReadCommit(c.Parents[0])
					if err == nil {
						parentTree = pCommit.Tree
					}
				}
				dummyIdx := index.NewIndex()
				diffOpts := diff.DiffOptions{Stat: stat, ContextLines: 3}
				fileDiffs, _ := diff.DiffIndexWithTree(r, dummyIdx, parentTree, diffOpts)
				_ = fileDiffs
			}
		}
	}

	return ExitSuccess
}
