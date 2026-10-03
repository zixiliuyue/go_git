package gogit

import (
	"context"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

// TestSDKInitAndOpen 测试通过公共 SDK 初始化与打开仓库
func TestSDKInitAndOpen(t *testing.T) {
	ctx := context.Background()
	tempDir, err := os.MkdirTemp("", "gogit-sdk-test-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tempDir)

	repoPath := filepath.Join(tempDir, "myrepo")

	// 1. 初始化标准仓库
	r, err := Init(ctx, repoPath, WithDefaultBranch("develop"))
	if err != nil {
		t.Fatalf("Init 失败: %v", err)
	}

	if r.IsBare() {
		t.Fatalf("期望非裸仓库")
	}
	if r.Path() != repoPath {
		t.Fatalf("工作区路径不匹配: %s vs %s", r.Path(), repoPath)
	}

	// 2. Open 打开该仓库
	opened, err := Open(ctx, repoPath)
	if err != nil {
		t.Fatalf("Open 失败: %v", err)
	}
	if opened.GitDir() != r.GitDir() {
		t.Fatalf("GitDir 不匹配: %s vs %s", opened.GitDir(), r.GitDir())
	}

	// 3. 初始化 Bare 仓库
	barePath := filepath.Join(tempDir, "bare.git")
	bareRepo, err := Init(ctx, barePath, WithBare(true))
	if err != nil {
		t.Fatalf("Init Bare 失败: %v", err)
	}
	if !bareRepo.IsBare() {
		t.Fatalf("期望裸仓库")
	}
}

// TestSDKWorktreeCommitAndStatus 测试工作区暂存、提交、状态查询与历史遍历
func TestSDKWorktreeCommitAndStatus(t *testing.T) {
	ctx := context.Background()
	tempDir, err := os.MkdirTemp("", "gogit-sdk-worktree-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tempDir)

	r, err := Init(ctx, tempDir, WithDefaultBranch("main"))
	if err != nil {
		t.Fatal(err)
	}

	// 1. 检查初始干净状态
	st, err := r.Status(ctx)
	if err != nil {
		t.Fatalf("Status 失败: %v", err)
	}
	if !st.IsClean {
		t.Fatalf("初始状态应为 Clean")
	}

	// 2. 创建新文件并检查未跟踪状态
	testFile := filepath.Join(tempDir, "hello.txt")
	if err := os.WriteFile(testFile, []byte("hello world\n"), 0644); err != nil {
		t.Fatal(err)
	}

	st, err = r.Status(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(st.Untracked) != 1 || st.Untracked[0] != "hello.txt" {
		t.Fatalf("未跟踪文件不符合预期: %+v", st.Untracked)
	}

	// 3. Add 暂存
	if err := r.Add(ctx, "hello.txt"); err != nil {
		t.Fatalf("Add 失败: %v", err)
	}

	st, err = r.Status(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(st.Staged) != 1 || st.Staged[0] != "hello.txt" {
		t.Fatalf("暂存区文件不符合预期: %+v", st.Staged)
	}

	// 4. CreateCommit 创建首个提交
	sig := Signature{
		Name:  "SDK Tester",
		Email: "tester@google.com",
		When:  time.Now(),
	}
	c1, err := r.CreateCommit(ctx, "feat: initial commit", WithAuthor(sig), WithCommitter(sig))
	if err != nil {
		t.Fatalf("CreateCommit 失败: %v", err)
	}
	if c1.Hash == "" || c1.TreeHash == "" {
		t.Fatalf("提交生成哈希为空")
	}
	if len(c1.Parents) != 0 {
		t.Fatalf("初始提交不应包含父提交")
	}

	// 验证状态变为 Clean
	st, err = r.Status(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !st.IsClean {
		t.Fatalf("提交后状态应为 Clean")
	}

	// 5. 创建第二个提交
	file2 := filepath.Join(tempDir, "doc.md")
	_ = os.WriteFile(file2, []byte("# Documentation\n"), 0644)
	if err := r.Add(ctx, "doc.md"); err != nil {
		t.Fatal(err)
	}
	sig2 := sig
	sig2.When = sig.When.Add(2 * time.Second)
	c2, err := r.CreateCommit(ctx, "docs: add documentation", WithAuthor(sig2), WithCommitter(sig2))
	if err != nil {
		t.Fatal(err)
	}
	if len(c2.Parents) != 1 || c2.Parents[0] != c1.Hash {
		t.Fatalf("第二提交父哈希不匹配: 期望 %s, 实际 %v", c1.Hash, c2.Parents)
	}

	// 6. 验证 Commit 查询
	headCommit, err := r.Commit(ctx, "HEAD")
	if err != nil {
		t.Fatalf("查询 HEAD 失败: %v", err)
	}
	if headCommit.Hash != c2.Hash {
		t.Fatalf("HEAD 提交哈希不匹配: %s vs %s", headCommit.Hash, c2.Hash)
	}

	// 7. 验证 Commits 历史遍历
	history, err := r.Commits(ctx, "HEAD", 10)
	if err != nil {
		t.Fatalf("Commits 历史遍历失败: %v", err)
	}
	if len(history) != 2 {
		t.Fatalf("期望遍历到 2 次提交，实际为 %d", len(history))
	}
	if history[0].Hash != c2.Hash || history[1].Hash != c1.Hash {
		t.Fatalf("历史拓扑排序错误")
	}

	// 8. 验证 Tree 与 ReadBlob
	tree, err := r.Tree(ctx, "HEAD")
	if err != nil {
		t.Fatalf("Tree 读取失败: %v", err)
	}
	if len(tree.Entries) != 2 {
		t.Fatalf("期望 Tree 包含 2 个条目，实际: %d", len(tree.Entries))
	}

	blobContent, err := r.ReadBlob(ctx, tree.Entries[0].Hash)
	if err != nil {
		t.Fatalf("ReadBlob 失败: %v", err)
	}
	if len(blobContent) == 0 {
		t.Fatalf("读取的 Blob 为空")
	}
}

// TestSDKTagsAndBranches 测试分支与标签管理
func TestSDKTagsAndBranches(t *testing.T) {
	ctx := context.Background()
	tempDir, err := os.MkdirTemp("", "gogit-sdk-refs-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tempDir)

	r, err := Init(ctx, tempDir, WithDefaultBranch("main"))
	if err != nil {
		t.Fatal(err)
	}

	_ = os.WriteFile(filepath.Join(tempDir, "file.txt"), []byte("content"), 0644)
	_ = r.Add(ctx, "file.txt")
	c1, err := r.CreateCommit(ctx, "init")
	if err != nil {
		t.Fatal(err)
	}

	// 1. 验证 HEAD 和分支
	headRef, err := r.Head(ctx)
	if err != nil {
		t.Fatalf("Head 失败: %v", err)
	}
	if headRef.Hash != c1.Hash {
		t.Fatalf("HEAD 哈希不匹配")
	}

	branches, err := r.Branches(ctx)
	if err != nil {
		t.Fatalf("Branches 失败: %v", err)
	}
	if len(branches) != 1 || branches[0].Name != "refs/heads/main" {
		t.Fatalf("分支列表不符合预期: %+v", branches)
	}

	// 2. 创建轻量标签
	tagLight, err := r.CreateTag(ctx, "v0.1.0")
	if err != nil {
		t.Fatalf("创建轻量标签失败: %v", err)
	}
	if tagLight.Name != "v0.1.0" || tagLight.TargetHash != c1.Hash {
		t.Fatalf("轻量标签不符合预期: %+v", tagLight)
	}

	// 3. 创建附注标签
	tagAnno, err := r.CreateTag(ctx, "v1.0.0", WithTagAnnotated(true), WithTagMessage("release v1.0.0"))
	if err != nil {
		t.Fatalf("创建附注标签失败: %v", err)
	}
	if tagAnno.Name != "v1.0.0" || tagAnno.Message != "release v1.0.0" || tagAnno.TargetHash != c1.Hash {
		t.Fatalf("附注标签不符合预期: %+v", tagAnno)
	}

	// 4. Tags 列表查询
	tags, err := r.Tags(ctx)
	if err != nil {
		t.Fatalf("Tags 列表查询失败: %v", err)
	}
	if len(tags) != 2 {
		t.Fatalf("期望 2 个标签，实际为: %d", len(tags))
	}

	// 5. 单个 Tag 查询
	queriedTag, err := r.Tag(ctx, "v1.0.0")
	if err != nil || queriedTag.Message != "release v1.0.0" {
		t.Fatalf("Tag 单独查询失败: %v", err)
	}
}

// TestSDKSignatureAndVerification 测试通过 SDK 创建数字签名提交并完成验签
func TestSDKSignatureAndVerification(t *testing.T) {
	ctx := context.Background()
	tempDir, err := os.MkdirTemp("", "gogit-sdk-sign-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tempDir)

	r, err := Init(ctx, tempDir, WithDefaultBranch("main"))
	if err != nil {
		t.Fatal(err)
	}

	_ = os.WriteFile(filepath.Join(tempDir, "signed.txt"), []byte("provenance verified"), 0644)
	_ = r.Add(ctx, "signed.txt")

	// 签署提交
	commit, err := r.CreateCommit(ctx, "feat: signed commit via sdk", WithSign(true))
	if err != nil {
		t.Fatalf("创建签名提交失败: %v", err)
	}
	if commit.GPGSig == "" {
		t.Fatalf("提交未附加数字签名")
	}

	// 验证提交签名
	vRes, err := r.VerifyCommit(ctx, commit.Hash)
	if err != nil {
		t.Fatalf("验签失败: %v", err)
	}
	if !vRes.Valid {
		t.Fatalf("签名验证报告应为有效: %+v", vRes)
	}
	if vRes.Type != "ssh" {
		t.Fatalf("期望 SSH 签名类型，实际: %s", vRes.Type)
	}

	// 签署附注标签
	tag, err := r.CreateTag(ctx, "v2.0-signed", WithTagSign(true), WithTagMessage("signed release"))
	if err != nil {
		t.Fatalf("创建签名标签失败: %v", err)
	}
	if !tag.IsSigned {
		t.Fatalf("标签应标明已签名")
	}

	// 验证标签签名
	tagRes, err := r.VerifyTag(ctx, "v2.0-signed")
	if err != nil {
		t.Fatalf("标签验签失败: %v", err)
	}
	if !tagRes.Valid {
		t.Fatalf("标签签名应有效: %+v", tagRes)
	}
}

// TestSDKHTTPServerAndCloneE2E 测试 Smart HTTP Handler 与客户端克隆全流程（纯 Go 内部闭环 + 跨协议互通）
func TestSDKHTTPServerAndCloneE2E(t *testing.T) {
	ctx := context.Background()
	tempDir, err := os.MkdirTemp("", "gogit-sdk-server-e2e-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tempDir)

	serverDir := filepath.Join(tempDir, "server-repo")
	serverRepo, err := Init(ctx, serverDir, WithDefaultBranch("main"))
	if err != nil {
		t.Fatal(err)
	}

	// 服务端写入文件并提交
	fileContent := "deployed via gogit smart http server\n"
	_ = os.WriteFile(filepath.Join(serverDir, "service.go"), []byte(fileContent), 0644)
	_ = serverRepo.Add(ctx, "service.go")
	c1, err := serverRepo.CreateCommit(ctx, "feat: initial service code")
	if err != nil {
		t.Fatal(err)
	}

	// 启动 Smart HTTP 服务
	handler := NewServer(serverRepo, WithServerAllowPush(true))
	ts := httptest.NewServer(handler)
	defer ts.Close()

	// 1. 使用原生 git clone 跨进程验证服务兼容性
	clientNativeDir := filepath.Join(tempDir, "client-native")
	gitCmd := exec.Command("git", "clone", ts.URL, clientNativeDir)
	if out, err := gitCmd.CombinedOutput(); err != nil {
		t.Fatalf("原生 git clone 失败: %v, output: %s", err, string(out))
	}

	nativeContent, err := os.ReadFile(filepath.Join(clientNativeDir, "service.go"))
	if err != nil || string(nativeContent) != fileContent {
		t.Fatalf("原生 git 克隆文件内容不符合预期: %v", err)
	}

	// 2. 使用 SDK Clone 验证纯 Go 客户端互操作性
	clientGoDir := filepath.Join(tempDir, "client-gogit")
	clonedRepo, err := Clone(ctx, ts.URL, clientGoDir)
	if err != nil {
		t.Fatalf("SDK Clone 失败: %v", err)
	}

	clonedCommit, err := clonedRepo.Commit(ctx, "HEAD")
	if err != nil {
		t.Fatalf("克隆仓库读取 HEAD 失败: %v", err)
	}
	if clonedCommit.Hash != c1.Hash {
		t.Fatalf("克隆提交哈希不匹配: %s vs %s", clonedCommit.Hash, c1.Hash)
	}

	goClientContent, err := os.ReadFile(filepath.Join(clientGoDir, "service.go"))
	if err != nil || string(goClientContent) != fileContent {
		t.Fatalf("Go SDK 克隆文件内容不符合预期: %v", err)
	}
}

// TestSDKHTTPServerPushE2E 验证原生 git push 推送代码到纯 Go Smart HTTP 服务端裸仓库
func TestSDKHTTPServerPushE2E(t *testing.T) {
	ctx := context.Background()
	tempDir, err := os.MkdirTemp("", "gogit-sdk-push-e2e-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tempDir)
	tempDir, err = filepath.EvalSymlinks(tempDir)
	if err != nil {
		t.Fatal(err)
	}

	// 1. 初始化服务端的 Bare 仓库
	barePath := filepath.Join(tempDir, "remote.git")
	serverRepo, err := Init(ctx, barePath, WithBare(true), WithDefaultBranch("main"))
	if err != nil {
		t.Fatal(err)
	}

	handler := NewServer(serverRepo, WithServerAllowPush(true))
	ts := httptest.NewServer(handler)
	defer ts.Close()

	// 2. 客户端使用原生 git 初始化并配置 remote
	clientDir := filepath.Join(tempDir, "client")
	if err := os.MkdirAll(clientDir, 0755); err != nil {
		t.Fatal(err)
	}

	cmds := [][]string{
		{"git", "-C", clientDir, "init", "-b", "main"},
		{"git", "-C", clientDir, "config", "user.name", "Pusher"},
		{"git", "-C", clientDir, "config", "user.email", "pusher@example.com"},
	}
	for _, c := range cmds {
		cmd := exec.Command(c[0], c[1:]...)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("配置客户端 git 失败: %v, out: %s", err, string(out))
		}
	}

	// 写入文件并提交（命名为 feature.go 避免受到用户系统级 *.txt 忽略规则影响）
	pushedContent := "pure go smart http server accepted this push!\n"
	_ = os.WriteFile(filepath.Join(clientDir, "feature.go"), []byte(pushedContent), 0644)
	addCmd := exec.Command("git", "-C", clientDir, "add", "feature.go")
	if out, err := addCmd.CombinedOutput(); err != nil {
		t.Fatalf("git add 失败: %v, out: %s", err, string(out))
	}
	commitCmd := exec.Command("git", "-C", clientDir, "commit", "-m", "feat: pushed commit")
	if out, err := commitCmd.CombinedOutput(); err != nil {
		t.Fatalf("原生 git commit 失败: %v, out: %s", err, string(out))
	}

	// 3. 原生 git push 到 Smart HTTP 服务端
	pushCmd := exec.Command("git", "-C", clientDir, "push", ts.URL, "main")
	if out, err := pushCmd.CombinedOutput(); err != nil {
		t.Fatalf("原生 git push 失败: %v, output: %s", err, string(out))
	}

	// 4. 验证裸仓库内部是否已成功接收该提交并移动 main 分支
	mainRef, err := serverRepo.Branch(ctx, "main")
	if err != nil {
		t.Fatalf("服务端获取 main 分支失败: %v", err)
	}
	if mainRef.Hash == "" {
		t.Fatalf("服务端 main 分支哈希为空")
	}

	commit, err := serverRepo.Commit(ctx, mainRef.Hash)
	if err != nil {
		t.Fatalf("服务端无法读取推送生成的提交: %v", err)
	}
	if commit.Message != "feat: pushed commit\n" {
		t.Fatalf("提交信息不符合预期: %q", commit.Message)
	}

	// 验证目录树和文件内容
	tree, err := serverRepo.Tree(ctx, commit.TreeHash)
	if err != nil || len(tree.Entries) == 0 {
		t.Fatalf("服务端 Tree 读取失败")
	}
	blobData, err := serverRepo.ReadBlob(ctx, tree.Entries[0].Hash)
	if err != nil || string(blobData) != pushedContent {
		t.Fatalf("服务端读取推送文件内容失败: %v", err)
	}
}
