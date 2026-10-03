package cleaner

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestCleanerManualCleanup(t *testing.T) {
	tmpDir := t.TempDir()
	lockFile := filepath.Join(tmpDir, "index.lock")
	_ = os.WriteFile(lockFile, []byte("locked"), 0644)

	unregister := Register(lockFile)
	_ = unregister // 不调用 unregister，模拟进程异常中断

	Cleanup()

	if _, err := os.Stat(lockFile); !os.IsNotExist(err) {
		t.Fatalf("expected lock file to be cleaned up, but it still exists")
	}
}

func TestCleanerUnregisterPreservesFile(t *testing.T) {
	tmpDir := t.TempDir()
	dataFile := filepath.Join(tmpDir, "pack.idx")
	_ = os.WriteFile(dataFile, []byte("data"), 0644)

	unregister := Register(dataFile)
	unregister() // 正常完成，取消跟踪

	Cleanup()

	if _, err := os.Stat(dataFile); err != nil {
		t.Fatalf("expected data file to be preserved, but got error: %v", err)
	}
}

func TestCleanerContextCancellation(t *testing.T) {
	tmpDir := t.TempDir()
	tmpPack := filepath.Join(tmpDir, ".tmp_pack_12345")
	_ = os.WriteFile(tmpPack, []byte("partial pack"), 0644)

	ctx, cancel := context.WithCancel(context.Background())
	Listen(ctx)

	Register(tmpPack)

	// 模拟接收中断信号取消 context
	cancel()

	// 等待后台清理完成
	time.Sleep(20 * time.Millisecond)

	if _, err := os.Stat(tmpPack); !os.IsNotExist(err) {
		t.Fatalf("expected temp pack to be cleaned up on context cancel, but it still exists")
	}
}
