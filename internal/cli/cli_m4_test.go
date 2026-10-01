package cli

import (
	"bytes"
	"gogit/internal/object"
	"gogit/internal/repo"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestM4CloneFetchPullPush(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "gogit-m4-test-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	// 1. 创建源裸仓库 bare.git
	barePath := filepath.Join(tmpDir, "bare.git")
	bareRepo, err := repo.InitRepository(barePath, true, "main")
	if err != nil {
		t.Fatal(err)
	}

	// 往裸仓库注入一个初始提交
	blobOID, err := bareRepo.WriteBlob([]byte("initial content for clone test\n"))
	if err != nil {
		t.Fatal(err)
	}
	tree := &object.Tree{
		Entries: []object.TreeEntry{
			{Mode: object.ModeRegular, Name: "welcome.txt", OID: blobOID},
		},
	}
	treeOID, err := bareRepo.WriteTree(tree)
	if err != nil {
		t.Fatal(err)
	}
	sig := bareRepo.AuthorSignature()
	commit := &object.Commit{
		Tree:      treeOID,
		Author:    sig,
		Committer: sig,
		Message:   "Initial commit in bare\n",
	}
	commitOID, err := bareRepo.WriteCommit(commit)
	if err != nil {
		t.Fatal(err)
	}
	_ = bareRepo.Refs.UpdateRef("refs/heads/main", commitOID, sig, "init")
	_ = bareRepo.Refs.SetHEADSymbolic("refs/heads/main")

	// 2. gogit clone 测试
	clonedPath := filepath.Join(tmpDir, "work-clone")
	var stdout, stderr bytes.Buffer
	ctxClone := &Context{
		Args:   []string{barePath, clonedPath},
		Stdout: &stdout,
		Stderr: &stderr,
	}
	code := cmdClone(ctxClone)
	if code != ExitSuccess {
		t.Fatalf("gogit clone 失败: code=%d, stderr=%s", code, stderr.String())
	}

	// 验证克隆后的工作区文件
	welcomeFile := filepath.Join(clonedPath, "welcome.txt")
	content, err := os.ReadFile(welcomeFile)
	if err != nil {
		t.Fatalf("读取克隆后的文件失败: %v", err)
	}
	if string(content) != "initial content for clone test\n" {
		t.Fatalf("克隆后文件内容不匹配: %s", string(content))
	}

	// 3. gogit remote 子命令测试
	origDir, _ := os.Getwd()
	_ = os.Chdir(clonedPath)
	defer os.Chdir(origDir)

	stdout.Reset()
	stderr.Reset()
	code = cmdRemote(&Context{Args: []string{"-v"}, Stdout: &stdout, Stderr: &stderr})
	if code != ExitSuccess || !strings.Contains(stdout.String(), "origin") {
		t.Fatalf("gogit remote -v 失败: %s", stdout.String())
	}

	// 4. 源仓库增加新提交，测试 gogit fetch
	blob2OID, err := bareRepo.WriteBlob([]byte("second file added\n"))
	if err != nil {
		t.Fatal(err)
	}
	tree2 := &object.Tree{
		Entries: []object.TreeEntry{
			{Mode: object.ModeRegular, Name: "file2.txt", OID: blob2OID},
			{Mode: object.ModeRegular, Name: "welcome.txt", OID: blobOID},
		},
	}
	tree2OID, err := bareRepo.WriteTree(tree2)
	if err != nil {
		t.Fatal(err)
	}
	commit2 := &object.Commit{
		Tree:      tree2OID,
		Parents:   []object.Hash{commitOID},
		Author:    sig,
		Committer: sig,
		Message:   "Second commit in bare\n",
	}
	commit2OID, err := bareRepo.WriteCommit(commit2)
	if err != nil {
		t.Fatal(err)
	}
	_ = bareRepo.Refs.UpdateRef("refs/heads/main", commit2OID, sig, "update")

	stdout.Reset()
	stderr.Reset()
	code = cmdFetch(&Context{Args: []string{"origin"}, Stdout: &stdout, Stderr: &stderr})
	if code != ExitSuccess {
		t.Fatalf("gogit fetch 失败: code=%d, stderr=%s", code, stderr.String())
	}

	// 验证跟踪分支已更新
	clonedRepo, err := repo.FindRepository(clonedPath)
	if err != nil {
		t.Fatal(err)
	}
	trackingRef, err := clonedRepo.Refs.GetRef("refs/remotes/origin/main")
	if err != nil || trackingRef.Hash != commit2OID {
		t.Fatalf("fetch 后跟踪分支未指向新提交: %v", trackingRef)
	}

	// 5. 测试 gogit pull
	stdout.Reset()
	stderr.Reset()
	code = cmdPull(&Context{Args: []string{}, Stdout: &stdout, Stderr: &stderr})
	if code != ExitSuccess {
		t.Fatalf("gogit pull 失败: code=%d, stderr=%s", code, stderr.String())
	}

	file2 := filepath.Join(clonedPath, "file2.txt")
	if _, err := os.Stat(file2); err != nil {
		t.Fatalf("pull 后工作区未更新 file2.txt: %v", err)
	}

	// 6. 在克隆仓库创建本地提交，测试 gogit push 回源仓库
	file3 := filepath.Join(clonedPath, "file3.txt")
	_ = os.WriteFile(file3, []byte("local commit to push\n"), 0644)
	_ = cmdAdd(&Context{Args: []string{"file3.txt"}, Stdout: &stdout, Stderr: &stderr})
	_ = cmdCommit(&Context{Args: []string{"-m", "Third commit locally"}, Stdout: &stdout, Stderr: &stderr})

	stdout.Reset()
	stderr.Reset()
	code = cmdPush(&Context{Args: []string{"origin", "main"}, Stdout: &stdout, Stderr: &stderr})
	if code != ExitSuccess {
		t.Fatalf("gogit push 失败: code=%d, stderr=%s", code, stderr.String())
	}

	// 7. 验证裸仓库收到了本地推送的新提交
	bareHead, err := bareRepo.Refs.ResolveHEAD()
	if err != nil {
		t.Fatal(err)
	}
	localHead, err := clonedRepo.Refs.ResolveHEAD()
	if err != nil {
		t.Fatal(err)
	}
	if bareHead != localHead {
		t.Fatalf("push 后裸仓库 HEAD 与本地不同步: %s != %s", bareHead.String(), localHead.String())
	}

	// 8. 原生 git fsck --strict 跨系统验收
	if gitPath, err := exec.LookPath("git"); err == nil && gitPath != "" {
		cmd := exec.Command("git", "fsck", "--strict")
		cmd.Dir = barePath
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git fsck 在 bare.git 上失败: %v, 输出: %s", err, string(out))
		}
		t.Logf("bare.git fsck: %s", string(out))

		cmd2 := exec.Command("git", "fsck", "--strict")
		cmd2.Dir = clonedPath
		out2, err := cmd2.CombinedOutput()
		if err != nil {
			t.Fatalf("git fsck 在 work-clone 上失败: %v, 输出: %s", err, string(out2))
		}
		t.Logf("work-clone fsck: %s", string(out2))
	}
}
