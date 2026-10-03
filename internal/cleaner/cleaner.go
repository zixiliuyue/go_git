package cleaner

import (
	"context"
	"os"
	"sync"
)

var (
	mu            sync.Mutex
	tempFiles     = make(map[string]struct{})
	cleanupFuncs  = make(map[int]func())
	funcCounter   int
	listenOnce    sync.Once
	listenerCtx   context.Context
	listenerClose context.CancelFunc
)

// Register 注册一个处于活跃事务中的临时文件或锁文件（如 index.lock, .tmp_pack_*）。
// 返回一个 unregister 函数，当操作成功提交后调用该函数取消跟踪，防止被意外删除。
func Register(path string) func() {
	if path == "" {
		return func() {}
	}
	mu.Lock()
	tempFiles[path] = struct{}{}
	mu.Unlock()

	return func() {
		Unregister(path)
	}
}

// Unregister 手动取消对指定临时文件的事务跟踪
func Unregister(path string) {
	mu.Lock()
	delete(tempFiles, path)
	mu.Unlock()
}

// RegisterFunc 注册一个自定义资源清理钩子
func RegisterFunc(fn func()) func() {
	if fn == nil {
		return func() {}
	}
	mu.Lock()
	funcCounter++
	id := funcCounter
	cleanupFuncs[id] = fn
	mu.Unlock()

	return func() {
		mu.Lock()
		delete(cleanupFuncs, id)
		mu.Unlock()
	}
}

// Cleanup 执行所有已注册的事务性清理：删除锁文件、临时文件并触发清理钩子
func Cleanup() {
	mu.Lock()
	defer mu.Unlock()

	// 1. 删除所有孤儿临时文件与锁文件
	for f := range tempFiles {
		_ = os.Remove(f)
	}
	clear(tempFiles)

	// 2. 执行自定义钩子
	for _, fn := range cleanupFuncs {
		fn()
	}
	clear(cleanupFuncs)
}

// Listen 启动信号/上下文监听器，一旦 context 被取消（如接收到 SIGINT），立即执行事务清理
func Listen(ctx context.Context) {
	if ctx == nil {
		return
	}
	mu.Lock()
	if listenerClose != nil {
		listenerClose()
	}
	subCtx, cancel := context.WithCancel(ctx)
	listenerCtx = subCtx
	listenerClose = cancel
	mu.Unlock()

	go func() {
		<-subCtx.Done()
		Cleanup()
	}()
}
