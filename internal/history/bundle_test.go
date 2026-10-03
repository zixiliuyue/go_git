package history

import (
	"bytes"
	"gogit/internal/index"
	"gogit/internal/object"
	"gogit/internal/pack"
	"gogit/internal/repo"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func TestBundleCreateVerifyAndNativeGitOracle(t *testing.T) {
	tmpDir := t.TempDir()

	r, err := repo.InitRepository(tmpDir, false, "main")
	if err != nil {
		t.Fatalf("InitRepository failed: %v", err)
	}

	// 1. 创建提交与分支
	_ = os.WriteFile(filepath.Join(tmpDir, "file1.txt"), []byte("hello bundle"), 0644)
	idx := index.NewIndex()
	data, _ := os.ReadFile(filepath.Join(tmpDir, "file1.txt"))
	h1, _ := r.WriteBlob(data)
	fi, _ := os.Stat(filepath.Join(tmpDir, "file1.txt"))
	idx.AddOrReplaceEntry(index.EntryFromOSFileInfo("file1.txt", fi, h1))
	_ = idx.WriteIndex(r.IndexPath)

	tree1, _ := idx.WriteTree(r.ObjectsDir)
	sig := object.Signature{Name: "Tester", Email: "tester@google.com", When: time.Now()}
	c1 := &object.Commit{Tree: tree1, Author: sig, Committer: sig, Message: "commit 1\n"}
	c1Hash, _ := r.WriteCommit(c1)
	_ = r.Refs.UpdateRef("refs/heads/main", c1Hash, sig, "commit 1")
	_ = r.Refs.SetHEADSymbolic("refs/heads/main")

	// 2. 创建 Bundle
	bundleBytes, header, err := CreateBundle(r, []string{"HEAD", "refs/heads/main"}, nil)
	if err != nil {
		t.Fatalf("CreateBundle failed: %v", err)
	}

	if len(header.References) != 2 {
		t.Fatalf("expected 2 references, got %d", len(header.References))
	}

	bundleFile := filepath.Join(t.TempDir(), "test.bundle")
	if err := os.WriteFile(bundleFile, bundleBytes, 0644); err != nil {
		t.Fatalf("failed to write bundle file: %v", err)
	}

	// 3. 原生 Git Oracle 验证：使用系统 git 运行 git bundle verify 校验 bundle
	verifyCmd := exec.Command("git", "bundle", "verify", bundleFile)
	verifyCmd.Dir = tmpDir
	verifyCmd.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null")
	if out, err := verifyCmd.CombinedOutput(); err != nil {
		t.Fatalf("native git bundle verify failed: %v\nOutput: %s", err, string(out))
	}

	// 4. 原生 Git Oracle 验证：使用系统 git 从 gogit 生成的 bundle 直接克隆出完整仓库
	nativeCloneDir := filepath.Join(t.TempDir(), "native-clone")
	cloneCmd := exec.Command("git", "clone", bundleFile, nativeCloneDir)
	cloneCmd.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null")
	if out, err := cloneCmd.CombinedOutput(); err != nil {
		t.Fatalf("native git clone from gogit bundle failed: %v\nOutput: %s", err, string(out))
	}

	// 验证克隆出的工作区文件
	clonedFileContent, err := os.ReadFile(filepath.Join(nativeCloneDir, "file1.txt"))
	if err != nil || string(clonedFileContent) != "hello bundle" {
		t.Fatalf("native clone file content mismatch: %v, content: %s", err, string(clonedFileContent))
	}

	// 5. 解包验证：在全新仓库中进行 Unbundle
	unbundleDir := filepath.Join(t.TempDir(), "unbundle-target")
	r2, err := repo.InitRepository(unbundleDir, true, "main")
	if err != nil {
		t.Fatalf("InitRepository for unbundle failed: %v", err)
	}

	hdr, packData, err := pack.ReadBundle(bytes.NewReader(bundleBytes))
	if err != nil {
		t.Fatalf("ReadBundle failed: %v", err)
	}

	if err := Unbundle(r2, hdr, packData); err != nil {
		t.Fatalf("Unbundle failed: %v", err)
	}

	if !r2.HasObject(c1Hash) {
		t.Fatalf("target repo missing commit %s after unbundle", c1Hash)
	}
}
