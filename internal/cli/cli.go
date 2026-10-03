package cli

import (
	"context"
	"fmt"
	"gogit/internal/cleaner"
	"gogit/internal/giterr"
	"gogit/internal/telemetry"
	"gogit/internal/trace2"
	"io"
	"os"
	"os/signal"
	"strings"
	"syscall"
)

// ExitCode 表示命令退出的状态码，与 Git 严格对齐：
// 0: 成功
// 1: 一般错误或条件未满足（如冲突、差异等）
// 128: 致命错误（如非 git 仓库、非法参数、损坏对象等）
// 130: 信号中断退出 (SIGINT/Ctrl+C, 128 + 2)
const (
	ExitSuccess     = 0
	ExitGeneral     = 1
	ExitError       = 1
	ExitUsage       = 128
	ExitFatal       = 128
	ExitInterrupted = 130
)

// Context 包含 CLI 命令执行的运行上下文（支持 Context 全链路穿透）。
type Context struct {
	Context context.Context
	Args    []string
	Stdin   io.Reader
	Stdout  io.Writer
	Stderr  io.Writer
}

// ContextOrDefault 获取有效 context，若未提供则默认回退到 context.Background()
func (ctx *Context) ContextOrDefault() context.Context {
	if ctx != nil && ctx.Context != nil {
		return ctx.Context
	}
	return context.Background()
}

// Command 定义子命令接口
type Command func(ctx *Context) int

var commands = make(map[string]Command)

// Register 注册子命令处理函数。
func Register(name string, cmd Command) {
	commands[name] = cmd
}

// Run 调度并执行命令行参数，自动捕获 SIGINT/SIGTERM 信号并返回 Git 标准退出码。
func Run(args []string) int {
	// 捕获操作系统优雅中断信号（SIGINT: Ctrl+C, SIGTERM）
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	return RunWithContext(&Context{
		Context: ctx,
		Args:    args,
		Stdin:   os.Stdin,
		Stdout:  os.Stdout,
		Stderr:  os.Stderr,
	})
}

// RunWithContext 在自定义上下文中执行命令（便于测试与重定向）。
func RunWithContext(ctx *Context) int {
	// 刷新全局结构化日志配置（响应最新的环境变量）
	telemetry.RefreshLogger()

	runCtx := ctx.ContextOrDefault()
	// 启动事务性临时文件与锁文件清理监听器（一旦信号或外部取消触发，立即删除所有活跃临时资源）
	cleaner.Listen(runCtx)

	if err := runCtx.Err(); err != nil {
		cleaner.Cleanup()
		return giterr.ExitCodeForError(err)
	}

	// 1. 提取全局 Profiling 诊断参数与通用全局参数
	var cleanArgs []string
	cpuProfile := ""
	memProfile := ""
	traceProfile := ""

	for i := 0; i < len(ctx.Args); i++ {
		arg := ctx.Args[i]
		if strings.HasPrefix(arg, "--cpuprofile=") {
			cpuProfile = strings.TrimPrefix(arg, "--cpuprofile=")
		} else if arg == "--cpuprofile" && i+1 < len(ctx.Args) {
			cpuProfile = ctx.Args[i+1]
			i++
		} else if strings.HasPrefix(arg, "--memprofile=") {
			memProfile = strings.TrimPrefix(arg, "--memprofile=")
		} else if arg == "--memprofile" && i+1 < len(ctx.Args) {
			memProfile = ctx.Args[i+1]
			i++
		} else if strings.HasPrefix(arg, "--trace=") {
			traceProfile = strings.TrimPrefix(arg, "--trace=")
		} else if arg == "-C" && i+1 < len(ctx.Args) {
			targetDir := ctx.Args[i+1]
			if err := os.Chdir(targetDir); err != nil {
				fmt.Fprintf(ctx.Stderr, "fatal: 无法切换工作目录到 '%s': %v\n", targetDir, err)
				return ExitFatal
			}
			i++
		} else if strings.HasPrefix(arg, "-C") {
			targetDir := strings.TrimPrefix(arg, "-C")
			if err := os.Chdir(targetDir); err != nil {
				fmt.Fprintf(ctx.Stderr, "fatal: 无法切换工作目录到 '%s': %v\n", targetDir, err)
				return ExitFatal
			}
		} else {
			cleanArgs = append(cleanArgs, arg)
		}
	}

	// 启动 Profiler（若指定了任意 profile 参数）
	prof, err := telemetry.StartProfiler(cpuProfile, memProfile, traceProfile)
	if err != nil {
		fmt.Fprintf(ctx.Stderr, "fatal: 启动性能分析采集失败: %v\n", err)
		return ExitFatal
	}
	if prof != nil {
		defer func() {
			if err := prof.Stop(); err != nil {
				fmt.Fprintf(ctx.Stderr, "warning: 停止性能分析器失败: %v\n", err)
			}
		}()
	}

	if len(cleanArgs) == 0 {
		fmt.Fprintln(ctx.Stderr, "用法: gogit [--cpuprofile=<file>] [--memprofile=<file>] [--trace=<file>] <命令> [<参数>...]")
		return ExitGeneral
	}

	cmdName := cleanArgs[0]
	subArgs := cleanArgs[1:]

	// 2. 初始化 Git Trace2 遥测会话
	trSession := trace2.Initialize(cmdName, cleanArgs)
	exitCode := ExitSuccess
	defer func() {
		if trSession != nil {
			trace2.Close(exitCode)
		}
	}()

	if cmdName == "--help" || cmdName == "-h" {
		fmt.Fprintln(ctx.Stdout, "支持的命令: init, hash-object, cat-file, ls-tree, write-tree, commit-tree, add, commit, rev-parse, log, fsck ...")
		return ExitSuccess
	}

	cmd, ok := commands[cmdName]
	if !ok {
		fmt.Fprintf(ctx.Stderr, "gogit: '%s' 不是一个有效命令。\n", cmdName)
		exitCode = ExitFatal
		return exitCode
	}

	subCtx := &Context{
		Context: runCtx,
		Args:    subArgs,
		Stdin:   ctx.Stdin,
		Stdout:  ctx.Stdout,
		Stderr:  ctx.Stderr,
	}

	exitCode = cmd(subCtx)

	// 若在子命令执行期间触发了 Context 取消或信号中断，确保执行事务清理并返回 Git 标准中断退出码 130
	if runCtx.Err() != nil {
		cleaner.Cleanup()
		exitCode = giterr.ExitCodeForError(runCtx.Err())
	}

	return exitCode
}
