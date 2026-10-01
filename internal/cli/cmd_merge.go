package cli

import (
	"fmt"
	"gogit/internal/merge"
	"gogit/internal/repo"
	"gogit/internal/worktree"
	"os"
	"path/filepath"
	"strings"
)

func init() {
	Register("merge", cmdMerge)
}

func cmdMerge(ctx *Context) int {
	abort := false
	noFF := false
	var message string
	var targetBranch string

	for i := 0; i < len(ctx.Args); i++ {
		arg := ctx.Args[i]
		switch {
		case arg == "--abort":
			abort = true
		case arg == "--no-ff":
			noFF = true
		case arg == "-m" && i+1 < len(ctx.Args):
			message = ctx.Args[i+1]
			i++
		case strings.HasPrefix(arg, "-m"):
			message = strings.TrimPrefix(arg, "-m")
		default:
			if !strings.HasPrefix(arg, "-") {
				targetBranch = arg
			}
		}
	}

	r, err := repo.FindRepository(".")
	if err != nil {
		fmt.Fprintf(ctx.Stderr, "fatal: %v\n", err)
		return ExitFatal
	}

	if abort {
		mergeHeadFile := filepath.Join(r.GitDir, "MERGE_HEAD")
		if _, err := os.Stat(mergeHeadFile); os.IsNotExist(err) {
			fmt.Fprintf(ctx.Stderr, "fatal: There is no merge to abort (MERGE_HEAD missing).\n")
			return ExitFatal
		}

		headOID, err := r.Refs.ResolveHEAD()
		if err != nil {
			fmt.Fprintf(ctx.Stderr, "fatal: 无法解析 HEAD: %v\n", err)
			return ExitFatal
		}

		// 还原工作区与索引到 HEAD
		_ = worktree.CheckoutSwitch(r, headOID.String(), worktree.CheckoutOptions{})
		_ = os.Remove(mergeHeadFile)
		_ = os.Remove(filepath.Join(r.GitDir, "MERGE_MSG"))
		_ = os.Remove(filepath.Join(r.GitDir, "MERGE_MODE"))
		return ExitSuccess
	}

	if targetBranch == "" {
		fmt.Fprintf(ctx.Stderr, "fatal: missing branch or commit to merge\n")
		return ExitFatal
	}

	opts := merge.MergeOptions{
		NoFF:    noFF,
		Message: message,
	}

	res, err := merge.DoMerge(r, targetBranch, opts)
	if err != nil {
		fmt.Fprintf(ctx.Stderr, "fatal: %v\n", err)
		return ExitFatal
	}

	switch res.Status {
	case merge.MergeAlreadyUpToDate:
		fmt.Fprintln(ctx.Stdout, res.Message)
		return ExitSuccess
	case merge.MergeFastForward:
		fmt.Fprintln(ctx.Stdout, res.Message)
		return ExitSuccess
	case merge.MergeClean:
		fmt.Fprintf(ctx.Stdout, "Merge made by the 'ort' strategy.\n")
		return ExitSuccess
	case merge.MergeConflictStatus:
		for _, c := range res.Conflicts {
			fmt.Fprintf(ctx.Stdout, "CONFLICT (%s): Merge conflict in %s\n", c.Type, c.Path)
		}
		fmt.Fprintln(ctx.Stderr, res.Message)
		return ExitGeneral
	}

	return ExitSuccess
}
