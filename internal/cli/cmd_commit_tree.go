package cli

import (
	"fmt"
	"gogit/internal/object"
	"gogit/internal/repo"
	"gogit/internal/rev"
	"io"
	"strings"
)

func init() {
	Register("commit-tree", cmdCommitTree)
}

func cmdCommitTree(ctx *Context) int {
	if len(ctx.Args) == 0 {
		fmt.Fprintln(ctx.Stderr, "用法: gogit commit-tree <tree> [-p <parent>...] -m <message>")
		return ExitFatal
	}

	treeExpr := ctx.Args[0]
	var parents []object.Hash
	var message string

	for i := 1; i < len(ctx.Args); i++ {
		arg := ctx.Args[i]
		if arg == "-m" && i+1 < len(ctx.Args) {
			message = ctx.Args[i+1]
			i++
		} else if arg == "-p" && i+1 < len(ctx.Args) {
			pHash, err := object.NewHashFromHex(ctx.Args[i+1])
			if err != nil {
				fmt.Fprintf(ctx.Stderr, "fatal: 无效的 parent 哈希: %s\n", ctx.Args[i+1])
				return ExitFatal
			}
			parents = append(parents, pHash)
			i++
		}
	}

	r, err := repo.FindRepository(".")
	if err != nil {
		fmt.Fprintf(ctx.Stderr, "fatal: %v\n", err)
		return ExitFatal
	}

	treeHash, err := rev.ParseRevision(r, treeExpr)
	if err != nil {
		fmt.Fprintf(ctx.Stderr, "fatal: 无法解析 Tree: %s\n", treeExpr)
		return ExitFatal
	}

	if message == "" && ctx.Stdin != nil {
		data, _ := io.ReadAll(ctx.Stdin)
		message = string(data)
	}

	if message == "" {
		fmt.Fprintln(ctx.Stderr, "fatal: 必须提供提交说明 (-m 或 stdin)")
		return ExitFatal
	}

	if !strings.HasSuffix(message, "\n") {
		message += "\n"
	}

	author := r.AuthorSignature()
	committer := r.CommitterSignature()

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

	fmt.Fprintln(ctx.Stdout, commitHash.String())
	return ExitSuccess
}
