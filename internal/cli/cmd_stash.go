package cli

import (
	"fmt"
	"gogit/internal/history"
	"gogit/internal/merge"
	"gogit/internal/repo"
	"strings"
)

func init() {
	Register("stash", cmdStash)
}

func cmdStash(ctx *Context) int {
	r, err := repo.FindRepository(".")
	if err != nil {
		fmt.Fprintf(ctx.Stderr, "fatal: %v\n", err)
		return ExitFatal
	}

	subcmd := "push"
	var remainingArgs []string
	if len(ctx.Args) > 0 {
		first := ctx.Args[0]
		switch first {
		case "list", "pop", "apply", "drop", "show", "push":
			subcmd = first
			remainingArgs = ctx.Args[1:]
		default:
			if strings.HasPrefix(first, "-") {
				subcmd = "push"
				remainingArgs = ctx.Args
			} else {
				subcmd = "push"
				remainingArgs = ctx.Args
			}
		}
	}

	switch subcmd {
	case "list":
		entries, err := history.StashList(r)
		if err != nil {
			fmt.Fprintf(ctx.Stderr, "fatal: %v\n", err)
			return ExitFatal
		}
		for _, e := range entries {
			fmt.Fprintf(ctx.Stdout, "stash@{%d}: %s\n", e.Index, e.Message)
		}
		return ExitSuccess

	case "push":
		includeUntracked := false
		var msg string
		for i := 0; i < len(remainingArgs); i++ {
			arg := remainingArgs[i]
			if arg == "-u" || arg == "--include-untracked" {
				includeUntracked = true
			} else if arg == "-m" && i+1 < len(remainingArgs) {
				msg = remainingArgs[i+1]
				i++
			} else if strings.HasPrefix(arg, "-m") {
				msg = strings.TrimPrefix(arg, "-m")
			}
		}
		stashOID, err := history.StashPush(r, msg, includeUntracked)
		if err != nil {
			fmt.Fprintln(ctx.Stderr, err.Error())
			return ExitGeneral
		}
		fmt.Fprintf(ctx.Stdout, "Saved working directory and index state WIP on %s: %s\n", stashOID.String()[:7], msg)
		return ExitSuccess

	case "pop":
		spec := ""
		if len(remainingArgs) > 0 {
			spec = remainingArgs[0]
		}
		res, err := history.StashPop(r, spec)
		if err != nil {
			fmt.Fprintf(ctx.Stderr, "fatal: %v\n", err)
			return ExitFatal
		}
		if res.Status == merge.MergeConflictStatus {
			for _, c := range res.Conflicts {
				fmt.Fprintf(ctx.Stdout, "CONFLICT (%s): Merge conflict in %s\n", c.Type, c.Path)
			}
			return ExitGeneral
		}
		fmt.Fprintln(ctx.Stdout, res.Message)
		return ExitSuccess

	case "apply":
		spec := ""
		if len(remainingArgs) > 0 {
			spec = remainingArgs[0]
		}
		res, err := history.StashApply(r, spec)
		if err != nil {
			fmt.Fprintf(ctx.Stderr, "fatal: %v\n", err)
			return ExitFatal
		}
		if res.Status == merge.MergeConflictStatus {
			for _, c := range res.Conflicts {
				fmt.Fprintf(ctx.Stdout, "CONFLICT (%s): Merge conflict in %s\n", c.Type, c.Path)
			}
			return ExitGeneral
		}
		fmt.Fprintln(ctx.Stdout, res.Message)
		return ExitSuccess

	case "drop":
		spec := ""
		if len(remainingArgs) > 0 {
			spec = remainingArgs[0]
		}
		if err := history.StashDrop(r, spec); err != nil {
			fmt.Fprintf(ctx.Stderr, "fatal: %v\n", err)
			return ExitFatal
		}
		fmt.Fprintf(ctx.Stdout, "Dropped %s\n", spec)
		return ExitSuccess

	case "show":
		spec := ""
		if len(remainingArgs) > 0 {
			spec = remainingArgs[0]
		}
		diffOut, err := history.StashShow(r, spec)
		if err != nil {
			fmt.Fprintf(ctx.Stderr, "fatal: %v\n", err)
			return ExitFatal
		}
		fmt.Fprint(ctx.Stdout, diffOut)
		return ExitSuccess
	}

	return ExitSuccess
}
