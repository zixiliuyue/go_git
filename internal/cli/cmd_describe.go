package cli

import (
	"fmt"
	"gogit/internal/history"
	"gogit/internal/repo"
	"strings"
)

func init() {
	Register("describe", cmdDescribe)
}

func cmdDescribe(ctx *Context) int {
	r, err := repo.FindRepository(".")
	if err != nil {
		fmt.Fprintf(ctx.Stderr, "fatal: %v\n", err)
		return ExitFatal
	}

	tags := false
	always := false
	var targetCommit string

	for _, arg := range ctx.Args {
		switch {
		case arg == "--tags":
			tags = true
		case arg == "--always":
			always = true
		default:
			if !strings.HasPrefix(arg, "-") && targetCommit == "" {
				targetCommit = arg
			}
		}
	}

	opts := history.DescribeOptions{
		Tags:   tags,
		Always: always,
		Abbrev: 7,
	}

	desc, err := history.Describe(r, targetCommit, opts)
	if err != nil {
		fmt.Fprintf(ctx.Stderr, "%v\n", err)
		return ExitGeneral
	}

	fmt.Fprintln(ctx.Stdout, desc)
	return ExitSuccess
}
