package cli

import (
	"fmt"
	"gogit/internal/history"
	"gogit/internal/repo"
	"strings"
)

func init() {
	Register("tag", cmdTag)
}

func cmdTag(ctx *Context) int {
	r, err := repo.FindRepository(".")
	if err != nil {
		fmt.Fprintf(ctx.Stderr, "fatal: %v\n", err)
		return ExitFatal
	}

	annotated := false
	deleteTag := false
	var message string
	var deleteName string
	var tagName string
	var targetCommit string

	for i := 0; i < len(ctx.Args); i++ {
		arg := ctx.Args[i]
		switch {
		case arg == "-l" || arg == "--list":
			// 列表模式
		case arg == "-a":
			annotated = true
		case arg == "-m" && i+1 < len(ctx.Args):
			message = ctx.Args[i+1]
			i++
		case strings.HasPrefix(arg, "-m"):
			message = strings.TrimPrefix(arg, "-m")
		case arg == "-d" && i+1 < len(ctx.Args):
			deleteTag = true
			deleteName = ctx.Args[i+1]
			i++
		default:
			if !strings.HasPrefix(arg, "-") {
				if tagName == "" {
					tagName = arg
				} else if targetCommit == "" {
					targetCommit = arg
				}
			}
		}
	}

	if deleteTag {
		if err := history.DeleteTag(r, deleteName); err != nil {
			fmt.Fprintf(ctx.Stderr, "error: %v\n", err)
			return ExitGeneral
		}
		fmt.Fprintf(ctx.Stdout, "Deleted tag '%s'\n", deleteName)
		return ExitSuccess
	}

	if tagName == "" {
		tags, err := history.ListTags(r)
		if err != nil {
			fmt.Fprintf(ctx.Stderr, "fatal: %v\n", err)
			return ExitFatal
		}
		for _, t := range tags {
			fmt.Fprintln(ctx.Stdout, t)
		}
		return ExitSuccess
	}

	opts := history.TagOptions{
		Annotated: annotated,
		Message:   message,
		Target:    targetCommit,
	}

	_, err = history.CreateTag(r, tagName, opts)
	if err != nil {
		fmt.Fprintf(ctx.Stderr, "%v\n", err)
		return ExitGeneral
	}

	return ExitSuccess
}
