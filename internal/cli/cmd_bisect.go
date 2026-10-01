package cli

import (
	"fmt"
	"gogit/internal/history"
	"gogit/internal/repo"
)

func init() {
	Register("bisect", cmdBisect)
}

func cmdBisect(ctx *Context) int {
	if len(ctx.Args) == 0 {
		fmt.Fprintln(ctx.Stderr, "usage: gogit bisect [start|bad|good|reset|run]")
		return ExitError
	}

	r, err := repo.FindRepository(".")
	if err != nil {
		fmt.Fprintf(ctx.Stderr, "fatal: %v\n", err)
		return ExitFatal
	}

	sub := ctx.Args[0]
	rem := ctx.Args[1:]

	switch sub {
	case "start":
		badStr := ""
		goodStr := ""
		if len(rem) > 0 {
			badStr = rem[0]
		}
		if len(rem) > 1 {
			goodStr = rem[1]
		}
		if err := history.BisectStart(r, badStr, goodStr); err != nil {
			fmt.Fprintf(ctx.Stderr, "fatal: %v\n", err)
			return ExitFatal
		}
		return ExitSuccess

	case "bad":
		commitStr := ""
		if len(rem) > 0 {
			commitStr = rem[0]
		}
		res, err := history.BisectMark(r, "bad", commitStr)
		if err != nil {
			fmt.Fprintf(ctx.Stderr, "fatal: %v\n", err)
			return ExitFatal
		}
		if res.Message != "" {
			fmt.Fprintln(ctx.Stdout, res.Message)
		}
		return ExitSuccess

	case "good":
		commitStr := ""
		if len(rem) > 0 {
			commitStr = rem[0]
		}
		res, err := history.BisectMark(r, "good", commitStr)
		if err != nil {
			fmt.Fprintf(ctx.Stderr, "fatal: %v\n", err)
			return ExitFatal
		}
		if res.Message != "" {
			fmt.Fprintln(ctx.Stdout, res.Message)
		}
		return ExitSuccess

	case "reset":
		target := ""
		if len(rem) > 0 {
			target = rem[0]
		}
		if err := history.BisectReset(r, target); err != nil {
			fmt.Fprintf(ctx.Stderr, "fatal: %v\n", err)
			return ExitFatal
		}
		return ExitSuccess

	case "run":
		if len(rem) == 0 {
			fmt.Fprintln(ctx.Stderr, "fatal: bisect run requires a command")
			return ExitError
		}
		res, err := history.BisectRun(r, rem[0], rem[1:])
		if err != nil {
			fmt.Fprintf(ctx.Stderr, "fatal: %v\n", err)
			return ExitFatal
		}
		if res.Message != "" {
			fmt.Fprintln(ctx.Stdout, res.Message)
		}
		return ExitSuccess
	}

	fmt.Fprintf(ctx.Stderr, "error: unknown sub-command %s\n", sub)
	return ExitError
}
