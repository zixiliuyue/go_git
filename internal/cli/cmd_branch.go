package cli

import (
	"fmt"
	"gogit/internal/repo"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

func init() {
	Register("branch", cmdBranch)
}

func cmdBranch(ctx *Context) int {
	r, err := repo.FindRepository(".")
	if err != nil {
		fmt.Fprintf(ctx.Stderr, "fatal: %v\n", err)
		return ExitFatal
	}

	args := ctx.Args
	if len(args) == 0 || (len(args) == 1 && args[0] == "-a") {
		return listBranches(ctx, r, len(args) > 0 && args[0] == "-a")
	}

	if args[0] == "-d" || args[0] == "-D" {
		if len(args) < 2 {
			fmt.Fprintf(ctx.Stderr, "fatal: branch name required\n")
			return ExitFatal
		}
		targetBranch := args[1]
		headRef, err := r.Refs.ReadHEAD()
		if err == nil && headRef.IsSymref && headRef.Target == "refs/heads/"+targetBranch {
			fmt.Fprintf(ctx.Stderr, "error: Cannot delete branch '%s' checked out at '%s'\n", targetBranch, r.WorkTree)
			return ExitError
		}

		branchFile := filepath.Join(r.GitDir, "refs", "heads", targetBranch)
		if err := os.Remove(branchFile); err != nil {
			fmt.Fprintf(ctx.Stderr, "error: branch '%s' not found.\n", targetBranch)
			return ExitError
		}
		fmt.Fprintf(ctx.Stdout, "Deleted branch %s.\n", targetBranch)
		return ExitSuccess
	}

	if args[0] == "-m" {
		if len(args) < 3 {
			fmt.Fprintf(ctx.Stderr, "fatal: old and new branch names required\n")
			return ExitFatal
		}
		oldName := args[1]
		newName := args[2]
		oldFile := filepath.Join(r.GitDir, "refs", "heads", oldName)
		newFile := filepath.Join(r.GitDir, "refs", "heads", newName)

		data, err := os.ReadFile(oldFile)
		if err != nil {
			fmt.Fprintf(ctx.Stderr, "error: ref '%s' does not exist\n", oldName)
			return ExitError
		}
		if err := os.WriteFile(newFile, data, 0644); err != nil {
			fmt.Fprintf(ctx.Stderr, "fatal: 写入新分支失败: %v\n", err)
			return ExitFatal
		}
		_ = os.Remove(oldFile)

		// 若当前正处于 oldName，更新 HEAD
		headRef, err := r.Refs.ReadHEAD()
		if err == nil && headRef.IsSymref && headRef.Target == "refs/heads/"+oldName {
			_ = r.Refs.SetHEADSymbolic("refs/heads/" + newName)
		}
		return ExitSuccess
	}

	// 创建新分支
	newBranchName := args[0]
	headOID, err := r.Refs.ResolveHEAD()
	if err != nil {
		fmt.Fprintf(ctx.Stderr, "fatal: Not a valid object name: 'HEAD'.\n")
		return ExitFatal
	}

	refPath := "refs/heads/" + newBranchName
	if _, err := r.Refs.GetRef(refPath); err == nil {
		fmt.Fprintf(ctx.Stderr, "fatal: A branch named '%s' already exists.\n", newBranchName)
		return ExitFatal
	}

	if err := r.Refs.UpdateRef(refPath, headOID, r.CommitterSignature(), fmt.Sprintf("branch: Created from HEAD")); err != nil {
		fmt.Fprintf(ctx.Stderr, "fatal: 创建分支失败: %v\n", err)
		return ExitFatal
	}

	return ExitSuccess
}

func listBranches(ctx *Context, r *repo.Repository, all bool) int {
	headRef, _ := r.Refs.ReadHEAD()
	curBranch := ""
	if headRef != nil && headRef.IsSymref {
		curBranch = strings.TrimPrefix(headRef.Target, "refs/heads/")
	}

	headsDir := filepath.Join(r.GitDir, "refs", "heads")
	var branches []string

	_ = filepath.Walk(headsDir, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return nil
		}
		rel, _ := filepath.Rel(headsDir, path)
		branches = append(branches, filepath.ToSlash(rel))
		return nil
	})

	sort.Strings(branches)

	for _, b := range branches {
		if b == curBranch {
			fmt.Fprintf(ctx.Stdout, "* %s\n", b)
		} else {
			fmt.Fprintf(ctx.Stdout, "  %s\n", b)
		}
	}

	return ExitSuccess
}
