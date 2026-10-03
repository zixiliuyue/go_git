package cli

import (
	"fmt"
	"gogit/internal/repo"
	"os"
	"path/filepath"
	"strings"
)

func init() {
	Register("config", cmdConfig)
}

// cmdConfig 读取或设置 Git 仓库配置项（支持 gogit config section.key [value]）
func cmdConfig(ctx *Context) int {
	if len(ctx.Args) == 0 {
		fmt.Fprintln(ctx.Stderr, "usage: gogit config <key> [value]")
		return ExitFatal
	}

	r, err := repo.FindRepository(".")
	if err != nil {
		fmt.Fprintf(ctx.Stderr, "fatal: %v\n", err)
		return ExitFatal
	}

	keyArg := ctx.Args[0]
	dotIdx := strings.LastIndex(keyArg, ".")
	if dotIdx <= 0 || dotIdx == len(keyArg)-1 {
		fmt.Fprintf(ctx.Stderr, "error: 键名格式无效 (应为 section.key): %s\n", keyArg)
		return ExitFatal
	}

	section := keyArg[:dotIdx]
	key := keyArg[dotIdx+1:]

	if len(ctx.Args) == 1 {
		// 读取配置
		val := r.Config.Get(section, key)
		if val == "" {
			return ExitGeneral
		}
		fmt.Fprintln(ctx.Stdout, val)
		return ExitSuccess
	}

	// 写入配置
	val := ctx.Args[1]
	r.Config.Set(section, key, val)
	cfgPath := filepath.Join(r.GitDir, "config")
	if err := os.WriteFile(cfgPath, r.Config.Serialize(), 0644); err != nil {
		fmt.Fprintf(ctx.Stderr, "fatal: 写入配置文件失败: %v\n", err)
		return ExitFatal
	}

	return ExitSuccess
}
