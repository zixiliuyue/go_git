package cli

import (
	"fmt"
	"gogit/internal/object"
	"gogit/internal/pack"
	"gogit/internal/repo"
	"gogit/internal/transport"
	"gogit/internal/worktree"
	"os"
	"path/filepath"
	"strings"
)

func init() {
	Register("clone", cmdClone)
}

func cmdClone(ctx *Context) int {
	if len(ctx.Args) == 0 {
		fmt.Fprintln(ctx.Stderr, "用法: gogit clone [--bare] [-b <分支>] <仓库URL> [<目录>]")
		return ExitGeneral
	}

	isBare := false
	specifiedBranch := ""
	var positional []string

	for i := 0; i < len(ctx.Args); i++ {
		arg := ctx.Args[i]
		if arg == "--bare" {
			isBare = true
		} else if arg == "-b" || arg == "--branch" {
			if i+1 < len(ctx.Args) {
				specifiedBranch = ctx.Args[i+1]
				i++
			}
		} else if strings.HasPrefix(arg, "--branch=") {
			specifiedBranch = strings.TrimPrefix(arg, "--branch=")
		} else {
			positional = append(positional, arg)
		}
	}

	if len(positional) == 0 {
		fmt.Fprintln(ctx.Stderr, "fatal: 必须提供克隆的仓库 URL")
		return ExitFatal
	}

	rawURL := positional[0]
	var targetDir string
	if len(positional) >= 2 {
		targetDir = positional[1]
	} else {
		// 从 URL 提取目标目录名
		trimmed := strings.TrimRight(rawURL, "/")
		base := filepath.Base(trimmed)
		targetDir = strings.TrimSuffix(base, ".git")
		if targetDir == "" || targetDir == "." {
			targetDir = "cloned-repo"
		}
	}

	// 检查目录状态
	if stat, err := os.Stat(targetDir); err == nil {
		if !stat.IsDir() {
			fmt.Fprintf(ctx.Stderr, "fatal: 目标路径 '%s' 已存在且不是目录\n", targetDir)
			return ExitFatal
		}
		entries, _ := os.ReadDir(targetDir)
		if len(entries) > 0 {
			fmt.Fprintf(ctx.Stderr, "fatal: 目标路径 '%s' 已存在且不为空\n", targetDir)
			return ExitFatal
		}
	}

	fmt.Fprintf(ctx.Stderr, "正克隆到 '%s'...\n", targetDir)

	// 1. 初始化目标仓库
	r, err := repo.InitRepository(targetDir, isBare, "master")
	if err != nil {
		fmt.Fprintf(ctx.Stderr, "fatal: 初始化本地仓库失败: %v\n", err)
		return ExitFatal
	}

	// 2. 添加 origin 远端
	if err := transport.AddRemote(r, "origin", rawURL); err != nil {
		fmt.Fprintf(ctx.Stderr, "warning: 添加远端失败: %v\n", err)
	}

	// 3. 连接远端客户端进行引用发现
	client, err := transport.NewClient(rawURL)
	if err != nil {
		fmt.Fprintf(ctx.Stderr, "fatal: 连接远端传输失败: %v\n", err)
		return ExitFatal
	}

	remoteRefs, headSymref, err := client.Discover()
	if err != nil {
		fmt.Fprintf(ctx.Stderr, "fatal: 无法读取远端仓库: %v\n", err)
		return ExitFatal
	}

	if len(remoteRefs) == 0 {
		fmt.Fprintln(ctx.Stderr, "warning: 您似乎克隆了一个空仓库。")
		return ExitSuccess
	}

	// 4. 决定默认主分支
	defaultBranch := ""
	var defaultCommit object.Hash

	if specifiedBranch != "" {
		for _, rf := range remoteRefs {
			if rf.Name == "refs/heads/"+specifiedBranch {
				defaultBranch = specifiedBranch
				defaultCommit = rf.OID
				break
			}
		}
		if defaultBranch == "" {
			fmt.Fprintf(ctx.Stderr, "fatal: 远端不存在指定分支 '%s'\n", specifiedBranch)
			return ExitFatal
		}
	} else if headSymref != "" && strings.HasPrefix(headSymref, "refs/heads/") {
		defaultBranch = strings.TrimPrefix(headSymref, "refs/heads/")
		for _, rf := range remoteRefs {
			if rf.Name == headSymref {
				defaultCommit = rf.OID
				break
			}
		}
	}

	// 若未通过 symref 获得，按常规常见分支查找
	if defaultBranch == "" {
		candidates := []string{"main", "master", "trunk", "development"}
		for _, cand := range candidates {
			targetName := "refs/heads/" + cand
			for _, rf := range remoteRefs {
				if rf.Name == targetName {
					defaultBranch = cand
					defaultCommit = rf.OID
					break
				}
			}
			if defaultBranch != "" {
				break
			}
		}
	}

	// 如果依然未匹配，选择首个 refs/heads/ 分支
	if defaultBranch == "" {
		for _, rf := range remoteRefs {
			if strings.HasPrefix(rf.Name, "refs/heads/") {
				defaultBranch = strings.TrimPrefix(rf.Name, "refs/heads/")
				defaultCommit = rf.OID
				break
			}
		}
	}

	// 5. 准备 wants 列表：拉取远端所有的分支与标签
	var wants []object.Hash
	seenWants := make(map[object.Hash]bool)
	for _, rf := range remoteRefs {
		if (strings.HasPrefix(rf.Name, "refs/heads/") || strings.HasPrefix(rf.Name, "refs/tags/")) && !rf.OID.IsZero() {
			if !seenWants[rf.OID] {
				wants = append(wants, rf.OID)
				seenWants[rf.OID] = true
			}
		}
	}
	if len(wants) == 0 && !defaultCommit.IsZero() {
		wants = append(wants, defaultCommit)
	}

	// 6. 拉取 packfile
	packData, err := client.Fetch(wants, nil)
	if err != nil {
		fmt.Fprintf(ctx.Stderr, "fatal: 拉取 pack 数据失败: %v\n", err)
		return ExitFatal
	}

	// 7. 保存 packfile 与生成 .idx
	if len(packData) > 0 {
		if _, err := pack.SavePackAndIndex(r.ObjectsDir, packData); err != nil {
			fmt.Fprintf(ctx.Stderr, "fatal: 解压索引 pack 失败: %v\n", err)
			return ExitFatal
		}
	}

	sig := r.AuthorSignature()

	// 8. 创建本地分支与远端跟踪分支 (refs/remotes/origin/*)
	for _, rf := range remoteRefs {
		if strings.HasPrefix(rf.Name, "refs/heads/") {
			branchName := strings.TrimPrefix(rf.Name, "refs/heads/")
			remoteTrackingRef := fmt.Sprintf("refs/remotes/origin/%s", branchName)
			_ = r.Refs.UpdateRef(remoteTrackingRef, rf.OID, sig, "clone: remote tracking")
			if branchName == defaultBranch {
				_ = r.Refs.UpdateRef(rf.Name, rf.OID, sig, "clone: local branch")
			}
		} else if strings.HasPrefix(rf.Name, "refs/tags/") {
			_ = r.Refs.UpdateRef(rf.Name, rf.OID, sig, "clone: tag")
		}
	}

	// 设置本地 HEAD
	if defaultBranch != "" {
		_ = r.Refs.SetHEADSymbolic("refs/heads/" + defaultBranch)
	}

	// 9. 如果不是裸仓库，检出工作区
	if !isBare && defaultBranch != "" {
		if err := worktree.CheckoutSwitch(r, defaultBranch, worktree.CheckoutOptions{}); err != nil {
			fmt.Fprintf(ctx.Stderr, "warning: 检出工作区失败: %v\n", err)
		}
	}

	return ExitSuccess
}
