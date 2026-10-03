package signature

import (
	"errors"
	"strings"
	"testing"
)

// TestSSHSignatureCreationAndVerification 测试使用原生 Ed25519 签署载荷并验证签名有效性及防篡改能力
func TestSSHSignatureCreationAndVerification(t *testing.T) {
	signer, err := NewSSHSigner("test@google.com")
	if err != nil {
		t.Fatalf("failed to create SSH signer: %v", err)
	}

	payload := []byte("tree 4b825dc642cb6eb9a060e54bf8d69288fbee4904\nauthor Developer <dev@example.com> 1700000000 +0000\ncommitter Developer <dev@example.com> 1700000000 +0000\n\nInitial commit\n")

	// 1. 签名
	sigArmor, err := signer.Sign(payload)
	if err != nil {
		t.Fatalf("signer.Sign failed: %v", err)
	}
	if !strings.Contains(sigArmor, "-----BEGIN SSH SIGNATURE-----") {
		t.Fatalf("invalid signature output: %s", sigArmor)
	}

	// 2. 验证有效签名
	res, err := VerifySignature(payload, sigArmor)
	if err != nil {
		t.Fatalf("VerifySignature failed on valid signature: %v", err)
	}
	if !res.Valid || res.Type != SigTypeSSH {
		t.Fatalf("expected valid SSH signature, got: %+v", res)
	}

	// 3. 验证防篡改能力（修改载荷中的一个字节）
	tamperedPayload := []byte("tree 4b825dc642cb6eb9a060e54bf8d69288fbee4904\nauthor Hacker <hacker@example.com> 1700000000 +0000\ncommitter Hacker <hacker@example.com> 1700000000 +0000\n\nInitial commit\n")
	_, err = VerifySignature(tamperedPayload, sigArmor)
	if err == nil {
		t.Fatalf("expected signature verification failure for tampered payload, but succeeded")
	}
	if !errors.Is(err, ErrSignatureVerificationFailed) {
		t.Errorf("expected ErrSignatureVerificationFailed, got: %v", err)
	}
}

// TestExtractCommitAndTagSignature 测试从 Commit 与 Tag 原始流中正确剥离载荷与签名
func TestExtractCommitAndTagSignature(t *testing.T) {
	rawCommit := []byte(`tree 4b825dc642cb6eb9a060e54bf8d69288fbee4904
author Alice <alice@example.com> 1700000000 +0000
committer Alice <alice@example.com> 1700000000 +0000
gpgsig -----BEGIN SSH SIGNATURE-----
 U1NIU0lHAAAAAQAAADMAAAAba2V5AAA...
 -----END SSH SIGNATURE-----

Feature commit message
`)

	payload, sig, sigType, err := ExtractCommitSignature(rawCommit)
	if err != nil {
		t.Fatalf("ExtractCommitSignature failed: %v", err)
	}
	if sigType != SigTypeSSH {
		t.Errorf("expected SigTypeSSH, got: %v", sigType)
	}
	if !strings.Contains(sig, "-----BEGIN SSH SIGNATURE-----") || !strings.Contains(sig, "U1NIU0lHAAAAAQAAADMAAAAba2V5AAA...") {
		t.Errorf("extracted signature corrupted: %s", sig)
	}
	// 载荷中不应包含 gpgsig
	if strings.Contains(string(payload), "gpgsig") {
		t.Errorf("payload should not contain gpgsig: %s", string(payload))
	}
	if !strings.Contains(string(payload), "Feature commit message") {
		t.Errorf("payload should contain commit message: %s", string(payload))
	}

	// 测试 Tag 签名抽取
	rawTag := []byte(`object 4b825dc642cb6eb9a060e54bf8d69288fbee4904
type commit
tag v1.0.0
tagger Bob <bob@example.com> 1700000000 +0000

Release v1.0.0
-----BEGIN PGP SIGNATURE-----
Version: GnuPG v2

iQEzBAABCAAdFiEE...
=ABCD
-----END PGP SIGNATURE-----`)

	tagPayload, tagSig, tagType, err := ExtractTagSignature(rawTag)
	if err != nil {
		t.Fatalf("ExtractTagSignature failed: %v", err)
	}
	if tagType != SigTypePGP {
		t.Errorf("expected SigTypePGP, got: %v", tagType)
	}
	if !strings.Contains(tagSig, "-----BEGIN PGP SIGNATURE-----") {
		t.Errorf("tag signature corrupted: %s", tagSig)
	}
	if strings.Contains(string(tagPayload), "-----BEGIN PGP SIGNATURE-----") {
		t.Errorf("tag payload should not contain signature block: %s", string(tagPayload))
	}
}
