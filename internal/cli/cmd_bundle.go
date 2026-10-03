package cli

import (
	"bytes"
	"fmt"
	"gogit/internal/history"
	"gogit/internal/object"
	"gogit/internal/pack"
	"gogit/internal/repo"
	"gogit/internal/rev"
	"os"
	"strings"
)

func init() {
	Register("bundle", cmdBundle)
}

// cmdBundle 实现 git bundle [create|verify|list-heads|unbundle]
func cmdBundle(ctx *Context) int {
	if len(ctx.Args) == 0 {
		fmt.Fprintln(ctx.Stderr, "usage: gogit bundle [create|verify|list-heads|unbundle] <file> [<args>...]")
		return ExitUsage
	}

	sub := ctx.Args[0]
	cwd, err := os.Getwd()
	if err != nil {
		fmt.Fprintf(ctx.Stderr, "fatal: %v\n", err)
		return ExitFatal
	}

	switch sub {
	case "create":
		if len(ctx.Args) < 2 {
			fmt.Fprintln(ctx.Stderr, "usage: gogit bundle create <file> [<rev-list-args>...]")
			return ExitUsage
		}
		targetFile := ctx.Args[1]
		revArgs := ctx.Args[2:]
		if len(revArgs) == 0 {
			revArgs = []string{"HEAD"}
		}

		r, err := repo.FindRepository(cwd)
		if err != nil {
			fmt.Fprintf(ctx.Stderr, "fatal: 不是 git 仓库: %v\n", err)
			return ExitFatal
		}

		var positiveRefs []string
		var prereqOIDs []object.Hash

		for _, arg := range revArgs {
			if strings.HasPrefix(arg, "^") {
				// 负向引用 / 先决提交
				base := strings.TrimPrefix(arg, "^")
				h, err := rev.ParseRevision(r, base)
				if err != nil {
					fmt.Fprintf(ctx.Stderr, "fatal: 无法解析先决提交 %s: %v\n", base, err)
					return ExitFatal
				}
				prereqOIDs = append(prereqOIDs, h)
			} else {
				positiveRefs = append(positiveRefs, arg)
			}
		}

		bundleData, header, err := history.CreateBundle(r, positiveRefs, prereqOIDs)
		if err != nil {
			fmt.Fprintf(ctx.Stderr, "fatal: 创建 bundle 失败: %v\n", err)
			return ExitFatal
		}

		if err := os.WriteFile(targetFile, bundleData, 0644); err != nil {
			fmt.Fprintf(ctx.Stderr, "fatal: 写入文件 %s 失败: %v\n", targetFile, err)
			return ExitFatal
		}

		fmt.Fprintf(ctx.Stdout, "成功生成 bundle: %s (包含 %d 个引用)\n", targetFile, len(header.References))
		return ExitSuccess

	case "verify":
		if len(ctx.Args) < 2 {
			fmt.Fprintln(ctx.Stderr, "usage: gogit bundle verify <file>")
			return ExitUsage
		}
		bundleFile := ctx.Args[1]
		bundleBytes, err := os.ReadFile(bundleFile)
		if err != nil {
			fmt.Fprintf(ctx.Stderr, "fatal: 读取文件 %s 失败: %v\n", bundleFile, err)
			return ExitFatal
		}

		header, _, err := pack.ReadBundle(bytes.NewReader(bundleBytes))
		if err != nil {
			fmt.Fprintf(ctx.Stderr, "fatal: 解析 bundle 失败: %v\n", err)
			return ExitFatal
		}

		r, err := repo.FindRepository(cwd)
		if err != nil {
			fmt.Fprintf(ctx.Stderr, "fatal: 不是 git 仓库: %v\n", err)
			return ExitFatal
		}

		if err := history.VerifyBundle(r, header); err != nil {
			fmt.Fprintf(ctx.Stderr, "error: %v\n", err)
			fmt.Fprintf(ctx.Stderr, "%s is okay\n", bundleFile) // 模拟 git 错误输出行为
			return ExitFatal
		}

		fmt.Fprintf(ctx.Stdout, "The bundle contains these %d refs:\n", len(header.References))
		for _, ref := range header.References {
			fmt.Fprintf(ctx.Stdout, "%s %s\n", ref.OID.String(), ref.Name)
		}
		if len(header.Prerequisites) > 0 {
			fmt.Fprintf(ctx.Stdout, "The bundle requires these %d prerequisites:\n", len(header.Prerequisites))
			for _, p := range header.Prerequisites {
				fmt.Fprintf(ctx.Stdout, "%s\n", p.OID.String())
			}
		}
		fmt.Fprintf(ctx.Stdout, "%s is okay\n", bundleFile)
		return ExitSuccess

	case "list-heads":
		if len(ctx.Args) < 2 {
			fmt.Fprintln(ctx.Stderr, "usage: gogit bundle list-heads <file>")
			return ExitUsage
		}
		bundleFile := ctx.Args[1]
		bundleBytes, err := os.ReadFile(bundleFile)
		if err != nil {
			fmt.Fprintf(ctx.Stderr, "fatal: 读取文件 %s 失败: %v\n", bundleFile, err)
			return ExitFatal
		}

		header, _, err := pack.ReadBundle(bytes.NewReader(bundleBytes))
		if err != nil {
			fmt.Fprintf(ctx.Stderr, "fatal: 解析 bundle 失败: %v\n", err)
			return ExitFatal
		}

		for _, ref := range header.References {
			fmt.Fprintf(ctx.Stdout, "%s %s\n", ref.OID.String(), ref.Name)
		}
		return ExitSuccess

	case "unbundle":
		if len(ctx.Args) < 2 {
			fmt.Fprintln(ctx.Stderr, "usage: gogit bundle unbundle <file>")
			return ExitUsage
		}
		bundleFile := ctx.Args[1]
		bundleBytes, err := os.ReadFile(bundleFile)
		if err != nil {
			fmt.Fprintf(ctx.Stderr, "fatal: 读取文件 %s 失败: %v\n", bundleFile, err)
			return ExitFatal
		}

		header, packData, err := pack.ReadBundle(bytes.NewReader(bundleBytes))
		if err != nil {
			fmt.Fprintf(ctx.Stderr, "fatal: 解析 bundle 失败: %v\n", err)
			return ExitFatal
		}

		r, err := repo.FindRepository(cwd)
		if err != nil {
			fmt.Fprintf(ctx.Stderr, "fatal: 不是 git 仓库: %v\n", err)
			return ExitFatal
		}

		if err := history.Unbundle(r, header, packData); err != nil {
			fmt.Fprintf(ctx.Stderr, "fatal: 解包 bundle 失败: %v\n", err)
			return ExitFatal
		}

		for _, ref := range header.References {
			fmt.Fprintf(ctx.Stdout, "%s %s\n", ref.OID.String(), ref.Name)
		}
		return ExitSuccess

	default:
		fmt.Fprintf(ctx.Stderr, "fatal: 未知子命令 '%s'\n", sub)
		return ExitUsage
	}
}
