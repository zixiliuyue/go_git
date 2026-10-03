package cli

import (
	"fmt"
	"gogit/internal/repo"
	"gogit/internal/worktree"
	"strings"
)

func init() {
	Register("status", cmdStatus)
}

func cmdStatus(ctx *Context) int {
	porcelain := false
	untrackedMode := worktree.UntrackedNormal

	for i := 0; i < len(ctx.Args); i++ {
		arg := ctx.Args[i]
		if arg == "--porcelain" || strings.HasPrefix(arg, "--porcelain=") {
			porcelain = true
		} else if arg == "-uall" || arg == "--untracked-files=all" {
			untrackedMode = worktree.UntrackedAll
		} else if arg == "-uno" || arg == "--untracked-files=no" {
			untrackedMode = worktree.UntrackedNo
		} else if arg == "-unormal" || arg == "--untracked-files=normal" || arg == "-u" {
			untrackedMode = worktree.UntrackedNormal
		} else if arg == "--untracked-files" && i+1 < len(ctx.Args) {
			val := ctx.Args[i+1]
			i++
			if val == "all" {
				untrackedMode = worktree.UntrackedAll
			} else if val == "no" {
				untrackedMode = worktree.UntrackedNo
			} else {
				untrackedMode = worktree.UntrackedNormal
			}
		}
	}

	r, err := repo.FindRepository(".")
	if err != nil {
		fmt.Fprintf(ctx.Stderr, "fatal: %v\n", err)
		return ExitFatal
	}

	st, err := worktree.ComputeStatusWithOptions(r, worktree.StatusOptions{Untracked: untrackedMode})
	if err != nil {
		fmt.Fprintf(ctx.Stderr, "fatal: 计算状态失败: %v\n", err)
		return ExitFatal
	}

	if porcelain {
		fmt.Fprint(ctx.Stdout, st.FormatPorcelain())
		return ExitSuccess
	}

	// 人性化长格式输出
	if st.IsDetached {
		fmt.Fprintf(ctx.Stdout, "HEAD detached at %s\n", st.BranchName)
	} else {
		fmt.Fprintf(ctx.Stdout, "On branch %s\n", st.BranchName)
	}

	var staged []string
	var unstaged []string
	var untracked []string

	for _, item := range st.Items {
		if item.IsUntracked {
			untracked = append(untracked, item.Path)
			continue
		}
		if item.Staged != ' ' {
			var action string
			switch item.Staged {
			case 'M':
				action = "modified"
			case 'A':
				action = "new file"
			case 'D':
				action = "deleted"
			case 'R':
				action = "renamed"
			}
			staged = append(staged, fmt.Sprintf("\t%s:   %s", action, item.Path))
		}
		if item.Unstaged != ' ' {
			var action string
			switch item.Unstaged {
			case 'M':
				action = "modified"
			case 'D':
				action = "deleted"
			}
			unstaged = append(unstaged, fmt.Sprintf("\t%s:   %s", action, item.Path))
		}
	}

	if len(staged) > 0 {
		fmt.Fprintln(ctx.Stdout, "Changes to be committed:")
		fmt.Fprintln(ctx.Stdout, "  (use \"gogit restore --staged <file>...\" to unstage)")
		for _, s := range staged {
			fmt.Fprintln(ctx.Stdout, s)
		}
		fmt.Fprintln(ctx.Stdout)
	}

	if len(unstaged) > 0 {
		fmt.Fprintln(ctx.Stdout, "Changes not staged for commit:")
		fmt.Fprintln(ctx.Stdout, "  (use \"gogit add <file>...\" to update what will be committed)")
		fmt.Fprintln(ctx.Stdout, "  (use \"gogit restore <file>...\" to discard changes in working directory)")
		for _, s := range unstaged {
			fmt.Fprintln(ctx.Stdout, s)
		}
		fmt.Fprintln(ctx.Stdout)
	}

	if len(untracked) > 0 {
		fmt.Fprintln(ctx.Stdout, "Untracked files:")
		fmt.Fprintln(ctx.Stdout, "  (use \"gogit add <file>...\" to include in what will be committed)")
		for _, u := range untracked {
			fmt.Fprintf(ctx.Stdout, "\t%s\n", u)
		}
		fmt.Fprintln(ctx.Stdout)
	}

	if len(staged) == 0 && len(unstaged) == 0 && len(untracked) == 0 {
		fmt.Fprintln(ctx.Stdout, "nothing to commit, working tree clean")
	}

	return ExitSuccess
}
