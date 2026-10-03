package worktree

import (
	"gogit/internal/index"
	"gogit/internal/object"
	"gogit/internal/repo"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestFSMonitorHookIntegrationAndNativeGitOracle(t *testing.T) {
	tmpDir := t.TempDir()

	r, err := repo.InitRepository(tmpDir, false, "main")
	if err != nil {
		t.Fatalf("InitRepository failed: %v", err)
	}

	// 1. 创建待跟踪测试文件
	_ = os.WriteFile(filepath.Join(tmpDir, "file1.txt"), []byte("hello 1"), 0644)
	_ = os.WriteFile(filepath.Join(tmpDir, "file2.txt"), []byte("hello 2"), 0644)

	idx := index.NewIndex()
	for _, p := range []string{"file1.txt", "file2.txt"} {
		data, _ := os.ReadFile(filepath.Join(tmpDir, p))
		h, _ := r.WriteBlob(data)
		fi, _ := os.Stat(filepath.Join(tmpDir, p))
		entry := index.EntryFromOSFileInfo(p, fi, h)
		idx.AddOrReplaceEntry(entry)
	}
	_ = idx.WriteIndex(r.IndexPath)

	treeHash, _ := idx.WriteTree(r.ObjectsDir)
	sig := object.Signature{Name: "Tester", Email: "tester@google.com", When: time.Now()}
	commit := &object.Commit{
		Tree:      treeHash,
		Author:    sig,
		Committer: sig,
		Message:   "init\n",
	}
	cHash, _ := r.WriteCommit(commit)
	_ = r.Refs.UpdateRef("refs/heads/main", cHash, sig, "init")

	// 2. 创建 Mock FSMonitor 钩子脚本
	hookFile := filepath.Join(tmpDir, "fsmonitor-mock.sh")
	changedListFile := filepath.Join(tmpDir, "mock-changed.txt")
	tokenRecordFile := filepath.Join(tmpDir, "mock-token.log")

	script := `#!/bin/sh
echo "$1 $2" >> "` + tokenRecordFile + `"
echo "token_step_1"
if [ -f "` + changedListFile + `" ]; then
    cat "` + changedListFile + `"
fi
`
	if err := os.WriteFile(hookFile, []byte(script), 0755); err != nil {
		t.Fatalf("failed to write mock hook: %v", err)
	}

	r.Config.Set("core", "fsmonitor", hookFile)
	_ = os.WriteFile(filepath.Join(r.GitDir, "config"), r.Config.Serialize(), 0644)

	// 3. 第一次执行 ComputeStatus：无文件发生变更
	res1, err := ComputeStatus(r)
	if err != nil {
		t.Fatalf("ComputeStatus failed: %v", err)
	}
	// 预期工作区全部干净
	for _, it := range res1.Items {
		if it.Path == "file1.txt" || it.Path == "file2.txt" {
			t.Fatalf("file %s should be clean, staged: %c, unstaged: %c", it.Path, it.Staged, it.Unstaged)
		}
	}

	// 检查 index 中是否已成功打上 FSMN 扩展
	idxReload, err := index.ReadIndex(r.IndexPath)
	if err != nil {
		t.Fatalf("ReadIndex failed: %v", err)
	}
	fsmnExt, err := idxReload.GetFSMonitorExtension()
	if err != nil || fsmnExt == nil {
		t.Fatalf("FSMN extension should be present in index, err: %v", err)
	}
	if fsmnExt.Token != "token_step_1" {
		t.Errorf("expected token_step_1, got %s", fsmnExt.Token)
	}

	// 4. 模拟 file2.txt 变更并在 hook 输出中增加 file2.txt
	_ = os.WriteFile(filepath.Join(tmpDir, "file2.txt"), []byte("hello 2 modified"), 0644)
	_ = os.WriteFile(changedListFile, []byte("file2.txt\n"), 0644)

	res2, err := ComputeStatus(r)
	if err != nil {
		t.Fatalf("ComputeStatus second run failed: %v", err)
	}

	foundFile2Modified := false
	for _, it := range res2.Items {
		if it.Path == "file2.txt" && it.Unstaged == 'M' {
			foundFile2Modified = true
		}
		if it.Path == "file1.txt" {
			t.Errorf("file1.txt should remain untouched and skipped")
		}
	}
	if !foundFile2Modified {
		t.Fatalf("expected file2.txt to be detected as modified unstaged ('M')")
	}

	// 5. 原生 Git Oracle 交叉验证：验证 git fsck 和 git status 对生成的包含 FSMN 索引能够无缝兼容
	gitCmd := exec.Command("git", "status")
	gitCmd.Dir = tmpDir
	gitCmd.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null")
	output, err := gitCmd.CombinedOutput()
	if err != nil {
		t.Fatalf("native git status failed on FSMN index: %v\nOutput: %s", err, string(output))
	}

	outStr := string(output)
	if !strings.Contains(outStr, "file2.txt") {
		t.Fatalf("native git status did not report modified file2.txt:\n%s", outStr)
	}

	fsckCmd := exec.Command("git", "fsck")
	fsckCmd.Dir = tmpDir
	fsckCmd.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null")
	if fsckOut, err := fsckCmd.CombinedOutput(); err != nil {
		t.Fatalf("native git fsck failed on FSMN index: %v\nOutput: %s", err, string(fsckOut))
	}
}
