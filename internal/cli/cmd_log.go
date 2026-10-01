package cli

import (
	"fmt"
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
	maxCount := 0
	targetRev := "HEAD"

	for i := 0; i < len(ctx.Args); i++ {
		arg := ctx.Args[i]
		if arg == "--oneline" {
			oneline = true
		} else if (arg == "-n" || arg == "--max-count") && i+1 < len(ctx.Args) {
			maxCount, _ = strconv.Atoi(ctx.Args[i+1])
			i++
		} else if strings.HasPrefix(arg, "-n") {
			maxCount, _ = strconv.Atoi(arg[2:])
		} else if strings.HasPrefix(arg, "--max-count=") {
			maxCount, _ = strconv.Atoi(strings.TrimPrefix(arg, "--max-count="))
		} else if !strings.HasPrefix(arg, "-") {
			targetRev = arg
		}
	}

	r, err := repo.FindRepository(".")
	if err != nil {
		fmt.Fprintf(ctx.Stderr, "fatal: %v\n", err)
		return ExitFatal
	}

	startHash, err := rev.ParseRevision(r, targetRev)
	if err != nil {
		fmt.Fprintf(ctx.Stderr, "fatal: 未知修订版本: %s\n", targetRev)
		return ExitFatal
	}

	opts := rev.RevListOptions{
		MaxCount: maxCount,
	}

	commits, err := rev.RevList(r, []object.Hash{startHash}, nil, opts)
	if err != nil {
		fmt.Fprintf(ctx.Stderr, "fatal: 遍历日志失败: %v\n", err)
		return ExitFatal
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
			// 标准 Git 提交时间展示格式: "Mon Jan 02 15:04:05 2006 -0700"
			dateStr := c.Author.When.Format("Mon Jan 02 15:04:05 2006")
			fmt.Fprintf(ctx.Stdout, "Date:       %s %s\n\n", dateStr, c.Author.TZ)

			lines := strings.Split(strings.TrimRight(c.Message, "\n"), "\n")
			for _, line := range lines {
				fmt.Fprintf(ctx.Stdout, "    %s\n", line)
			}
			fmt.Fprintln(ctx.Stdout)
		}
	}

	return ExitSuccess
}
