package cli

import (
	"fmt"
	"gogit/internal/index"
	"gogit/internal/repo"
	"gogit/internal/worktree"
	"os"
)

func init() {
	Register("fsmonitor", cmdFSMonitor)
}

// cmdFSMonitor 管理并调试 FSMonitor 文件系统事件监控
func cmdFSMonitor(ctx *Context) int {
	sub := "status"
	if len(ctx.Args) > 0 {
		sub = ctx.Args[0]
	}

	cwd, err := os.Getwd()
	if err != nil {
		fmt.Fprintf(ctx.Stderr, "fatal: %v\n", err)
		return ExitFatal
	}

	r, err := repo.FindRepository(cwd)
	if err != nil {
		fmt.Fprintf(ctx.Stderr, "fatal: 不是 git 仓库: %v\n", err)
		return ExitFatal
	}

	switch sub {
	case "status":
		enabled := worktree.IsFSMonitorEnabled(r)
		fmt.Fprintf(ctx.Stdout, "FSMonitor 启用状态: %v\n", enabled)
		if enabled {
			fmt.Fprintf(ctx.Stdout, "core.fsmonitor 配置: %s\n", r.Config.Get("core", "fsmonitor"))
		}
		idx, err := r.GetIndex()
		if err == nil {
			fsmn, _ := idx.GetFSMonitorExtension()
			if fsmn != nil {
				fmt.Fprintf(ctx.Stdout, "FSMN Token: %s (协议版本: %d)\n", fsmn.Token, fsmn.Version)
			} else {
				fmt.Fprintln(ctx.Stdout, "索引中未记录 FSMN 扩展标记")
			}
			validCount := 0
			for _, e := range idx.Entries {
				if e.IsFSMonitorValid() {
					validCount++
				}
			}
			fmt.Fprintf(ctx.Stdout, "已验证无变更条目数: %d / %d\n", validCount, len(idx.Entries))
		}
		return ExitSuccess

	case "query":
		if !worktree.IsFSMonitorEnabled(r) {
			fmt.Fprintln(ctx.Stderr, "fatal: core.fsmonitor 未在 git 配置中启用")
			return ExitFatal
		}
		prevToken := ""
		idx, err := r.GetIndex()
		if err == nil {
			if fsmn, _ := idx.GetFSMonitorExtension(); fsmn != nil {
				prevToken = fsmn.Token
			}
		}
		newToken, changed, isFull, err := worktree.QueryFSMonitor(r, prevToken)
		if err != nil {
			fmt.Fprintf(ctx.Stderr, "fatal: fsmonitor 查询失败: %v\n", err)
			return ExitFatal
		}
		fmt.Fprintf(ctx.Stdout, "New Token: %s (全量刷新: %v)\n", newToken, isFull)
		fmt.Fprintf(ctx.Stdout, "变更文件数: %d\n", len(changed))
		for p := range changed {
			fmt.Fprintf(ctx.Stdout, "  M %s\n", p)
		}
		return ExitSuccess

	case "run":
		idx, err := r.GetIndex()
		if err != nil {
			idx = index.NewIndex()
		}
		updated, err := worktree.ApplyFSMonitorUpdate(r, idx)
		if err != nil {
			fmt.Fprintf(ctx.Stderr, "fatal: 应用 fsmonitor 更新失败: %v\n", err)
			return ExitFatal
		}
		if updated {
			if err := idx.WriteIndex(r.IndexPath); err != nil {
				fmt.Fprintf(ctx.Stderr, "fatal: 写入索引失败: %v\n", err)
				return ExitFatal
			}
			fmt.Fprintln(ctx.Stdout, "FSMonitor 更新已成功应用并写入索引")
		} else {
			fmt.Fprintln(ctx.Stdout, "FSMonitor 未启用或无状态变更")
		}
		return ExitSuccess

	default:
		fmt.Fprintf(ctx.Stderr, "usage: gogit fsmonitor [status|query|run]\n")
		return ExitUsage
	}
}
