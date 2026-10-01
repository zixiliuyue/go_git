package history

import (
	"crypto/sha1"
	"encoding/hex"
	"gogit/internal/repo"
	"os"
	"path/filepath"
)

// RerereRecordConflict 记录冲突前镜像 (preimage)
func RerereRecordConflict(r *repo.Repository, conflictPath string, conflictContent []byte) (string, error) {
	h := sha1.Sum(conflictContent)
	conflictID := hex.EncodeToString(h[:])

	cacheDir := filepath.Join(r.GitDir, "rr-cache", conflictID)
	if err := os.MkdirAll(cacheDir, 0755); err != nil {
		return "", err
	}

	preimagePath := filepath.Join(cacheDir, "preimage")
	if err := os.WriteFile(preimagePath, conflictContent, 0644); err != nil {
		return "", err
	}

	return conflictID, nil
}

// RerereRecordResolution 记录解决后的冲突结果 (postimage)
func RerereRecordResolution(r *repo.Repository, conflictID string, resolvedContent []byte) error {
	cacheDir := filepath.Join(r.GitDir, "rr-cache", conflictID)
	if _, err := os.Stat(cacheDir); os.IsNotExist(err) {
		return nil
	}

	postimagePath := filepath.Join(cacheDir, "postimage")
	return os.WriteFile(postimagePath, resolvedContent, 0644)
}

// RerereAutoResolve 尝试依据历史 postimage 自动重放解决冲突
func RerereAutoResolve(r *repo.Repository, conflictContent []byte) ([]byte, bool) {
	h := sha1.Sum(conflictContent)
	conflictID := hex.EncodeToString(h[:])

	postimagePath := filepath.Join(r.GitDir, "rr-cache", conflictID, "postimage")
	data, err := os.ReadFile(postimagePath)
	if err != nil {
		return nil, false
	}

	return data, true
}
