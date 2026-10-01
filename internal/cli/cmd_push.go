package cli

import (
	"fmt"
	"gogit/internal/hook"
	"gogit/internal/merge"
	"gogit/internal/object"
	"gogit/internal/pack"
	"gogit/internal/repo"
	"gogit/internal/transport"
	"os"
	"path/filepath"
	"strings"
)

func init() {
	Register("push", cmdPush)
}

func cmdPush(ctx *Context) int {
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

	isForce := false
	setUpstream := false
	var positional []string

	for _, arg := range ctx.Args {
		if arg == "-f" || arg == "--force" {
			isForce = true
		} else if arg == "-u" || arg == "--set-upstream" {
			setUpstream = true
		} else {
			positional = append(positional, arg)
		}
	}

	remoteName := "origin"
	if len(positional) > 0 {
		remoteName = positional[0]
	}

	// 解析当前分支
	headRef, err := r.Refs.ReadHEAD()
	if err != nil || !headRef.IsSymref {
		fmt.Fprintln(ctx.Stderr, "fatal: 您当前未处于任何分支上。")
		return ExitFatal
	}
	currentBranch := strings.TrimPrefix(headRef.Target, "refs/heads/")

	srcBranch := currentBranch
	dstBranch := currentBranch

	if len(positional) > 1 {
		refspec := positional[1]
		if strings.Contains(refspec, ":") {
			parts := strings.SplitN(refspec, ":", 2)
			srcBranch = parts[0]
			dstBranch = parts[1]
		} else {
			srcBranch = refspec
			dstBranch = refspec
		}
	}

	localOID, err := r.Refs.ResolveRef("refs/heads/" + srcBranch)
	if err != nil {
		fmt.Fprintf(ctx.Stderr, "fatal: 找不到本地分支 '%s'\n", srcBranch)
		return ExitFatal
	}

	remoteCfg, err := transport.GetRemote(r, remoteName)
	if err != nil {
		if strings.Contains(remoteName, "://") || strings.HasPrefix(remoteName, "/") {
			remoteCfg = &transport.RemoteConfig{Name: "origin", URL: remoteName}
		} else {
			fmt.Fprintf(ctx.Stderr, "fatal: 未知远端 '%s'\n", remoteName)
			return ExitFatal
		}
	}

	// 运行 pre-push 钩子
	if err := hook.RunHook(r.GitDir, "pre-push", []string{remoteName, remoteCfg.URL}, nil, nil, ctx.Stdout, ctx.Stderr); err != nil {
		fmt.Fprintf(ctx.Stderr, "%v\n", err)
		return ExitError
	}

	client, err := transport.NewClient(remoteCfg.URL)
	if err != nil {
		fmt.Fprintf(ctx.Stderr, "fatal: 连接远端失败: %v\n", err)
		return ExitFatal
	}

	remoteRefs, _, err := client.Discover()
	if err != nil {
		fmt.Fprintf(ctx.Stderr, "fatal: 无法读取远端引用: %v\n", err)
		return ExitFatal
	}

	targetRefName := "refs/heads/" + dstBranch
	var remoteOID object.Hash
	for _, rf := range remoteRefs {
		if rf.Name == targetRefName {
			remoteOID = rf.OID
			break
		}
	}

	// 非强制推送时的 Fast-forward 检查
	if !remoteOID.IsZero() && !isForce {
		lca, err := merge.FindMergeBase(r, localOID, remoteOID)
		if err == nil && lca != remoteOID {
			fmt.Fprintf(ctx.Stderr, "error: 无法推送到远端引用 '%s'\n", targetRefName)
			fmt.Fprintf(ctx.Stderr, "hint: 更新被拒绝，因为您当前分支的最新提交落后于其对应的远程分支。\n")
			fmt.Fprintf(ctx.Stderr, "hint: 请先执行 'gogit pull' 整合远端变更后再推送。\n")
			return ExitGeneral
		}
	}

	// 收集待推送的对象 (从 localOID 开始，至 remoteOID 截断)
	haveSet := make(map[object.Hash]bool)
	if !remoteOID.IsZero() {
		haveSet[remoteOID] = true
	}

	visitedObjects := make(map[object.Hash]bool)
	var packableList []pack.PackableObject

	var collectTree func(treeOID object.Hash) error
	collectTree = func(treeOID object.Hash) error {
		if visitedObjects[treeOID] || haveSet[treeOID] {
			return nil
		}
		visitedObjects[treeOID] = true

		raw, err := r.ReadObject(treeOID)
		if err != nil {
			return err
		}
		packableList = append(packableList, pack.PackableObject{
			OID:     treeOID,
			Type:    object.TypeTree,
			Content: raw.Content,
		})

		treeObj, err := object.ParseTree(raw.Content)
		if err != nil {
			return err
		}

		for _, e := range treeObj.Entries {
			if visitedObjects[e.OID] || haveSet[e.OID] {
				continue
			}
			if e.Mode == object.ModeDirectory {
				if err := collectTree(e.OID); err != nil {
					return err
				}
			} else {
				visitedObjects[e.OID] = true
				blobRaw, err := r.ReadObject(e.OID)
				if err != nil {
					return err
				}
				packableList = append(packableList, pack.PackableObject{
					OID:     e.OID,
					Type:    object.TypeBlob,
					Content: blobRaw.Content,
				})
			}
		}
		return nil
	}

	queue := []object.Hash{localOID}
	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]

		if visitedObjects[cur] || haveSet[cur] {
			continue
		}
		visitedObjects[cur] = true

		raw, err := r.ReadObject(cur)
		if err != nil {
			return ExitFatal
		}

		packableList = append(packableList, pack.PackableObject{
			OID:     cur,
			Type:    raw.ObjType,
			Content: raw.Content,
		})

		if raw.ObjType == object.TypeCommit {
			commitObj, err := object.ParseCommit(raw.Content)
			if err != nil {
				return ExitFatal
			}
			if err := collectTree(commitObj.Tree); err != nil {
				return ExitFatal
			}
			for _, p := range commitObj.Parents {
				if !haveSet[p] && !visitedObjects[p] {
					queue = append(queue, p)
				}
			}
		}
	}

	var packData []byte
	if len(packableList) > 0 {
		var err error
		packData, _, _, err = pack.BuildPack(packableList)
		if err != nil {
			fmt.Fprintf(ctx.Stderr, "fatal: 构建 pack 失败: %v\n", err)
			return ExitFatal
		}
	}

	updates := []transport.RefUpdate{
		{
			Name:    targetRefName,
			OldOID:  remoteOID,
			NewOID:  localOID,
			IsForce: isForce,
		},
	}

	if err := client.Push(updates, packData); err != nil {
		fmt.Fprintf(ctx.Stderr, "fatal: 推送失败: %v\n", err)
		return ExitFatal
	}

	// 更新本地远程跟踪分支
	trackingRef := fmt.Sprintf("refs/remotes/%s/%s", remoteName, dstBranch)
	_ = r.Refs.UpdateRef(trackingRef, localOID, r.AuthorSignature(), fmt.Sprintf("push: %s", targetRefName))

	// 处理 -u / --set-upstream
	if setUpstream {
		cfgPath := filepath.Join(r.GitDir, "config")
		cfg, _ := repo.ParseConfigFile(cfgPath, r.GitDir)
		if cfg != nil {
			sec := fmt.Sprintf("branch.%s", srcBranch)
			cfg.Set(sec, "remote", remoteName)
			cfg.Set(sec, "merge", targetRefName)
			_ = os.WriteFile(cfgPath, cfg.Serialize(), 0644)
		}
	}

	fmt.Fprintf(ctx.Stderr, "To %s\n", remoteCfg.URL)
	if remoteOID.IsZero() {
		fmt.Fprintf(ctx.Stderr, " * [new branch]      %s -> %s\n", srcBranch, dstBranch)
	} else {
		fmt.Fprintf(ctx.Stderr, "   %s..%s  %s -> %s\n", remoteOID.Short(), localOID.Short(), srcBranch, dstBranch)
	}

	return ExitSuccess
}
