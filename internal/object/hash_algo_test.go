package object

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"testing"
)

// TestHashAlgorithmAgility 测试 SHA-1 与 SHA-256 算法计算及格式识别
func TestHashAlgorithmAgility(t *testing.T) {
	content := []byte("hello world")

	// 1. SHA-1 测试
	sha1Hex, err := HashObjectHex(FormatSHA1, TypeBlob, content)
	if err != nil {
		t.Fatalf("HashObjectHex SHA1 failed: %v", err)
	}
	if len(sha1Hex) != 40 {
		t.Fatalf("expected 40 chars for SHA1 hex, got %d", len(sha1Hex))
	}
	// 与原生 HashObject 对比
	expectedSHA1 := HashObject(TypeBlob, content).String()
	if sha1Hex != expectedSHA1 {
		t.Errorf("mismatch: got %s, expected %s", sha1Hex, expectedSHA1)
	}

	// 2. SHA-256 测试
	sha256Hex, err := HashObjectHex(FormatSHA256, TypeBlob, content)
	if err != nil {
		t.Fatalf("HashObjectHex SHA256 failed: %v", err)
	}
	if len(sha256Hex) != 64 {
		t.Fatalf("expected 64 chars for SHA256 hex, got %d", len(sha256Hex))
	}

	// 验证 SHA-256 计算符合 Git blob 标准: "blob 11\x00hello world"
	fullPayload := append([]byte("blob 11\x00"), content...)
	expectedSum := sha256.Sum256(fullPayload)
	if sha256Hex != hex.EncodeToString(expectedSum[:]) {
		t.Errorf("sha256 mismatch: got %s, expected %s", sha256Hex, hex.EncodeToString(expectedSum[:]))
	}

	// 3. 自动识别格式
	fmt1, err := DetectFormatFromHex(sha1Hex)
	if err != nil || fmt1 != FormatSHA1 {
		t.Errorf("expected FormatSHA1, got %v (%v)", fmt1, err)
	}
	fmt2, err := DetectFormatFromHex(sha256Hex)
	if err != nil || fmt2 != FormatSHA256 {
		t.Errorf("expected FormatSHA256, got %v (%v)", fmt2, err)
	}
}

// TestSafeSHA1CollisionDetection 测试 Safe-SHA1 成功拦截 Shattered 碰撞特征
func TestSafeSHA1CollisionDetection(t *testing.T) {
	// 构造模拟 Shattered 恶意 PDF 头部特征
	maliciousPayload := make([]byte, 400)
	copy(maliciousPayload, []byte("%PDF-1.3\n"))
	// 植入 Shattered 攻击碰撞块关键特征差分
	copy(maliciousPayload[200:], []byte{0x7F, 0x48, 0x07, 0x00})

	_, err := HashObjectHex(FormatSHA1, TypeBlob, maliciousPayload)
	if err == nil {
		t.Fatalf("expected error detecting Shattered collision, but got nil")
	}
	if !errors.Is(err, ErrSHA1CollisionDetected) {
		t.Errorf("expected ErrSHA1CollisionDetected, got: %v", err)
	}

	// 同样的恶意输入在 SHA-256 下不会受 SHA-1 碰撞特征影响，能够正常计算安全哈希
	sha256Hex, err := HashObjectHex(FormatSHA256, TypeBlob, maliciousPayload)
	if err != nil {
		t.Fatalf("SHA-256 should safely compute hash even for malicious SHA-1 patterns: %v", err)
	}
	if len(sha256Hex) != 64 {
		t.Errorf("expected 64 chars for SHA-256, got %d", len(sha256Hex))
	}
}
