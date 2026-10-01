package transport

import (
	"bytes"
	"gogit/internal/object"
	"gogit/internal/pack"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestSmartHTTPProtocolV2(t *testing.T) {
	// 构造测试 packfile
	blobData := []byte("smart http v2 test data")
	blobOID := object.HashObject(object.TypeBlob, blobData)
	packData, _, _, err := pack.BuildPack([]pack.PackableObject{
		{OID: blobOID, Type: object.TypeBlob, Content: blobData},
	})
	if err != nil {
		t.Fatal(err)
	}

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		proto := r.Header.Get("Git-Protocol")
		if r.URL.Path == "/testrepo/info/refs" && r.URL.Query().Get("service") == "git-upload-pack" {
			if proto == "version=2" {
				w.Header().Set("Content-Type", "application/x-git-upload-pack-advertisement")
				_ = WritePacketString(w, "version 2\n")
				_ = WritePacketString(w, "ls-refs\n")
				_ = WritePacketString(w, "fetch\n")
				_ = WriteFlush(w)
				return
			}
		}

		if r.URL.Path == "/testrepo/git-upload-pack" {
			bodyBytes := new(bytes.Buffer)
			_, _ = bodyBytes.ReadFrom(r.Body)
			bodyStr := bodyBytes.String()

			if strings.Contains(bodyStr, "command=ls-refs") {
				w.Header().Set("Content-Type", "application/x-git-upload-pack-result")
				_ = WritePacketString(w, blobOID.String()+" HEAD symref-target:refs/heads/main\n")
				_ = WritePacketString(w, blobOID.String()+" refs/heads/main\n")
				_ = WriteFlush(w)
				return
			}

			if strings.Contains(bodyStr, "command=fetch") {
				w.Header().Set("Content-Type", "application/x-git-upload-pack-result")
				_ = WritePacketString(w, "packfile\n")

				// sideband-64k 写入 packfile 数据
				sw := NewSidebandWriter(w, SidebandData)
				_, _ = sw.Write(packData)
				_ = WriteFlush(w)
				return
			}
		}

		http.NotFound(w, r)
	}))
	defer ts.Close()

	ep, err := ParseEndpoint(ts.URL + "/testrepo")
	if err != nil {
		t.Fatal(err)
	}

	ht := NewHTTPTransport(ep)
	refs, headSymref, caps, err := ht.DiscoverUploadPack()
	if err != nil {
		t.Fatalf("DiscoverUploadPack 失败: %v", err)
	}

	if caps.Version != 2 {
		t.Fatalf("期望 Protocol v2，实际得到 %d", caps.Version)
	}

	if headSymref != "refs/heads/main" {
		t.Fatalf("headSymref 期望 refs/heads/main，实际得到 %s", headSymref)
	}

	if len(refs) != 2 {
		t.Fatalf("发现引用数量不匹配: %v", refs)
	}

	fetchedPack, err := ht.FetchPack([]object.Hash{blobOID}, nil, caps)
	if err != nil {
		t.Fatalf("FetchPack 失败: %v", err)
	}

	resolved, _, err := pack.ReadPack(bytes.NewReader(fetchedPack))
	if err != nil {
		t.Fatalf("解析拉取的 pack 失败: %v", err)
	}

	if len(resolved) != 1 || !bytes.Equal(resolved[0].Content, blobData) {
		t.Fatalf("拉取对象内容不匹配")
	}
}

func TestSmartHTTPProtocolV1(t *testing.T) {
	blobData := []byte("smart http v1 test data")
	blobOID := object.HashObject(object.TypeBlob, blobData)
	packData, _, _, err := pack.BuildPack([]pack.PackableObject{
		{OID: blobOID, Type: object.TypeBlob, Content: blobData},
	})
	if err != nil {
		t.Fatal(err)
	}

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/testrepo/info/refs" && r.URL.Query().Get("service") == "git-upload-pack" {
			w.Header().Set("Content-Type", "application/x-git-upload-pack-advertisement")
			_ = WritePacketString(w, "# service=git-upload-pack\n")
			_ = WriteFlush(w)
			firstRef := blobOID.String() + " HEAD\x00side-band-64k ofs-delta symref=HEAD:refs/heads/master\n"
			_ = WritePacketString(w, firstRef)
			_ = WritePacketString(w, blobOID.String()+" refs/heads/master\n")
			_ = WriteFlush(w)
			return
		}

		if r.URL.Path == "/testrepo/git-upload-pack" {
			w.Header().Set("Content-Type", "application/x-git-upload-pack-result")
			_ = WritePacketString(w, "NAK\n")

			sw := NewSidebandWriter(w, SidebandData)
			_, _ = sw.Write(packData)
			_ = WriteFlush(w)
			return
		}

		http.NotFound(w, r)
	}))
	defer ts.Close()

	ep, err := ParseEndpoint(ts.URL + "/testrepo")
	if err != nil {
		t.Fatal(err)
	}

	ht := NewHTTPTransport(ep)
	_, headSymref, caps, err := ht.DiscoverUploadPack()
	if err != nil {
		t.Fatalf("DiscoverUploadPack v1 失败: %v", err)
	}

	if caps.Version != 1 {
		t.Fatalf("期望 Protocol v1，实际得到 %d", caps.Version)
	}

	if headSymref != "refs/heads/master" {
		t.Fatalf("headSymref 期望 refs/heads/master，实际得到 %s", headSymref)
	}

	fetchedPack, err := ht.FetchPack([]object.Hash{blobOID}, nil, caps)
	if err != nil {
		t.Fatalf("FetchPack 失败: %v", err)
	}

	resolved, _, err := pack.ReadPack(bytes.NewReader(fetchedPack))
	if err != nil {
		t.Fatalf("解析拉取的 pack 失败: %v", err)
	}

	if len(resolved) != 1 || !bytes.Equal(resolved[0].Content, blobData) {
		t.Fatalf("拉取对象内容不匹配")
	}
}
