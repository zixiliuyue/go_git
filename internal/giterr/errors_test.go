package giterr

import (
	"context"
	"errors"
	"fmt"
	"gogit/internal/object"
	"testing"
)

func TestGitErrorsMatching(t *testing.T) {
	// 1. 验证 ObjectNotFoundError 匹配 ErrObjectNotFound
	oid := object.MustHashFromHex("1111111111111111111111111111111111111111")
	var err error = &ObjectNotFoundError{OID: oid}
	if !errors.Is(err, ErrObjectNotFound) {
		t.Errorf("expected err to match ErrObjectNotFound via errors.Is")
	}

	// 2. 验证包装错误 (fmt.Errorf %w) 依然能匹配
	wrapped := fmt.Errorf("read failed: %w", err)
	if !errors.Is(wrapped, ErrObjectNotFound) {
		t.Errorf("expected wrapped err to match ErrObjectNotFound")
	}

	// 3. 验证 MergeConflictError
	conflictErr := &MergeConflictError{Path: "main.go", Reason: "both modified"}
	if !errors.Is(conflictErr, ErrMergeConflict) {
		t.Errorf("expected conflictErr to match ErrMergeConflict")
	}

	// 4. 验证 CorruptedIndexError
	indexErr := &CorruptedIndexError{Path: ".git/index", Reason: "checksum mismatch"}
	if !errors.Is(indexErr, ErrCorruptedIndex) {
		t.Errorf("expected indexErr to match ErrCorruptedIndex")
	}
}

func TestExitCodeMapping(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		expected int
	}{
		{"nil error", nil, 0},
		{"context canceled", context.Canceled, 130},
		{"interrupted error", ErrInterrupted, 130},
		{"merge conflict", &MergeConflictError{Path: "foo.txt"}, 1},
		{"wrapped merge conflict", fmt.Errorf("pre-merge: %w", ErrMergeConflict), 1},
		{"object not found", &ObjectNotFoundError{OID: object.ZeroHash}, 128},
		{"unknown generic error", errors.New("io timeout"), 128},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			code := ExitCodeForError(tc.err)
			if code != tc.expected {
				t.Errorf("for error %v: expected exit code %d, got %d", tc.err, tc.expected, code)
			}
		})
	}
}
