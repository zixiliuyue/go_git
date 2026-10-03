package pack

import (
	"bytes"
	"gogit/internal/object"
	"testing"
)

func TestBundleEncodeDecode(t *testing.T) {
	oid1 := object.MustHashFromHex("1111111111111111111111111111111111111111")
	oid2 := object.MustHashFromHex("2222222222222222222222222222222222222222")
	prereqOID := object.MustHashFromHex("3333333333333333333333333333333333333333")

	header := &BundleHeader{
		Version: 2,
		Prerequisites: []BundleRef{
			{OID: prereqOID, Comment: "prerequisite commit"},
		},
		References: []BundleRef{
			{OID: oid1, Name: "HEAD"},
			{OID: oid2, Name: "refs/heads/main"},
		},
	}

	mockPack := []byte("PACK\x00\x00\x00\x02mock_packfile_data")

	var buf bytes.Buffer
	if err := WriteBundle(&buf, header, mockPack); err != nil {
		t.Fatalf("WriteBundle failed: %v", err)
	}

	decodedHeader, decodedPack, err := ReadBundle(bytes.NewReader(buf.Bytes()))
	if err != nil {
		t.Fatalf("ReadBundle failed: %v", err)
	}

	if decodedHeader.Version != 2 {
		t.Errorf("expected version 2, got %d", decodedHeader.Version)
	}

	if len(decodedHeader.Prerequisites) != 1 || decodedHeader.Prerequisites[0].OID != prereqOID {
		t.Errorf("prerequisites mismatch: %+v", decodedHeader.Prerequisites)
	}

	if len(decodedHeader.References) != 2 {
		t.Fatalf("expected 2 references, got %d", len(decodedHeader.References))
	}

	if decodedHeader.References[0].Name != "HEAD" || decodedHeader.References[0].OID != oid1 {
		t.Errorf("ref 0 mismatch: %+v", decodedHeader.References[0])
	}
	if decodedHeader.References[1].Name != "refs/heads/main" || decodedHeader.References[1].OID != oid2 {
		t.Errorf("ref 1 mismatch: %+v", decodedHeader.References[1])
	}

	if !bytes.Equal(decodedPack, mockPack) {
		t.Errorf("pack data mismatch: got %q, want %q", decodedPack, mockPack)
	}
}
