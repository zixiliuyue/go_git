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
	Register("verify-tag", cmdVerifyTag)
}

// cmdVerifyTag 校验 Git 附注标签（Annotated Tag）中的 GPG / SSH 数字签名（对齐 git verify-tag 规范）
func cmdVerifyTag(ctx *Context) int {
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
		fmt.Fprintln(ctx.Stderr, "usage: gogit verify-tag [-v | --verbose] <tag>...")
		return ExitFatal
	}

	r, err := repo.FindRepository(".")
	if err != nil {
		fmt.Fprintf(ctx.Stderr, "fatal: %v\n", err)
		return ExitFatal
	}

	hasError := false

	for _, target := range targets {
		// 1. 尝试解析标签引用（优先尝试 refs/tags/<target>）
		tagHash, err := rev.ParseRevision(r, "refs/tags/"+target)
		if err != nil {
			tagHash, err = rev.ParseRevision(r, target)
		}
		if err != nil {
			fmt.Fprintf(ctx.Stderr, "error: 无法解析标签引用 %q: %v\n", target, err)
			hasError = true
			continue
		}

		// 2. 读取原始对象
		raw, err := r.ReadObject(tagHash)
		if err != nil {
			fmt.Fprintf(ctx.Stderr, "error: %s: 找不到标签对象: %v\n", target, err)
			hasError = true
			continue
		}

		if raw.Type() != object.TypeTag {
			fmt.Fprintf(ctx.Stderr, "error: %s: 不是附注标签对象 (轻量标签不支持签名, 类型: %s)\n", target, raw.Type())
			hasError = true
			continue
		}

		// 3. 提取签名与载荷
		payload, sigStr, sigType, err := signature.ExtractTagSignature(raw.Content)
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
