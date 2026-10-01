package cli

import (
	"fmt"
	"gogit/internal/object"
	"gogit/internal/repo"
	"strings"
)

func init() {
	Register("commit", cmdCommit)
}

func cmdCommit(ctx *Context) int {
	var message string
	amend := false
	var authorOverride string

	for i := 0; i < len(ctx.Args); i++ {
		arg := ctx.Args[i]
		if arg == "-m" && i+1 < len(ctx.Args) {
			message = ctx.Args[i+1]
			i++
		} else if arg == "--amend" {
			amend = true
		} else if strings.HasPrefix(arg, "--author=") {
			authorOverride = strings.TrimPrefix(arg, "--author=")
		}
	}

	if message == "" {
		fmt.Fprintln(ctx.Stderr, "fatal: 暂不支持交互式编辑器输入提交信息，请使用 -m <message>")
		return ExitFatal
	}

	r, err := repo.FindRepository(".")
	if err != nil {
		fmt.Fprintf(ctx.Stderr, "fatal: %v\n", err)
		return ExitFatal
	}

	idx, err := r.GetIndex()
	if err != nil {
		fmt.Fprintf(ctx.Stderr, "fatal: 获取索引失败: %v\n", err)
		return ExitFatal
	}
	if len(idx.Entries) == 0 {
		fmt.Fprintln(ctx.Stderr, "nothing to commit (create/copy files and use \"gogit add\" to track)")
		return ExitGeneral
	}

	// 1. 从当前 index 写入 Tree 对象
	treeHash, err := idx.WriteTree(r.ObjectsDir)
	if err != nil {
		fmt.Fprintf(ctx.Stderr, "fatal: 构建 Tree 失败: %v\n", err)
		return ExitFatal
	}

	// 2. 解析当前 HEAD 获取父提交
	var parents []object.Hash
	headRef, headErr := r.Refs.ReadHEAD()
	if headErr != nil {
		fmt.Fprintf(ctx.Stderr, "fatal: 读取 HEAD 失败: %v\n", headErr)
		return ExitFatal
	}

	currentHeadHash, resolveErr := r.Refs.ResolveHEAD()
	isRootCommit := (resolveErr != nil || currentHeadHash.IsZero())

	if !isRootCommit {
		if amend {
			// amend 模式继承当前 HEAD 的父提交列表
			oldCommit, err := r.ReadCommit(currentHeadHash)
			if err == nil {
				parents = oldCommit.Parents
			}
		} else {
			parents = append(parents, currentHeadHash)
		}
	}

	// 3. 构建作者与提交者签名
	author := r.AuthorSignature()
	if authorOverride != "" {
		if parsedSig, err := object.ParseSignature(authorOverride); err == nil {
			author = parsedSig
		}
	}
	committer := r.CommitterSignature()

	if !strings.HasSuffix(message, "\n") {
		message += "\n"
	}

	// 4. 创建并写入 Commit 对象
	commit := &object.Commit{
		Tree:      treeHash,
		Parents:   parents,
		Author:    author,
		Committer: committer,
		Message:   message,
	}

	commitHash, err := r.WriteObject(commit)
	if err != nil {
		fmt.Fprintf(ctx.Stderr, "fatal: 写入 commit 对象失败: %v\n", err)
		return ExitFatal
	}

	// 5. 更新引用系统
	logAction := "commit"
	if isRootCommit {
		logAction = "commit (initial)"
	} else if amend {
		logAction = "commit (amend)"
	}
	logMsg := fmt.Sprintf("%s: %s", logAction, strings.TrimSpace(strings.Split(message, "\n")[0]))

	if headRef.IsSymref {
		if err := r.Refs.UpdateRef(headRef.Target, commitHash, committer, logMsg); err != nil {
			fmt.Fprintf(ctx.Stderr, "fatal: 更新分支引用失败: %v\n", err)
			return ExitFatal
		}
	} else {
		if err := r.Refs.SetHEADDetached(commitHash); err != nil {
			fmt.Fprintf(ctx.Stderr, "fatal: 更新 detached HEAD 失败: %v\n", err)
			return ExitFatal
		}
	}

	// 6. 输出简短信息，格式与 git commit 对齐
	branchName := "detached"
	if headRef.IsSymref {
		branchName = strings.TrimPrefix(headRef.Target, "refs/heads/")
	}
	summary := strings.TrimSpace(strings.Split(message, "\n")[0])
	shortHash := commitHash.String()[:7]
	if isRootCommit {
		fmt.Fprintf(ctx.Stdout, "[%s (root-commit) %s] %s\n", branchName, shortHash, summary)
	} else {
		fmt.Fprintf(ctx.Stdout, "[%s %s] %s\n", branchName, shortHash, summary)
	}

	return ExitSuccess
}
