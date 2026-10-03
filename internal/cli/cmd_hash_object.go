package cli

import (
	"fmt"
	"gogit/internal/object"
	"gogit/internal/repo"
	"io"
	"os"
	"strings"
)

func init() {
	Register("hash-object", cmdHashObject)
}

func cmdHashObject(ctx *Context) int {
	write := false
	fromStdin := false
	objType := object.TypeBlob
	var filePath string
	formatStr := ""

	for i := 0; i < len(ctx.Args); i++ {
		arg := ctx.Args[i]
		if arg == "-w" {
			write = true
		} else if arg == "--stdin" {
			fromStdin = true
		} else if arg == "-t" && i+1 < len(ctx.Args) {
			objType = object.ObjectType(ctx.Args[i+1])
			i++
		} else if strings.HasPrefix(arg, "--object-format=") {
			formatStr = strings.TrimPrefix(arg, "--object-format=")
		} else if arg == "--object-format" && i+1 < len(ctx.Args) {
			formatStr = ctx.Args[i+1]
			i++
		} else if len(arg) > 0 && arg[0] != '-' {
			filePath = arg
		}
	}

	var content []byte
	var err error

	if fromStdin {
		content, err = io.ReadAll(ctx.Stdin)
	} else if filePath != "" {
		content, err = os.ReadFile(filePath)
	} else {
		fmt.Fprintln(ctx.Stderr, "fatal: 未提供目标文件或 --stdin")
		return ExitFatal
	}

	if err != nil {
		fmt.Fprintf(ctx.Stderr, "fatal: 读取内容失败: %v\n", err)
		return ExitFatal
	}

	// 确定对象哈希格式：显式指定优先，否则探测当前仓库配置，默认使用 SHA-1
	objFormat := object.FormatSHA1
	if formatStr != "" {
		objFormat = object.ObjectFormat(formatStr)
	} else if r, err := repo.FindRepository("."); err == nil && r.ObjectFormat != "" {
		objFormat = r.ObjectFormat
	}

	hashStr, err := object.HashObjectHex(objFormat, objType, content)
	if err != nil {
		fmt.Fprintf(ctx.Stderr, "fatal: 计算对象哈希失败: %v\n", err)
		return ExitFatal
	}

	if write {
		r, err := repo.FindRepository(".")
		if err != nil {
			fmt.Fprintf(ctx.Stderr, "fatal: %v\n", err)
			return ExitFatal
		}
		// 校验格式一致性
		if r.ObjectFormat != "" && r.ObjectFormat != objFormat {
			fmt.Fprintf(ctx.Stderr, "fatal: 对象哈希格式 %s 与仓库配置 %s 不一致\n", objFormat, r.ObjectFormat)
			return ExitFatal
		}
		_, err = object.WriteLooseObjectToDir(r.ObjectsDir, objType, content)
		if err != nil {
			fmt.Fprintf(ctx.Stderr, "fatal: 写入对象失败: %v\n", err)
			return ExitFatal
		}
	}

	fmt.Fprintln(ctx.Stdout, hashStr)
	return ExitSuccess
}
