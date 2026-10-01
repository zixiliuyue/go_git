package transport

import (
	"bytes"
	"gogit/internal/object"
	"gogit/internal/repo"
	"os"
	"path/filepath"
	"testing"
)

func TestPktLineRoundTrip(t *testing.T) {
	var buf bytes.Buffer

	payload1 := []byte("hello pktline\n")
	if err := WritePacket(&buf, payload1); err != nil {
		t.Fatalf("WritePacket 失败: %v", err)
	}

	if err := WriteFlush(&buf); err != nil {
		t.Fatalf("WriteFlush 失败: %v", err)
	}

	if err := WriteDelim(&buf); err != nil {
		t.Fatalf("WriteDelim 失败: %v", err)
	}

	// 1. 读取首包
	p1, t1, err := ReadPacket(&buf)
	if err != nil || t1 != PktData || !bytes.Equal(p1, payload1) {
		t.Fatalf("读取首包失败: %v, %v, %s", err, t1, string(p1))
	}

	// 2. 读取 Flush
	_, t2, err := ReadPacket(&buf)
	if err != nil || t2 != PktFlush {
		t.Fatalf("读取 Flush 失败: %v, %v", err, t2)
	}

	// 3. 读取 Delim
	_, t3, err := ReadPacket(&buf)
	if err != nil || t3 != PktDelim {
		t.Fatalf("读取 Delim 失败: %v, %v", err, t3)
	}
}

func TestSidebandDemux(t *testing.T) {
	var stream bytes.Buffer

	// 写入 channel 1 (data)
	packBytes := []byte("dummy pack data")
	sw1 := NewSidebandWriter(&stream, SidebandData)
	_, _ = sw1.Write(packBytes)

	// 写入 channel 2 (progress)
	progBytes := []byte("Counting objects: 100%\n")
	sw2 := NewSidebandWriter(&stream, SidebandProgress)
	_, _ = sw2.Write(progBytes)

	_ = WriteFlush(&stream)

	var packOut, progOut bytes.Buffer
	if err := DemuxSideband(&stream, &packOut, &progOut); err != nil {
		t.Fatalf("DemuxSideband 失败: %v", err)
	}

	if !bytes.Equal(packOut.Bytes(), packBytes) {
		t.Fatalf("Sideband channel 1 数据不匹配: %s != %s", packOut.String(), string(packBytes))
	}
	if !bytes.Equal(progOut.Bytes(), progBytes) {
		t.Fatalf("Sideband channel 2 数据不匹配: %s != %s", progOut.String(), string(progBytes))
	}
}

func TestEndpointParsing(t *testing.T) {
	cases := []struct {
		url      string
		protocol ProtocolType
		host     string
		path     string
	}{
		{"https://github.com/user/repo.git", ProtoHTTPS, "github.com", "/user/repo.git"},
		{"http://example.com:8080/repo", ProtoHTTP, "example.com", "/repo"},
		{"git://git.example.com/repo.git", ProtoGit, "git.example.com", "/repo.git"},
		{"ssh://git@github.com:22/user/repo.git", ProtoSSH, "github.com", "/user/repo.git"},
		{"git@github.com:user/repo.git", ProtoSSH, "github.com", "user/repo.git"},
		{"/var/git/repo.git", ProtoFile, "", "/var/git/repo.git"},
	}

	for _, c := range cases {
		ep, err := ParseEndpoint(c.url)
		if err != nil {
			t.Fatalf("解析 %s 失败: %v", c.url, err)
		}
		if ep.Protocol != c.protocol {
			t.Errorf("URL %s 协议不匹配: 期望 %s, 实际 %s", c.url, c.protocol, ep.Protocol)
		}
		if c.host != "" && ep.Host != c.host {
			t.Errorf("URL %s host 不匹配: 期望 %s, 实际 %s", c.url, c.host, ep.Host)
		}
	}
}

func TestLocalTransportFetch(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "gogit-transport-test-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	srcRepoPath := filepath.Join(tmpDir, "source")
	r, err := repo.InitRepository(srcRepoPath, false, "master")
	if err != nil {
		t.Fatal(err)
	}

	// 写入一个 blob 和 commit
	blobOID, err := r.WriteBlob([]byte("test content for transport"))
	if err != nil {
		t.Fatal(err)
	}

	tree := &object.Tree{
		Entries: []object.TreeEntry{
			{Mode: object.ModeRegular, Name: "file.txt", OID: blobOID},
		},
	}
	treeOID, err := r.WriteTree(tree)
	if err != nil {
		t.Fatal(err)
	}

	sig := r.AuthorSignature()
	commit := &object.Commit{
		Tree:      treeOID,
		Author:    sig,
		Committer: sig,
		Message:   "commit for transport\n",
	}
	commitOID, err := r.WriteCommit(commit)
	if err != nil {
		t.Fatal(err)
	}

	_ = r.Refs.UpdateRef("refs/heads/master", commitOID, sig, "init commit")
	_ = r.Refs.SetHEADSymbolic("refs/heads/master")

	// 使用 Client 发现与拉取
	client, err := NewClient(srcRepoPath)
	if err != nil {
		t.Fatal(err)
	}

	refs, headSymref, err := client.Discover()
	if err != nil {
		t.Fatalf("Discover 失败: %v", err)
	}

	if headSymref != "refs/heads/master" {
		t.Errorf("HEAD symref 不匹配: %s", headSymref)
	}

	foundMaster := false
	for _, rf := range refs {
		if rf.Name == "refs/heads/master" && rf.OID == commitOID {
			foundMaster = true
			break
		}
	}
	if !foundMaster {
		t.Fatalf("未能发现 refs/heads/master")
	}

	packData, err := client.Fetch([]object.Hash{commitOID}, nil)
	if err != nil {
		t.Fatalf("Fetch 失败: %v", err)
	}

	if len(packData) == 0 {
		t.Fatalf("返回的 pack 数据为空")
	}
}
