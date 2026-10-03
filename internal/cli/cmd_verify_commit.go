package cli

import (
	"errors"
	"fmt"
	"gogit/internal/object"
	"gogit/internal/repo"
	"gogit/internal/rev"
	"gogit/internal/signature"
)

func init() {
	Register("verify-commit", cmdVerifyCommit)
}

// cmdVerifyCommit 校验 Git Commit 中的 GPG / SSH 数字签名（对齐 git verify-commit 规范）
func cmdVerifyCommit(ctx *Context) int {
	verbose := false
	var targets []string

	for _, arg := range ctx.Args {
		if arg == "-v" || arg == "--verbose" {
			verbose = true
		} else if len(arg) > 0 && arg[0] != '-' {
			targets = append(targets, arg)
		}
	}

	if len(targets) == 0 {
		fmt.Fprintln(ctx.Stderr, "usage: gogit verify-commit [-v | --verbose] <commit>...")
		return ExitFatal
	}

	r, err := repo.FindRepository(".")
	if err != nil {
		fmt.Fprintf(ctx.Stderr, "fatal: %v\n", err)
		return ExitFatal
	}

	hasError := false

	for _, target := range targets {
		// 1. 解析目标提交哈希（支持分支名、HEAD 或 SHA 前缀）
		targetHash, err := rev.ParseRevision(r, target)
		if err != nil {
			fmt.Fprintf(ctx.Stderr, "error: 无法解析提交引用 %q: %v\n", target, err)
			hasError = true
			continue
		}

		// 2. 读取原始对象
		raw, err := r.ReadObject(targetHash)
		if err != nil {
			fmt.Fprintf(ctx.Stderr, "error: %s: 找不到提交对象: %v\n", target, err)
			hasError = true
			continue
		}

		if raw.Type() != object.TypeCommit {
			fmt.Fprintf(ctx.Stderr, "error: %s: 对象类型不是 commit (实际为 %s)\n", target, raw.Type())
			hasError = true
			continue
		}

		// 3. 提取签名与载荷
		payload, sigStr, sigType, err := signature.ExtractCommitSignature(raw.Content)
		if err != nil {
			if errors.Is(err, signature.ErrNoSignature) {
				fmt.Fprintf(ctx.Stderr, "error: %s: 未找到数字签名 (no signature found)\n", target)
			} else {
				fmt.Fprintf(ctx.Stderr, "error: %s: 提取签名失败: %v\n", target, err)
			}
			hasError = true
			continue
		}

		// 4. 执行密码学校验
		res, err := signature.VerifySignature(payload, sigStr)
		if err != nil || !res.Valid {
			fmt.Fprintf(ctx.Stderr, "error: %s: 数字签名校验失败: %v\n", target, err)
			hasError = true
			continue
		}

		// 5. 输出验证成功信息
		if verbose {
			fmt.Fprintf(ctx.Stdout, "[%s] %s\n", sigType, res.RawOutput)
		} else {
			fmt.Fprintln(ctx.Stdout, res.RawOutput)
		}
	}

	if hasError {
		return ExitGeneral
	}
	return ExitSuccess
}
