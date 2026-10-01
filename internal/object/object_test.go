package object

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestHashBlobVsGitOracle 测试 gogit 计算的 blob SHA-1 与系统 git hash-object 严格一致
func TestHashBlobVsGitOracle(t *testing.T) {
	testCases := [][]byte{
		[]byte(""),
		[]byte("hello world\n"),
		[]byte("multi-line\ncontent\nwith utf-8: 你好世界\n"),
		bytes.Repeat([]byte("abcdef0123456789"), 1000), // 16KB
	}

	for idx, content := range testCases {
		gogitHash := HashObject(TypeBlob, content)

		cmd := exec.Command("git", "hash-object", "--stdin")
		cmd.Stdin = bytes.NewReader(content)
		out, err := cmd.Output()
		if err != nil {
			t.Fatalf("用例 %d 执行 git hash-object 失败: %v", idx, err)
		}
		gitHash := strings.TrimSpace(string(out))

		if gogitHash.String() != gitHash {
			t.Fatalf("用例 %d 哈希不一致! gogit=%s, git=%s", idx, gogitHash.String(), gitHash)
		}
	}
}

// TestTreeSerializationVsGit 测试 Tree 序列化与 Git mktree 完全一致
func TestTreeSerializationVsGit(t *testing.T) {
	// 构造测试条目：包含文件和目录，重点测试 Git 目录排序特性（'/' 比较）
	// 注意：在 Git 中，若 a 是目录，比较时作为 "a/"；a-b 作为 "a-b"。因为 '/' (0x2F) > '-' (0x2D)，
	// 所以 "a-b" 排在 "a/" 之前！
	dummyHash1 := MustHashFromHex("e69de29bb2d1d6434b8b29ae775ad8c2e48c5391")
	dummyHash2 := MustHashFromHex("257cc5642cb1a054f08cc83f2d943e56fd3ebe99")
	dummyHash3 := MustHashFromHex("ba3b95a329d7247343e5069273c52a0614995f5c")

	tree := &Tree{
		Entries: []TreeEntry{
			{Mode: ModeRegular, Name: "a-b", OID: dummyHash1},
			{Mode: ModeDirectory, Name: "a", OID: dummyHash2},
			{Mode: ModeExec, Name: "main.go", OID: dummyHash3},
		},
	}

	gogitPayload := tree.Payload()
	gogitHash := tree.Hash()

	// 用 git mktree 验证生成的 tree
	// git mktree 接收格式: "<mode> <type> <sha>\t<name>\n"
	var mktreeInput bytes.Buffer
	mktreeInput.WriteString("100644 blob e69de29bb2d1d6434b8b29ae775ad8c2e48c5391\ta-b\n")
	mktreeInput.WriteString("040000 tree 257cc5642cb1a054f08cc83f2d943e56fd3ebe99\ta\n")
	mktreeInput.WriteString("100755 blob ba3b95a329d7247343e5069273c52a0614995f5c\tmain.go\n")

	cmd := exec.Command("git", "mktree", "--missing")
	cmd.Stdin = &mktreeInput
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("执行 git mktree 失败: %v", err)
	}
	gitHash := strings.TrimSpace(string(out))

	if gogitHash.String() != gitHash {
		t.Fatalf("Tree 哈希与 git mktree 不一致! gogit=%s, git=%s", gogitHash.String(), gitHash)
	}

	// 反向解析测试
	parsedTree, err := ParseTree(gogitPayload)
	if err != nil {
		t.Fatalf("ParseTree 失败: %v", err)
	}
	if len(parsedTree.Entries) != 3 {
		t.Fatalf("解析得到的条目数不符: %d", len(parsedTree.Entries))
	}
	if parsedTree.Hash() != gogitHash {
		t.Fatalf("解析重构后 Tree 哈希不一致")
	}
}

// TestLooseObjectWriteAndReadVsGitFsck 测试 loose 写入后真实 git 能读取并通过 fsck 严格检查
func TestLooseObjectWriteAndReadVsGitFsck(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "gogit_test_obj_*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tempDir)

	// 初始化一个空 git 仓库用于校验
	cmd := exec.Command("git", "init", tempDir)
	if err := cmd.Run(); err != nil {
		t.Fatalf("git init 失败: %v", err)
	}

	objectsDir := filepath.Join(tempDir, ".git", "objects")

	// 1. 写入 blob
	blobContent := []byte("hello from gogit loose storage!\n")
	blobHash, err := WriteLooseObjectToDir(objectsDir, TypeBlob, blobContent)
	if err != nil {
		t.Fatalf("WriteLooseObjectToDir blob 失败: %v", err)
	}

	// 验证读取
	loosePath := filepath.Join(objectsDir, blobHash.String()[:2], blobHash.String()[2:])
	readObj, err := ReadLooseObject(loosePath)
	if err != nil {
		t.Fatalf("ReadLooseObject 失败: %v", err)
	}
	if !bytes.Equal(readObj.Content, blobContent) {
		t.Fatalf("读出的 blob 内容不一致")
	}

	// 2. 写入 Tree
	tree := &Tree{
		Entries: []TreeEntry{
			{Mode: ModeRegular, Name: "greeting.txt", OID: blobHash},
		},
	}
	treeHash, err := WriteLooseObjectToDir(objectsDir, TypeTree, tree.Payload())
	if err != nil {
		t.Fatalf("WriteLooseObjectToDir tree 失败: %v", err)
	}

	// 3. 写入 Commit
	commit := &Commit{
		Tree: treeHash,
		Author: Signature{
			Name:  "Gogit Tester",
			Email: "tester@gogit.local",
			When:  time.Unix(1700000000, 0),
			TZ:    "+0800",
		},
		Committer: Signature{
			Name:  "Gogit Tester",
			Email: "tester@gogit.local",
			When:  time.Unix(1700000000, 0),
			TZ:    "+0800",
		},
		Message: "initial commit from gogit\n",
	}
	commitHash, err := WriteLooseObjectToDir(objectsDir, TypeCommit, commit.Payload())
	if err != nil {
		t.Fatalf("WriteLooseObjectToDir commit 失败: %v", err)
	}

	// 4. 用原生 git 运行 fsck 校验
	fsckCmd := exec.Command("git", "--git-dir="+filepath.Join(tempDir, ".git"), "fsck", "--strict")
	fsckOut, err := fsckCmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git fsck --strict 校验失败: %v\n输出: %s", err, string(fsckOut))
	}

	// 5. 用原生 git cat-file 读取验证
	catCmd := exec.Command("git", "--git-dir="+filepath.Join(tempDir, ".git"), "cat-file", "-p", commitHash.String())
	catOut, err := catCmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git cat-file 失败: %v\n输出: %s", err, string(catOut))
	}

	if !strings.Contains(string(catOut), "initial commit from gogit") {
		t.Fatalf("git cat-file 输出未包含预期提交信息: %s", string(catOut))
	}
}
