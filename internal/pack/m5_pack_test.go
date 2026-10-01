package pack

import (
	"bytes"
	"fmt"
	"gogit/internal/object"
	"os"
	"path/filepath"
	"testing"
)

func TestVerifyPackAndFormatVerbose(t *testing.T) {
	dir := t.TempDir()

	// 构造测试对象
	commitObj := PackableObject{
		OID:     object.HashObject(object.TypeCommit, []byte("tree 4b825dc642cb6eb9a060e54bf8d69288fbee4904\n\ntest commit")),
		Type:    object.TypeCommit,
		Content: []byte("tree 4b825dc642cb6eb9a060e54bf8d69288fbee4904\n\ntest commit"),
	}
	blobObj := PackableObject{
		OID:     object.HashObject(object.TypeBlob, []byte("hello world pack test")),
		Type:    object.TypeBlob,
		Content: []byte("hello world pack test"),
	}

	packBytes, idxBytes, chk, err := BuildPack([]PackableObject{commitObj, blobObj})
	if err != nil {
		t.Fatalf("BuildPack 失败: %v", err)
	}

	packPath := filepath.Join(dir, fmt.Sprintf("pack-%s.pack", chk.String()))
	idxPath := filepath.Join(dir, fmt.Sprintf("pack-%s.idx", chk.String()))
	if err := os.WriteFile(packPath, packBytes, 0644); err != nil {
		t.Fatalf("写入 pack 失败: %v", err)
	}
	if err := os.WriteFile(idxPath, idxBytes, 0644); err != nil {
		t.Fatalf("写入 idx 失败: %v", err)
	}

	// 验证 packfile
	res, err := VerifyPackFile(packPath, true)
	if err != nil {
		t.Fatalf("VerifyPackFile 失败: %v", err)
	}

	if res.TotalObjects != 2 {
		t.Fatalf("预期 2 个对象，实际 %d", res.TotalObjects)
	}

	var buf bytes.Buffer
	FormatVerboseStat(&buf, res)
	outputStr := buf.String()
	if len(outputStr) == 0 {
		t.Fatal("FormatVerboseStat 输出为空")
	}

	// 验证 idx 路径直接校验
	resIdx, err := VerifyPackFile(idxPath, false)
	if err != nil {
		t.Fatalf("VerifyPackFile (传入 idx 路径) 失败: %v", err)
	}
	if resIdx.TotalObjects != 2 {
		t.Fatalf("预期 2 个对象，实际 %d", resIdx.TotalObjects)
	}
}

func TestMIDXWriteAndVerify(t *testing.T) {
	dir := t.TempDir()

	// 构建一个 packfile
	blob1 := PackableObject{
		OID:     object.HashObject(object.TypeBlob, []byte("content 1")),
		Type:    object.TypeBlob,
		Content: []byte("content 1"),
	}
	blob2 := PackableObject{
		OID:     object.HashObject(object.TypeBlob, []byte("content 2")),
		Type:    object.TypeBlob,
		Content: []byte("content 2"),
	}

	packBytes, idxBytes, chk, err := BuildPack([]PackableObject{blob1, blob2})
	if err != nil {
		t.Fatalf("BuildPack 失败: %v", err)
	}

	packPath := filepath.Join(dir, fmt.Sprintf("pack-%s.pack", chk.String()))
	idxPath := filepath.Join(dir, fmt.Sprintf("pack-%s.idx", chk.String()))
	_ = os.WriteFile(packPath, packBytes, 0644)
	_ = os.WriteFile(idxPath, idxBytes, 0644)

	// 生成 multi-pack-index
	midxPath, err := WriteMIDX(dir)
	if err != nil {
		t.Fatalf("WriteMIDX 失败: %v", err)
	}

	if _, err := os.Stat(midxPath); err != nil {
		t.Fatalf("MIDX 文件不存在: %v", err)
	}

	// 校验 multi-pack-index
	if err := VerifyMIDX(midxPath); err != nil {
		t.Fatalf("VerifyMIDX 失败: %v", err)
	}
}

func TestPackBitmapWriteAndVerify(t *testing.T) {
	dir := t.TempDir()

	commitObj := PackableObject{
		OID:     object.HashObject(object.TypeCommit, []byte("commit payload")),
		Type:    object.TypeCommit,
		Content: []byte("commit payload"),
	}
	treeObj := PackableObject{
		OID:     object.HashObject(object.TypeTree, []byte{}),
		Type:    object.TypeTree,
		Content: []byte{},
	}
	blobObj := PackableObject{
		OID:     object.HashObject(object.TypeBlob, []byte("bitmap blob")),
		Type:    object.TypeBlob,
		Content: []byte("bitmap blob"),
	}

	packBytes, idxBytes, chk, err := BuildPack([]PackableObject{commitObj, treeObj, blobObj})
	if err != nil {
		t.Fatalf("BuildPack 失败: %v", err)
	}

	packPath := filepath.Join(dir, fmt.Sprintf("pack-%s.pack", chk.String()))
	idxPath := filepath.Join(dir, fmt.Sprintf("pack-%s.idx", chk.String()))
	_ = os.WriteFile(packPath, packBytes, 0644)
	_ = os.WriteFile(idxPath, idxBytes, 0644)

	bitmapPath, err := WritePackBitmap(packPath, idxPath, func(h object.Hash) (object.ObjectType, error) {
		if h == commitObj.OID {
			return object.TypeCommit, nil
		}
		if h == treeObj.OID {
			return object.TypeTree, nil
		}
		return object.TypeBlob, nil
	})
	if err != nil {
		t.Fatalf("WritePackBitmap 失败: %v", err)
	}

	if _, err := os.Stat(bitmapPath); err != nil {
		t.Fatalf("Bitmap 文件未生成: %s", bitmapPath)
	}

	// 校验 bitmap 文件格式与魔数
	if err := VerifyPackBitmap(filepath.Clean(bitmapPath)); err != nil {
		t.Fatalf("VerifyPackBitmap 校验失败: %v", err)
	}
}
