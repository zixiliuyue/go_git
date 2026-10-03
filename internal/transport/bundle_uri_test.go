package transport

import (
	"gogit/internal/history"
	"gogit/internal/index"
	"gogit/internal/object"
	"gogit/internal/repo"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestBundleURICDNDownloadAndApply(t *testing.T) {
	srcDir := t.TempDir()
	rSrc, err := repo.InitRepository(srcDir, false, "main")
	if err != nil {
		t.Fatalf("InitRepository failed: %v", err)
	}

	// 1. 生成初始版本
	_ = os.WriteFile(filepath.Join(srcDir, "cdn_asset.txt"), []byte("Google Piper CDN Fast Fetch"), 0644)
	idx := index.NewIndex()
	data, _ := os.ReadFile(filepath.Join(srcDir, "cdn_asset.txt"))
	h, _ := rSrc.WriteBlob(data)
	fi, _ := os.Stat(filepath.Join(srcDir, "cdn_asset.txt"))
	idx.AddOrReplaceEntry(index.EntryFromOSFileInfo("cdn_asset.txt", fi, h))
	_ = idx.WriteIndex(rSrc.IndexPath)

	tree, _ := idx.WriteTree(rSrc.ObjectsDir)
	sig := object.Signature{Name: "Google SRE", Email: "sre@google.com", When: time.Now()}
	commit := &object.Commit{Tree: tree, Author: sig, Committer: sig, Message: "cdn initial\n"}
	cHash, _ := rSrc.WriteCommit(commit)
	_ = rSrc.Refs.UpdateRef("refs/heads/main", cHash, sig, "cdn initial")
	_ = rSrc.Refs.SetHEADSymbolic("refs/heads/main")

	bundleBytes, _, err := history.CreateBundle(rSrc, []string{"HEAD", "refs/heads/main"}, nil)
	if err != nil {
		t.Fatalf("CreateBundle failed: %v", err)
	}

	// 2. 搭建 Mock HTTP CDN Server
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/snapshot.bundle" {
			w.Header().Set("Content-Type", "application/octet-stream")
			_, _ = w.Write(bundleBytes)
			return
		}
		http.NotFound(w, r)
	}))
	defer ts.Close()

	// 3. 在全新的目标仓库中通过 HTTP CDN 下载并应用 Bundle-URI
	targetDir := t.TempDir()
	rDst, err := repo.InitRepository(targetDir, false, "main")
	if err != nil {
		t.Fatalf("InitRepository dst failed: %v", err)
	}

	cdnBundleURL := ts.URL + "/snapshot.bundle"
	bHeader, err := ApplyBundleURI(rDst, cdnBundleURL)
	if err != nil {
		t.Fatalf("ApplyBundleURI from HTTP CDN failed: %v", err)
	}

	if len(bHeader.References) != 2 {
		t.Errorf("expected 2 references in header, got %d", len(bHeader.References))
	}

	// 验证目标仓库已拥有该提交
	if !rDst.HasObject(cHash) {
		t.Fatalf("destination repo missing commit %s after CDN bundle apply", cHash)
	}

	// 4. 测试 ParseBundleURIResponse
	mockResp := []byte("bundle.version=1\nbundle.mode=all\nbundle.piper.uri=https://cdn.example.com/aosp/repo.bundle\n")
	uris, err := ParseBundleURIResponse(mockResp)
	if err != nil {
		t.Fatalf("ParseBundleURIResponse failed: %v", err)
	}
	if len(uris) != 1 || uris[0] != "https://cdn.example.com/aosp/repo.bundle" {
		t.Errorf("parsed uris mismatch: %+v", uris)
	}
}
