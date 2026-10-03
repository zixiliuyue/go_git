package cli

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"gogit/internal/object"
	"gogit/internal/repo"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestCLIInitAndHashObjectSHA256 测试以 SHA-256 格式初始化仓库及 hash-object 计算 64 位哈希
func TestCLIInitAndHashObjectSHA256(t *testing.T) {
	tmpDir := t.TempDir()

	// 1. 测试 gogit init --object-format=sha256
	var stdout, stderr bytes.Buffer
	code := RunWithContext(&Context{
		Args:   []string{"init", "--object-format=sha256", tmpDir},
		Stdout: &stdout,
		Stderr: &stderr,
	})
	if code != ExitSuccess {
		t.Fatalf("init with sha256 failed (exit %d): %s", code, stderr.String())
	}

	// 验证 config 中写入了 extensions.objectFormat = sha256
	r, err := repo.OpenRepository(filepath.Join(tmpDir, ".git"), tmpDir)
	if err != nil {
		t.Fatalf("failed to open created repo: %v", err)
	}
	if r.ObjectFormat != object.FormatSHA256 {
		t.Fatalf("expected repo ObjectFormat to be sha256, got: %s", r.ObjectFormat)
	}

	// 2. 在临时目录下写入测试文件
	testFile := filepath.Join(tmpDir, "test.txt")
	testData := []byte("sha256 crypto agility test\n")
	if err := os.WriteFile(testFile, testData, 0644); err != nil {
		t.Fatalf("failed to write test file: %v", err)
	}

	// 3. 执行 gogit hash-object --object-format=sha256 test.txt
	stdout.Reset()
	stderr.Reset()
	code = RunWithContext(&Context{
		Args:   []string{"hash-object", "--object-format=sha256", testFile},
		Stdout: &stdout,
		Stderr: &stderr,
	})
	if code != ExitSuccess {
		t.Fatalf("hash-object sha256 failed: %s", stderr.String())
	}

	actualHash := strings.TrimSpace(stdout.String())
	if len(actualHash) != 64 {
		t.Fatalf("expected 64 characters SHA-256 hash, got %d (%s)", len(actualHash), actualHash)
	}

	// 验证标准值: "blob 27\x00" + testData 的 sha256
	expectedHeader := append([]byte("blob 27\x00"), testData...)
	expectedSum := sha256.Sum256(expectedHeader)
	expectedHex := hex.EncodeToString(expectedSum[:])
	if actualHash != expectedHex {
		t.Errorf("hash mismatch: got %s, expected %s", actualHash, expectedHex)
	}

	// 4. 测试 Safe-SHA1 拦截恶意 Shattered 输入
	shatteredFile := filepath.Join(tmpDir, "shattered.pdf")
	shatteredData := make([]byte, 400)
	copy(shatteredData, []byte("%PDF-1.3\n"))
	copy(shatteredData[200:], []byte{0x7F, 0x48, 0x07, 0x00})
	_ = os.WriteFile(shatteredFile, shatteredData, 0644)

	stdout.Reset()
	stderr.Reset()
	code = RunWithContext(&Context{
		Args:   []string{"hash-object", "--object-format=sha1", shatteredFile},
		Stdout: &stdout,
		Stderr: &stderr,
	})
	if code != ExitFatal {
		t.Fatalf("expected Safe-SHA1 to abort with ExitFatal, got exit %d", code)
	}
	if !strings.Contains(stderr.String(), "Shattered") && !strings.Contains(stderr.String(), "Safe-SHA1") {
		t.Errorf("expected Shattered collision warning in stderr, got: %s", stderr.String())
	}
}
