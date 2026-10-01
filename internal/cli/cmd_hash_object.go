package cli

import (
	"fmt"
	"gogit/internal/object"
	"gogit/internal/repo"
	"io"
	"os"
)

func init() {
	Register("hash-object", cmdHashObject)
}

func cmdHashObject(ctx *Context) int {
	write := false
	fromStdin := false
	objType := object.TypeBlob
	var filePath string

	for i := 0; i < len(ctx.Args); i++ {
		arg := ctx.Args[i]
		if arg == "-w" {
			write = true
		} else if arg == "--stdin" {
			fromStdin = true
		} else if arg == "-t" && i+1 < len(ctx.Args) {
			objType = object.ObjectType(ctx.Args[i+1])
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

	h := object.HashObject(objType, content)

	if write {
		r, err := repo.FindRepository(".")
		if err != nil {
			fmt.Fprintf(ctx.Stderr, "fatal: %v\n", err)
			return ExitFatal
		}
		_, err = object.WriteLooseObjectToDir(r.ObjectsDir, objType, content)
		if err != nil {
			fmt.Fprintf(ctx.Stderr, "fatal: 写入对象失败: %v\n", err)
			return ExitFatal
		}
	}

	fmt.Fprintln(ctx.Stdout, h.String())
	return ExitSuccess
}
