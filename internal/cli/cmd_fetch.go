package cli

import (
	"fmt"
	"gogit/internal/object"
	"gogit/internal/pack"
	"gogit/internal/repo"
	"gogit/internal/transport"
	"os"
	"strings"
)

func init() {
	Register("fetch", cmdFetch)
}

func cmdFetch(ctx *Context) int {
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

	remoteName := "origin"
	if len(ctx.Args) > 0 && !strings.HasPrefix(ctx.Args[0], "-") {
		remoteName = ctx.Args[0]
	}

	remoteCfg, err := transport.GetRemote(r, remoteName)
	if err != nil {
		// 检查是否直接传入了 URL
		if strings.Contains(remoteName, "://") || strings.HasPrefix(remoteName, "/") {
			remoteCfg = &transport.RemoteConfig{Name: "origin", URL: remoteName}
		} else {
			fmt.Fprintf(ctx.Stderr, "fatal: '%s' 并非一个有效的远端仓库名\n", remoteName)
			return ExitFatal
		}
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

	// 收集本地已有的 haves
	localRefs, _ := r.Refs.ListRefs("")
	var haves []object.Hash
	haveSet := make(map[object.Hash]bool)
	for _, oid := range localRefs {
		if !haveSet[oid] {
			haves = append(haves, oid)
			haveSet[oid] = true
		}
	}

	// 确定 wants
	var wants []object.Hash
	wantSet := make(map[object.Hash]bool)
	for _, rf := range remoteRefs {
		if strings.HasPrefix(rf.Name, "refs/heads/") && !rf.OID.IsZero() {
			if !haveSet[rf.OID] && !wantSet[rf.OID] {
				wants = append(wants, rf.OID)
				wantSet[rf.OID] = true
			}
		}
	}

	if len(wants) > 0 {
		packData, err := client.Fetch(wants, haves)
		if err != nil {
			fmt.Fprintf(ctx.Stderr, "fatal: fetch-pack 失败: %v\n", err)
			return ExitFatal
		}
		if len(packData) > 0 {
			if _, err := pack.SavePackAndIndex(r.ObjectsDir, packData); err != nil {
				fmt.Fprintf(ctx.Stderr, "fatal: 保存 pack 失败: %v\n", err)
				return ExitFatal
			}
		}
	}

	sig := r.AuthorSignature()

	// 更新本地跟踪分支 refs/remotes/<remoteName>/*
	for _, rf := range remoteRefs {
		if strings.HasPrefix(rf.Name, "refs/heads/") {
			branchName := strings.TrimPrefix(rf.Name, "refs/heads/")
			trackingRef := fmt.Sprintf("refs/remotes/%s/%s", remoteName, branchName)

			oldRef, _ := r.Refs.GetRef(trackingRef)
			var oldOID object.Hash
			if oldRef != nil {
				oldOID = oldRef.Hash
			}

			if oldOID != rf.OID {
				_ = r.Refs.UpdateRef(trackingRef, rf.OID, sig, fmt.Sprintf("fetch: %s", rf.Name))
				if oldOID.IsZero() {
					fmt.Fprintf(ctx.Stderr, " * [new branch]      %s -> %s/%s\n", branchName, remoteName, branchName)
				} else {
					fmt.Fprintf(ctx.Stderr, "   %s..%s  %s -> %s/%s\n", oldOID.Short(), rf.OID.Short(), branchName, remoteName, branchName)
				}
			}
		} else if strings.HasPrefix(rf.Name, "refs/tags/") {
			_ = r.Refs.UpdateRef(rf.Name, rf.OID, sig, "fetch: tag")
		}
	}

	return ExitSuccess
}
