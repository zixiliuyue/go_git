package cli

import (
	"bytes"
	"context"
	"errors"
	"gogit/internal/cleaner"
	"gogit/internal/giterr"
	"gogit/internal/index"
	"gogit/internal/object"
	"gogit/internal/repo"
	"os"
	"path/filepath"
	"testing"
)

// TestCLIContextCancelGracefulInterrupt 测试信号或 Context 取消时返回退出码 130 并自动清理锁文件
func TestCLIContextCancelGracefulInterrupt(t *testing.T) {
	tmpDir := t.TempDir()
	r, err := repo.InitRepository(tmpDir, false, "main")
	if err != nil {
		t.Fatalf("InitRepository failed: %v", err)
	}

	origWd, _ := os.Getwd()
	defer os.Chdir(origWd)
	_ = os.Chdir(r.WorkTree)

	// 注册一个模拟的长耗时命令，在执行过程中取消 context 并创建 index.lock
	lockPath := filepath.Join(r.GitDir, "index.lock")
	started := make(chan struct{})
	Register("test-interrupt", func(ctx *Context) int {
		// 模拟创建临时锁文件并登记到 cleaner
		_ = os.WriteFile(lockPath, []byte("in-progress"), 0644)
		cleaner.Register(lockPath)

		close(started) // 告知外部锁已就绪

		// 模拟被信号中断
		<-ctx.Context.Done()
		return ExitInterrupted
	})

	ctx, cancel := context.WithCancel(context.Background())

	// 确保在锁建立并登记后触发取消信号 (模拟 Ctrl+C)
	go func() {
		<-started
		cancel()
	}()

	var stdout, stderr bytes.Buffer
	code := RunWithContext(&Context{
		Context: ctx,
		Args:    []string{"test-interrupt"},
		Stdout:  &stdout,
		Stderr:  &stderr,
	})

	// 验证退出码与 Git 对齐为 130 (SIGINT中断: 128 + 2)
	if code != ExitInterrupted {
		t.Fatalf("expected exit code %d (ExitInterrupted), got %d", ExitInterrupted, code)
	}

	// 验证事务性资源清理：确保 index.lock 被自动清理，没有遗留僵死锁
	if _, err := os.Stat(lockPath); !os.IsNotExist(err) {
		t.Fatalf("expected lock file %s to be cleaned up after interrupt, but it still exists", lockPath)
	}
}

// TestDomainErrorsVerification 验证结构化领域错误 errors.Is 匹配与 Git 退出码映射
func TestDomainErrorsVerification(t *testing.T) {
	tmpDir := t.TempDir()
	r, err := repo.InitRepository(tmpDir, false, "main")
	if err != nil {
		t.Fatalf("InitRepository failed: %v", err)
	}

	// 1. 验证对象不存在返回 ErrObjectNotFound
	nonExistentOID := object.MustHashFromHex("abcdefabcdefabcdefabcdefabcdefabcdefabcd")
	_, err = r.ReadObject(nonExistentOID)
	if err == nil {
		t.Fatalf("expected error reading non-existent object")
	}
	if !errors.Is(err, giterr.ErrObjectNotFound) {
		t.Errorf("expected error to match giterr.ErrObjectNotFound, got: %v", err)
	}
	if code := giterr.ExitCodeForError(err); code != ExitFatal {
		t.Errorf("expected fatal exit code 128 for ErrObjectNotFound, got %d", code)
	}

	// 2. 验证索引校验和损坏返回 ErrCorruptedIndex
	idxPath := filepath.Join(r.GitDir, "index")
	_ = os.WriteFile(idxPath, []byte("DIRC corrupted invalid bytes here 1234567890"), 0644)
	_, err = index.ReadIndex(idxPath)
	if err == nil {
		t.Fatalf("expected error reading corrupted index")
	}
	if !errors.Is(err, giterr.ErrCorruptedIndex) {
		t.Errorf("expected error to match giterr.ErrCorruptedIndex, got: %v", err)
	}
	if code := giterr.ExitCodeForError(err); code != ExitFatal {
		t.Errorf("expected fatal exit code 128 for ErrCorruptedIndex, got %d", code)
	}

	// 3. 验证合并冲突错误映射为退出码 1 (ExitGeneral)
	conflictErr := &giterr.MergeConflictError{Path: "conflict.txt", Reason: "content conflict"}
	if !errors.Is(conflictErr, giterr.ErrMergeConflict) {
		t.Errorf("expected conflictErr to match giterr.ErrMergeConflict")
	}
	if code := giterr.ExitCodeForError(conflictErr); code != ExitGeneral {
		t.Errorf("expected exit code 1 for ErrMergeConflict, got %d", code)
	}
}
