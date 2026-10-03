package transport

import (
	"bytes"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gogit/internal/object"
	"gogit/internal/pack"
	"gogit/internal/repo"
)

// TestSmartHTTPServerUploadPack 测试 Smart HTTP 服务端 upload-pack 协议全链路（引用发现与打包下载）
func TestSmartHTTPServerUploadPack(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "gogit-http-server-test-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tempDir)

	// 1. 初始化服务端空仓库并写入一个 commit
	r, err := repo.InitRepository(tempDir, false, "main")
	if err != nil {
		t.Fatal(err)
	}

	blobContent := []byte("hello smart http server\n")
	blobHash, err := object.WriteLooseObjectToDir(r.ObjectsDir, object.TypeBlob, blobContent)
	if err != nil {
		t.Fatal(err)
	}

	tree := &object.Tree{
		Entries: []object.TreeEntry{
			{Mode: object.ModeRegular, Name: "file.txt", OID: blobHash},
		},
	}
	treeHash, err := object.WriteLooseObjectToDir(r.ObjectsDir, object.TypeTree, tree.Payload())
	if err != nil {
		t.Fatal(err)
	}

	author := r.AuthorSignature()
	commit := &object.Commit{
		Tree:      treeHash,
		Author:    author,
		Committer: author,
		Message:   "initial commit\n",
	}
	commitHash, err := object.WriteLooseObjectToDir(r.ObjectsDir, object.TypeCommit, commit.Payload())
	if err != nil {
		t.Fatal(err)
	}

	if err := r.Refs.UpdateRef("refs/heads/main", commitHash, author, "init commit"); err != nil {
		t.Fatal(err)
	}
	if err := r.Refs.SetHEADSymbolic("refs/heads/main"); err != nil {
		t.Fatal(err)
	}

	// 2. 启动 Smart HTTP 服务
	handler := NewSmartHTTPServer(r)
	ts := httptest.NewServer(handler)
	defer ts.Close()

	// 3. 测试 GET /info/refs?service=git-upload-pack
	resp, err := http.Get(ts.URL + "/info/refs?service=git-upload-pack")
	if err != nil {
		t.Fatalf("请求 info/refs 失败: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("期望状态码 200，实际: %d", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); ct != "application/x-git-upload-pack-advertisement" {
		t.Fatalf("Content-Type 不匹配: %s", ct)
	}

	// 验证包含 main 分支与 HEAD
	p1, _, err := ReadPacket(resp.Body)
	if err != nil || string(p1) != "# service=git-upload-pack\n" {
		t.Fatalf("无效的第一包: %s, err: %v", string(p1), err)
	}
	_, pType, _ := ReadPacket(resp.Body)
	if pType != PktFlush {
		t.Fatalf("期望 flush 包")
	}

	pHEAD, _, _ := ReadPacket(resp.Body)
	if !strings.Contains(string(pHEAD), "HEAD") || !strings.Contains(string(pHEAD), commitHash.String()) {
		t.Fatalf("HEAD 广告包不匹配: %s", string(pHEAD))
	}

	pMain, _, _ := ReadPacket(resp.Body)
	if !strings.Contains(string(pMain), "refs/heads/main") {
		t.Fatalf("refs/heads/main 广告包不匹配: %s", string(pMain))
	}

	// 4. 测试 POST /git-upload-pack 拉取 pack 数据
	var uploadReq bytes.Buffer
	_ = WritePacketString(&uploadReq, fmt.Sprintf("want %s side-band-64k\n", commitHash.String()))
	_ = WriteFlush(&uploadReq)
	_ = WritePacketString(&uploadReq, "done\n")

	uploadResp, err := http.Post(ts.URL+"/git-upload-pack", "application/x-git-upload-pack-request", &uploadReq)
	if err != nil {
		t.Fatalf("POST git-upload-pack 失败: %v", err)
	}
	defer uploadResp.Body.Close()

	if uploadResp.StatusCode != http.StatusOK {
		t.Fatalf("upload-pack 状态码不匹配: %d", uploadResp.StatusCode)
	}

	// 读取 NAK
	pNAK, _, _ := ReadPacket(uploadResp.Body)
	if string(pNAK) != "NAK\n" {
		t.Fatalf("期望 NAK 包，实际: %s", string(pNAK))
	}

	// 读取 sideband packfile
	var packData bytes.Buffer
	for {
		pkt, pt, err := ReadPacket(uploadResp.Body)
		if err != nil || pt == PktFlush {
			break
		}
		if len(pkt) > 0 && pkt[0] == byte(SidebandData) {
			packData.Write(pkt[1:])
		}
	}

	if packData.Len() == 0 {
		t.Fatalf("未接收到 pack 数据")
	}

	resolved, _, err := pack.ReadPack(bytes.NewReader(packData.Bytes()))
	if err != nil {
		t.Fatalf("解析服务端返回的 pack 失败: %v", err)
	}

	if len(resolved) != 3 { // commit, tree, blob
		t.Fatalf("期望 3 个对象，实际解析到 %d", len(resolved))
	}
}

// TestSmartHTTPServerReceivePack 测试 Smart HTTP 服务端 receive-pack 客户端推送处理
func TestSmartHTTPServerReceivePack(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "gogit-http-receive-test-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tempDir)

	// 初始化一个裸仓库作为服务端
	r, err := repo.InitRepository(filepath.Join(tempDir, "remote.git"), true, "main")
	if err != nil {
		t.Fatal(err)
	}

	handler := NewSmartHTTPServer(r)
	ts := httptest.NewServer(handler)
	defer ts.Close()

	// 1. GET /info/refs?service=git-receive-pack
	resp, err := http.Get(ts.URL + "/info/refs?service=git-receive-pack")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("期望状态码 200，实际: %d", resp.StatusCode)
	}

	// 2. 构造客户端要推送的 Packfile
	pBlobData := []byte("content pushed via pure go smart http\n")
	pBlobOID := object.HashObject(object.TypeBlob, pBlobData)
	pTree := &object.Tree{
		Entries: []object.TreeEntry{
			{Mode: object.ModeRegular, Name: "pushed.txt", OID: pBlobOID},
		},
	}
	pTreeOID := object.HashObject(object.TypeTree, pTree.Payload())
	sig := r.AuthorSignature()
	pCommit := &object.Commit{
		Tree:      pTreeOID,
		Author:    sig,
		Committer: sig,
		Message:   "feat: pushed via smart http\n",
	}
	pCommitOID := object.HashObject(object.TypeCommit, pCommit.Payload())

	packBytes, _, _, err := pack.BuildPack([]pack.PackableObject{
		{OID: pCommitOID, Type: object.TypeCommit, Content: pCommit.Payload()},
		{OID: pTreeOID, Type: object.TypeTree, Content: pTree.Payload()},
		{OID: pBlobOID, Type: object.TypeBlob, Content: pBlobData},
	})
	if err != nil {
		t.Fatal(err)
	}

	// 3. 发送 POST /git-receive-pack
	var pushReq bytes.Buffer
	cmdLine := fmt.Sprintf("%s %s refs/heads/main\x00report-status\n", object.ZeroHash.String(), pCommitOID.String())
	_ = WritePacketString(&pushReq, cmdLine)
	_ = WriteFlush(&pushReq)
	pushReq.Write(packBytes)

	pushResp, err := http.Post(ts.URL+"/git-receive-pack", "application/x-git-receive-pack-request", &pushReq)
	if err != nil {
		t.Fatalf("POST git-receive-pack 失败: %v", err)
	}
	defer pushResp.Body.Close()

	if pushResp.StatusCode != http.StatusOK {
		t.Fatalf("状态码不符合预期: %d", pushResp.StatusCode)
	}

	// 4. 验证 report-status
	unpackPkt, _, _ := ReadPacket(pushResp.Body)
	if string(unpackPkt) != "unpack ok\n" {
		t.Fatalf("期望 'unpack ok\\n'，实际: %q", string(unpackPkt))
	}
	refPkt, _, _ := ReadPacket(pushResp.Body)
	if string(refPkt) != "ok refs/heads/main\n" {
		t.Fatalf("期望 'ok refs/heads/main\\n'，实际: %q", string(refPkt))
	}

	// 5. 验证裸仓库内部是否已成功持久化对象与引用
	refOID, err := r.Refs.ResolveRef("refs/heads/main")
	if err != nil {
		t.Fatalf("获取服务端 refs/heads/main 失败: %v", err)
	}
	if refOID != pCommitOID {
		t.Fatalf("服务端引用哈希不匹配: 期望 %s，实际 %s", pCommitOID.String(), refOID.String())
	}

	// 验证对象可读取
	obj, err := r.ReadObject(pCommitOID)
	if err != nil {
		t.Fatalf("服务端无法读取推送的 commit: %v", err)
	}
	if obj.Type() != object.TypeCommit {
		t.Fatalf("对象类型不匹配")
	}
}
