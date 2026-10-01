package cli

import (
	"fmt"
	"gogit/internal/history"
	"gogit/internal/repo"
	"strings"
)

func init() {
	Register("notes", cmdNotes)
}

func cmdNotes(ctx *Context) int {
	r, err := repo.FindRepository(".")
	if err != nil {
		fmt.Fprintf(ctx.Stderr, "fatal: %v\n", err)
		return ExitFatal
	}

	sub := "list"
	var rem []string
	if len(ctx.Args) > 0 {
		sub = ctx.Args[0]
		rem = ctx.Args[1:]
	}

	switch sub {
	case "list":
		m, err := history.ListNotes(r)
		if err != nil {
			fmt.Fprintf(ctx.Stderr, "fatal: %v\n", err)
			return ExitFatal
		}
		for c, blob := range m {
			fmt.Fprintf(ctx.Stdout, "%s %s\n", blob.String(), c.String())
		}
		return ExitSuccess

	case "show":
		target := "HEAD"
		if len(rem) > 0 {
			target = rem[0]
		}
		content, err := history.ShowNote(r, target)
		if err != nil {
			fmt.Fprintf(ctx.Stderr, "error: %v\n", err)
			return ExitGeneral
		}
		fmt.Fprint(ctx.Stdout, content)
		return ExitSuccess

	case "add":
		var message string
		var target string
		for i := 0; i < len(rem); i++ {
			arg := rem[i]
			switch {
			case arg == "-m" && i+1 < len(rem):
				message = rem[i+1]
				i++
			case strings.HasPrefix(arg, "-m"):
				message = strings.TrimPrefix(arg, "-m")
			default:
				if !strings.HasPrefix(arg, "-") && target == "" {
					target = arg
				}
			}
		}

		if target == "" {
			target = "HEAD"
		}
		if message == "" {
			fmt.Fprintln(ctx.Stderr, "fatal: no note message specified (-m <msg>)")
			return ExitError
		}

		if err := history.AddNote(r, target, message); err != nil {
			fmt.Fprintf(ctx.Stderr, "fatal: %v\n", err)
			return ExitFatal
		}
		return ExitSuccess
	}

	fmt.Fprintf(ctx.Stderr, "error: unknown sub-command %s\n", sub)
	return ExitError
}
