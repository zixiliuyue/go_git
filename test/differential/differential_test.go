package differential

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"testing"
	"time"
)

// buildGogitForTest 编译临时 gogit 二进制供差分引擎调用
func buildGogitForTest(t *testing.T) string {
	t.Helper()
	tempBin := filepath.Join(os.TempDir(), "gogit-difftest-bin")
	cmd := exec.Command("go", "build", "-o", tempBin, "gogit/cmd/gogit")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("编译 gogit 失败: %v, out: %s", err, string(out))
	}
	return tempBin
}

// TestPropertyBasedDifferential 基于属性的随机拓扑与文件变动差分测试
func TestPropertyBasedDifferential(t *testing.T) {
	binPath := buildGogitForTest(t)
	defer os.Remove(binPath)

	seeds := []int64{1001, 2026, 8888}
	if envSeed := os.Getenv("SEED"); envSeed != "" {
		if s, err := strconv.ParseInt(envSeed, 10, 64); err == nil {
			seeds = []int64{s}
		}
	}

	stepsPerSeed := 25
	if envSteps := os.Getenv("DIFF_STEPS"); envSteps != "" {
		if st, err := strconv.Atoi(envSteps); err == nil && st > 0 {
			stepsPerSeed = st
		}
	}

	for _, seed := range seeds {
		t.Run("Seed_"+strconv.FormatInt(seed, 10), func(t *testing.T) {
			engine := NewDifferentialEngine(t, seed, binPath)
			defer engine.Cleanup()

			for i := 0; i < stepsPerSeed; i++ {
				action := engine.GenerateRandomAction()
				engine.ExecuteAndAssert(action)
			}
		})
	}
}

// TestDifferentialCornerCases 专门针对极端边界场景的双轨差分比对
func TestDifferentialCornerCases(t *testing.T) {
	binPath := buildGogitForTest(t)
	defer os.Remove(binPath)

	seed := time.Now().UnixNano()
	engine := NewDifferentialEngine(t, seed, binPath)
	defer engine.Cleanup()

	// 1. 深度嵌套目录与空文件测试
	engine.ExecuteAndAssert(Action{
		Type:      ActionCreateFile,
		Path:      "deeply/nested/path/to/a/b/c/empty.go",
		Content:   []byte{},
		Timestamp: 1700000000,
	})
	engine.ExecuteAndAssert(Action{Type: ActionGitAdd, Path: "."})
	engine.ExecuteAndAssert(Action{Type: ActionGitCommit, CommitMsg: "add empty file in deep tree", Timestamp: 1700000000})

	// 2. 特殊二进制字符与 NULL 字节测试
	engine.ExecuteAndAssert(Action{
		Type:      ActionCreateFile,
		Path:      "binary_null_test.dat",
		Content:   []byte("\x00\x01\x02\xFF\xFE\x00\xAA\xBB"),
		Timestamp: 1700000060,
	})
	engine.ExecuteAndAssert(Action{Type: ActionGitAdd, Path: "."})
	engine.ExecuteAndAssert(Action{Type: ActionGitCommit, CommitMsg: "add binary file", Timestamp: 1700000060})

	// 3. 跨目录移动/重命名文件
	engine.ExecuteAndAssert(Action{
		Type:      ActionRenameFile,
		OldPath:   "deeply/nested/path/to/a/b/c/empty.go",
		Path:      "moved/relocated_empty.go",
		Timestamp: 1700000120,
	})
	engine.ExecuteAndAssert(Action{Type: ActionGitAdd, Path: "."})
	engine.ExecuteAndAssert(Action{Type: ActionGitCommit, CommitMsg: "relocate file across dirs", Timestamp: 1700000120})

	// 4. 分支创建、切换与文件增量修改
	engine.ExecuteAndAssert(Action{Type: ActionGitBranch, Branch: "feature-branch", Timestamp: 1700000180})
	engine.ExecuteAndAssert(Action{Type: ActionGitCheckout, Branch: "feature-branch", Timestamp: 1700000180})
	engine.ExecuteAndAssert(Action{
		Type:      ActionCreateFile,
		Path:      "branch_feature.go",
		Content:   []byte("package feature\nfunc Run() {}\n"),
		Timestamp: 1700000240,
	})
	engine.ExecuteAndAssert(Action{Type: ActionGitAdd, Path: "."})
	engine.ExecuteAndAssert(Action{Type: ActionGitCommit, CommitMsg: "feature commit on branch", Timestamp: 1700000240})

	// 5. 切回主分支并验证状态隔离
	engine.ExecuteAndAssert(Action{Type: ActionGitCheckout, Branch: "main", Timestamp: 1700000300})

	// 6. 执行分支合并（Fast-Forward Merge），断言两边 Tree 与 Status 100% 对齐
	engine.ExecuteAndAssert(Action{Type: ActionGitMerge, Branch: "feature-branch", Timestamp: 1700000360})
}
