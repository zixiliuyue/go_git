package cli

import (
	"fmt"
	"gogit/internal/merge"
	"gogit/internal/repo"
	"os"
	"strings"
)

func init() {
	Register("pull", cmdPull)
}

func cmdPull(ctx *Context) int {
	cwd, err := os.Getwd()
	if err != nil {
		fmt.Fprintf(ctx.Stderr, "fatal: 无法获取工作目录: %v\n", err)
		return ExitFatal
	}

	r, err := repo.FindRepository(cwd)
	if err != nil {
		fmt.Fprintf(ctx.Stderr, "fatal: 不是 git 仓库: %v\n", err)
		return ExitFatal
	}

	// 1. 先执行 fetch
	fetchExit := cmdFetch(ctx)
	if fetchExit != ExitSuccess {
		return fetchExit
	}

	// 2. 获取当前分支
	headRef, err := r.Refs.ReadHEAD()
	if err != nil || !headRef.IsSymref {
		fmt.Fprintln(ctx.Stderr, "fatal: 您当前未处于任何分支上。")
		return ExitFatal
	}

	currentBranch := strings.TrimPrefix(headRef.Target, "refs/heads/")
	remoteName := "origin"
	targetBranch := currentBranch

	if len(ctx.Args) > 0 && !strings.HasPrefix(ctx.Args[0], "-") {
		remoteName = ctx.Args[0]
	}
	if len(ctx.Args) > 1 && !strings.HasPrefix(ctx.Args[1], "-") {
		targetBranch = ctx.Args[1]
	}

	remoteTrackingRef := fmt.Sprintf("refs/remotes/%s/%s", remoteName, targetBranch)
	if _, err := r.Refs.GetRef(remoteTrackingRef); err != nil {
		fmt.Fprintf(ctx.Stderr, "fatal: 未找到远程跟踪分支 '%s'\n", remoteTrackingRef)
		return ExitFatal
	}

	// 3. 执行合并
	res, err := merge.DoMerge(r, remoteTrackingRef, merge.MergeOptions{})
	if err != nil {
		fmt.Fprintf(ctx.Stderr, "fatal: 合并失败: %v\n", err)
		return ExitFatal
	}

	switch res.Status {
	case merge.MergeAlreadyUpToDate:
		fmt.Fprintln(ctx.Stdout, "Already up to date.")
	case merge.MergeFastForward:
		fmt.Fprintf(ctx.Stdout, "Updating %s..%s\nFast-forward\n", headRef.Hash.Short(), res.FastForwardTo.Short())
	case merge.MergeClean:
		fmt.Fprintf(ctx.Stdout, "Merge made by the 'ort' strategy.\n")
	case merge.MergeConflictStatus:
		fmt.Fprintln(ctx.Stderr, "CONFLICT (content): Merge conflict in files")
		fmt.Fprintln(ctx.Stderr, "Automatic merge failed; fix conflicts and then commit the result.")
		return ExitGeneral
	}

	return ExitSuccess
}
