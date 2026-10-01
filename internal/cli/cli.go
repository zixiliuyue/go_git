package cli

import (
	"fmt"
	"io"
	"os"
)

// ExitCode 表示命令退出的状态码，与 Git 严格对齐：
// 0: 成功
// 1: 一般错误或条件未满足
// 128: 致命错误（如非 git 仓库、非法参数等）
const (
	ExitSuccess = 0
	ExitGeneral = 1
	ExitError   = 1
	ExitFatal   = 128
)

// Context 包含 CLI 命令执行的运行上下文。
type Context struct {
	Args   []string
	Stdin  io.Reader
	Stdout io.Writer
	Stderr io.Writer
}

// Command 定义子命令接口
type Command func(ctx *Context) int

var commands = make(map[string]Command)

// Register 注册子命令处理函数。
func Register(name string, cmd Command) {
	commands[name] = cmd
}

// Run 调度并执行命令行参数，返回退出码。
func Run(args []string) int {
	return RunWithContext(&Context{
		Args:   args,
		Stdin:  os.Stdin,
		Stdout: os.Stdout,
		Stderr: os.Stderr,
	})
}

// RunWithContext 在自定义上下文中执行命令（便于测试与重定向）。
func RunWithContext(ctx *Context) int {
	if len(ctx.Args) == 0 {
		fmt.Fprintln(ctx.Stderr, "用法: gogit <命令> [<参数>...]")
		return ExitGeneral
	}

	cmdName := ctx.Args[0]
	subArgs := ctx.Args[1:]

	if cmdName == "--help" || cmdName == "-h" {
		fmt.Fprintln(ctx.Stdout, "支持的命令: init, hash-object, cat-file, ls-tree, write-tree, commit-tree, add, commit, rev-parse, log, fsck ...")
		return ExitSuccess
	}

	cmd, ok := commands[cmdName]
	if !ok {
		fmt.Fprintf(ctx.Stderr, "gogit: '%s' 不是一个有效命令。\n", cmdName)
		return ExitFatal
	}

	subCtx := &Context{
		Args:   subArgs,
		Stdin:  ctx.Stdin,
		Stdout: ctx.Stdout,
		Stderr: ctx.Stderr,
	}

	return cmd(subCtx)
}
