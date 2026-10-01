package cli

import (
	"fmt"
	"gogit/internal/history"
	"gogit/internal/repo"
	"strconv"
	"strings"
)

func init() {
	Register("blame", cmdBlame)
}

func cmdBlame(ctx *Context) int {
	r, err := repo.FindRepository(".")
	if err != nil {
		fmt.Fprintf(ctx.Stderr, "fatal: %v\n", err)
		return ExitFatal
	}

	var filePath string
	startLine := 0
	endLine := 0

	for i := 0; i < len(ctx.Args); i++ {
		arg := ctx.Args[i]
		switch {
		case arg == "-L" && i+1 < len(ctx.Args):
			rangeStr := ctx.Args[i+1]
			parts := strings.Split(rangeStr, ",")
			if len(parts) >= 1 {
				startLine, _ = strconv.Atoi(parts[0])
			}
			if len(parts) >= 2 {
				endLine, _ = strconv.Atoi(parts[1])
			}
			i++
		case strings.HasPrefix(arg, "-L"):
			rangeStr := strings.TrimPrefix(arg, "-L")
			parts := strings.Split(rangeStr, ",")
			if len(parts) >= 1 {
				startLine, _ = strconv.Atoi(parts[0])
			}
			if len(parts) >= 2 {
				endLine, _ = strconv.Atoi(parts[1])
			}
		default:
			if !strings.HasPrefix(arg, "-") && filePath == "" {
				filePath = arg
			}
		}
	}

	if filePath == "" {
		fmt.Fprintln(ctx.Stderr, "fatal: no file specified")
		return ExitFatal
	}

	lines, err := history.Blame(r, filePath, "HEAD", startLine, endLine)
	if err != nil {
		fmt.Fprintf(ctx.Stderr, "fatal: %v\n", err)
		return ExitFatal
	}

	for _, l := range lines {
		shortHash := l.CommitOID.String()[:8]
		dateStr := l.AuthorTime.Format("2006-01-02 15:04:05 -0700")
		fmt.Fprintf(ctx.Stdout, "%s (%s %s %d) %s\n", shortHash, l.Author, dateStr, l.LineNum, l.Content)
	}

	return ExitSuccess
}
