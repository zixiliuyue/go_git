package pack

import (
	"bytes"
	"gogit/internal/object"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestPackBuildAndRead(t *testing.T) {
	blob1 := []byte("hello world first blob")
	h1 := object.HashObject(object.TypeBlob, blob1)

	blob2 := []byte("hello world second blob with some similar content")
	h2 := object.HashObject(object.TypeBlob, blob2)

	objs := []PackableObject{
		{OID: h1, Type: object.TypeBlob, Content: blob1},
		{OID: h2, Type: object.TypeBlob, Content: blob2},
	}

	packData, idxData, packChecksum, err := BuildPack(objs)
	if err != nil {
		t.Fatalf("BuildPack 失败: %v", err)
	}

	// 1. 本地 ReadPack 解析验证
	reader := bytes.NewReader(packData)
	resolved, checksum, err := ReadPack(reader)
	if err != nil {
		t.Fatalf("ReadPack 失败: %v", err)
	}

	if checksum != packChecksum {
		t.Fatalf("校验和不匹配: %s != %s", checksum.String(), packChecksum.String())
	}

	if len(resolved) != 2 {
		t.Fatalf("解析对象数量不正确: %d", len(resolved))
	}

	foundMap := make(map[object.Hash][]byte)
	for _, ro := range resolved {
		foundMap[ro.OID] = ro.Content
	}

	if !bytes.Equal(foundMap[h1], blob1) {
		t.Fatalf("blob1 数据还原不一致")
	}
	if !bytes.Equal(foundMap[h2], blob2) {
		t.Fatalf("blob2 数据还原不一致")
	}

	// 2. 原生 git verify-pack 互操作验证
	if gitPath, err := exec.LookPath("git"); err == nil && gitPath != "" {
		tmpDir, err := os.MkdirTemp("", "gogit-pack-test-*")
		if err != nil {
			t.Fatal(err)
		}
		defer os.RemoveAll(tmpDir)

		packPath := filepath.Join(tmpDir, "test.pack")
		idxPath := filepath.Join(tmpDir, "test.idx")

		if err := os.WriteFile(packPath, packData, 0644); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(idxPath, idxData, 0644); err != nil {
			t.Fatal(err)
		}

		cmd := exec.Command("git", "verify-pack", "-v", idxPath)
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git verify-pack 验证失败: %v, 输出: %s", err, string(out))
		}
		t.Logf("git verify-pack 输出:\n%s", string(out))
	}
}

func TestPackWithDelta(t *testing.T) {
	// 构造具有高度重叠内容的大文件以触发 Delta 压缩
	baseText := bytes.Repeat([]byte("Alpha Beta Gamma Delta Epsilon Zeta Eta Theta Iota Kappa Lambda Mu\n"), 20)
	targetText := append(baseText, []byte("MODIFIED LINE AT THE END OF DELTA TEST\n")...)

	hBase := object.HashObject(object.TypeBlob, baseText)
	hTarget := object.HashObject(object.TypeBlob, targetText)

	objs := []PackableObject{
		{OID: hBase, Type: object.TypeBlob, Content: baseText},
		{OID: hTarget, Type: object.TypeBlob, Content: targetText},
	}

	packData, idxData, packChecksum, err := BuildPack(objs)
	if err != nil {
		t.Fatalf("BuildPack 失败: %v", err)
	}

	// 1. 本地 ReadPack
	resolved, checksum, err := ReadPack(bytes.NewReader(packData))
	if err != nil {
		t.Fatalf("ReadPack 失败: %v", err)
	}
	if checksum != packChecksum {
		t.Fatalf("checksum mismatch")
	}

	foundMap := make(map[object.Hash][]byte)
	for _, ro := range resolved {
		foundMap[ro.OID] = ro.Content
	}
	if !bytes.Equal(foundMap[hBase], baseText) || !bytes.Equal(foundMap[hTarget], targetText) {
		t.Fatalf("Delta 对象解压还原失败")
	}

	// 2. 原生 git verify-pack 验证
	if gitPath, err := exec.LookPath("git"); err == nil && gitPath != "" {
		tmpDir, err := os.MkdirTemp("", "gogit-delta-pack-*")
		if err != nil {
			t.Fatal(err)
		}
		defer os.RemoveAll(tmpDir)

		packPath := filepath.Join(tmpDir, "delta.pack")
		idxPath := filepath.Join(tmpDir, "delta.idx")

		_ = os.WriteFile(packPath, packData, 0644)
		_ = os.WriteFile(idxPath, idxData, 0644)

		cmd := exec.Command("git", "verify-pack", "-v", idxPath)
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git verify-pack 验证失败: %v, 输出: %s", err, string(out))
		}
		t.Logf("git verify-pack 输出:\n%s", string(out))
	}
}

