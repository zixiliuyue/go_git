package cli

import (
	"bytes"
	"gogit/internal/index"
	"gogit/internal/object"
	"gogit/internal/repo"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestCLIBundleAndCloneWithBundleURI(t *testing.T) {
	tmpDir := t.TempDir()
	originDir := filepath.Join(tmpDir, "origin")

	r, err := repo.InitRepository(originDir, false, "main")
	if err != nil {
		t.Fatalf("InitRepository failed: %v", err)
	}

	// 1. 创建基准提交
	_ = os.WriteFile(filepath.Join(originDir, "app.txt"), []byte("package main"), 0644)
	idx := index.NewIndex()
	data, _ := os.ReadFile(filepath.Join(originDir, "app.txt"))
	h, _ := r.WriteBlob(data)
	fi, _ := os.Stat(filepath.Join(originDir, "app.txt"))
	idx.AddOrReplaceEntry(index.EntryFromOSFileInfo("app.txt", fi, h))
	_ = idx.WriteIndex(r.IndexPath)

	tree, _ := idx.WriteTree(r.ObjectsDir)
	sig := object.Signature{Name: "Piper", Email: "piper@google.com", When: time.Now()}
	commit1 := &object.Commit{Tree: tree, Author: sig, Committer: sig, Message: "commit 1\n"}
	c1Hash, _ := r.WriteCommit(commit1)
	_ = r.Refs.UpdateRef("refs/heads/main", c1Hash, sig, "commit 1")
	_ = r.Refs.SetHEADSymbolic("refs/heads/main")

	// 2. 测试 gogit bundle create
	bundlePath := filepath.Join(tmpDir, "cdn-snapshot.bundle")
	var stdout, stderr bytes.Buffer
	origWd, _ := os.Getwd()
	defer os.Chdir(origWd)

	_ = os.Chdir(originDir)
	code := cmdBundle(&Context{
		Args:   []string{"create", bundlePath, "HEAD", "refs/heads/main"},
		Stdout: &stdout,
		Stderr: &stderr,
	})
	if code != ExitSuccess {
		t.Fatalf("cmdBundle create failed (%d): %s\n%s", code, stdout.String(), stderr.String())
	}

	// 3. 测试 gogit bundle verify
	stdout.Reset()
	stderr.Reset()
	code = cmdBundle(&Context{
		Args:   []string{"verify", bundlePath},
		Stdout: &stdout,
		Stderr: &stderr,
	})
	if code != ExitSuccess {
		t.Fatalf("cmdBundle verify failed (%d): %s\n%s", code, stdout.String(), stderr.String())
	}
	if !strings.Contains(stdout.String(), "is okay") {
		t.Fatalf("expected verify okay, got: %s", stdout.String())
	}

	// 4. 测试 gogit bundle list-heads
	stdout.Reset()
	stderr.Reset()
	code = cmdBundle(&Context{
		Args:   []string{"list-heads", bundlePath},
		Stdout: &stdout,
		Stderr: &stderr,
	})
	if code != ExitSuccess {
		t.Fatalf("cmdBundle list-heads failed (%d): %s\n%s", code, stdout.String(), stderr.String())
	}
	if !strings.Contains(stdout.String(), "refs/heads/main") {
		t.Fatalf("expected refs/heads/main in list-heads: %s", stdout.String())
	}

	// 5. 在 origin 中提交一个新改动（用于模拟增量提交）
	_ = os.WriteFile(filepath.Join(originDir, "delta.txt"), []byte("incremental content"), 0644)
	data2, _ := os.ReadFile(filepath.Join(originDir, "delta.txt"))
	h2, _ := r.WriteBlob(data2)
	fi2, _ := os.Stat(filepath.Join(originDir, "delta.txt"))
	idx.AddOrReplaceEntry(index.EntryFromOSFileInfo("delta.txt", fi2, h2))
	_ = idx.WriteIndex(r.IndexPath)

	tree2, _ := idx.WriteTree(r.ObjectsDir)
	commit2 := &object.Commit{Tree: tree2, Parents: []object.Hash{c1Hash}, Author: sig, Committer: sig, Message: "commit 2\n"}
	c2Hash, _ := r.WriteCommit(commit2)
	_ = r.Refs.UpdateRef("refs/heads/main", c2Hash, sig, "commit 2")

	// 6. 测试 gogit clone --bundle-uri=<bundle-file> <originDir> <targetDir>
	// 期望：通过 bundle 快速恢复初始快照，随后向 origin 增量拉取 commit 2！
	targetCloneDir := filepath.Join(tmpDir, "cdn-cloned")
	stdout.Reset()
	stderr.Reset()
	_ = os.Chdir(tmpDir)

	cloneCode := cmdClone(&Context{
		Args:   []string{"--bundle-uri=" + bundlePath, originDir, targetCloneDir},
		Stdout: &stdout,
		Stderr: &stderr,
	})
	if cloneCode != ExitSuccess {
		t.Fatalf("cmdClone with --bundle-uri failed (%d):\nStdout: %s\nStderr: %s", cloneCode, stdout.String(), stderr.String())
	}

	// 7. 验证目标克隆仓库中已包含由 bundle 还原的 app.txt 和通过增量拉取的 delta.txt
	appContent, err := os.ReadFile(filepath.Join(targetCloneDir, "app.txt"))
	if err != nil || string(appContent) != "package main" {
		t.Fatalf("cloned app.txt mismatch: %v, content: %s", err, string(appContent))
	}
	deltaContent, err := os.ReadFile(filepath.Join(targetCloneDir, "delta.txt"))
	if err != nil || string(deltaContent) != "incremental content" {
		t.Fatalf("cloned delta.txt mismatch: %v, content: %s", err, string(deltaContent))
	}
}
