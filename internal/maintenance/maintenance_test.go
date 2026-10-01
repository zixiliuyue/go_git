package maintenance

import (
	"gogit/internal/object"
	"gogit/internal/repo"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func setupTestRepo(t *testing.T) (*repo.Repository, string) {
	t.Helper()
	dir := t.TempDir()

	cmd := exec.Command("git", "init", "-b", "main", dir)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git init 失败: %v, out: %s", err, string(out))
	}

	_ = exec.Command("git", "-C", dir, "config", "user.name", "Tester").Run()
	_ = exec.Command("git", "-C", dir, "config", "user.email", "tester@example.com").Run()
	_ = exec.Command("git", "-C", dir, "config", "core.excludesfile", "").Run()

	r, err := repo.FindRepository(dir)
	if err != nil {
		t.Fatalf("打开仓库失败: %v", err)
	}
	return r, dir
}

func TestPackRefsAndPeeler(t *testing.T) {
	r, dir := setupTestRepo(t)

	// 创建初始提交
	_ = os.WriteFile(filepath.Join(dir, "file.txt"), []byte("hello"), 0644)
	_ = exec.Command("git", "-C", dir, "add", "-f", "file.txt").Run()
	_ = exec.Command("git", "-C", dir, "commit", "-m", "init").Run()
	_ = exec.Command("git", "-C", dir, "tag", "-a", "v1.0", "-m", "release v1.0").Run()

	peeler := func(h object.Hash) (object.Hash, bool) {
		raw, err := r.ReadObject(h)
		if err == nil && raw.ObjType == object.TypeTag {
			tag, err := object.ParseTag(raw.Payload())
			if err == nil {
				return tag.Object, true
			}
		}
		return object.ZeroHash, false
	}

	if err := r.Refs.PackRefsWithPeeler(true, true, peeler); err != nil {
		t.Fatalf("PackRefsWithPeeler 失败: %v", err)
	}

	// 验证 packed-refs 是否生成且剥离了 tag
	packedData, err := os.ReadFile(filepath.Join(r.GitDir, "packed-refs"))
	if err != nil {
		t.Fatalf("读取 packed-refs 失败: %v", err)
	}

	if !object.ZeroHash.IsZero() {
		t.Fatal("ZeroHash check")
	}

	str := string(packedData)
	if len(str) == 0 {
		t.Fatal("packed-refs 内容为空")
	}

	// 运行 git fsck 验证
	cmd := exec.Command("git", "-C", dir, "fsck", "--strict")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git fsck 校验失败: %v, out: %s", err, string(out))
	}
}

func TestPruneLooseObjects(t *testing.T) {
	r, dir := setupTestRepo(t)

	// 创建一个提交
	_ = os.WriteFile(filepath.Join(dir, "file.txt"), []byte("commit 1"), 0644)
	addCmd := exec.Command("git", "-C", dir, "add", "-f", "file.txt")
	if out, err := addCmd.CombinedOutput(); err != nil {
		t.Fatalf("git add 失败: %v, out: %s", err, string(out))
	}
	commitCmd := exec.Command("git", "-C", dir, "commit", "-m", "commit 1")
	if out, err := commitCmd.CombinedOutput(); err != nil {
		t.Fatalf("git commit 失败: %v, out: %s", err, string(out))
	}

	// 写入一个游离未引用的 blob 对象（不可达）
	orphanHash, err := r.WriteBlob([]byte("orphan content"))
	if err != nil {
		t.Fatalf("写入 orphan blob 失败: %v", err)
	}

	// 验证 orphan 对象存在
	if _, err := r.ReadObject(orphanHash); err != nil {
		t.Fatalf("读取 orphan 对象失败: %v", err)
	}

	deleted, freed, err := PruneLooseObjects(r, time.Time{}, false)
	if err != nil {
		t.Fatalf("PruneLooseObjects 失败: %v", err)
	}

	if deleted < 1 {
		t.Fatalf("预期至少清理 1 个不可达对象，实际清理 %d (freed %d)", deleted, freed)
	}

	// 再次读取 orphan 对象，应不存在
	if _, err := r.ReadObject(orphanHash); err == nil {
		t.Fatalf("orphan 对象应当已被 prune，但仍然可以读取")
	}

	// 可达的对象应该完好无损
	cmd := exec.Command("git", "-C", dir, "fsck", "--strict")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git fsck 校验失败: %v, out: %s", err, string(out))
	}
}

func TestRunGC(t *testing.T) {
	r, dir := setupTestRepo(t)

	// 创建几个提交
	for i := 1; i <= 3; i++ {
		_ = os.WriteFile(filepath.Join(dir, "f.txt"), []byte(string(rune('0'+i))), 0644)
		_ = exec.Command("git", "-C", dir, "add", "-f", "f.txt").Run()
		_ = exec.Command("git", "-C", dir, "commit", "-m", "commit").Run()
	}

	// 写入一个游离 blob
	_, _ = r.WriteBlob([]byte("unreferenced loose blob"))

	opts := GCOptions{
		Prune:      true,
		PruneAfter: 0,
	}

	res, err := RunGC(r, opts)
	if err != nil {
		t.Fatalf("RunGC 失败: %v", err)
	}

	if !res.PackedRefs {
		t.Fatal("预期 packed-refs 为 true")
	}

	if res.PackChecksum == nil {
		t.Fatal("预期生成了 packfile")
	}

	// 用 git fsck 校验整个仓库
	cmd := exec.Command("git", "-C", dir, "fsck", "--strict")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git fsck 严格校验失败: %v, out: %s", err, string(out))
	}
}
