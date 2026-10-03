package transport

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"gogit/internal/history"
	"gogit/internal/pack"
	"gogit/internal/repo"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

// DownloadBundle 根据给定的 URI (支持 http/https/file 或本地路径) 获取 Bundle 二进制数据
func DownloadBundle(uri string) ([]byte, error) {
	return DownloadBundleWithContext(context.Background(), uri)
}

// DownloadBundleWithContext 携带 context.Context 下载 Bundle 二进制数据，支持用户信号随时中断
func DownloadBundleWithContext(ctx context.Context, uri string) ([]byte, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	parsed, err := url.Parse(uri)
	if err == nil && (parsed.Scheme == "http" || parsed.Scheme == "https") {
		// HTTP(S) CDN 静态下载
		client := &http.Client{
			Timeout: 60 * time.Second,
		}
		req, err := http.NewRequestWithContext(ctx, "GET", uri, nil)
		if err != nil {
			return nil, fmt.Errorf("构建 bundle 请求失败: %w", err)
		}
		resp, err := client.Do(req)
		if err != nil {
			return nil, fmt.Errorf("CDN 下载 bundle 失败 (%s): %w", uri, err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("CDN 下载 bundle 返回非 200 状态码: %s", resp.Status)
		}

		return io.ReadAll(resp.Body)
	}

	// 本地路径或 file://
	filePath := uri
	if strings.HasPrefix(filePath, "file://") {
		filePath = strings.TrimPrefix(filePath, "file://")
	}

	return os.ReadFile(filePath)
}

// ParseBundleURIResponse 解析 Git Protocol v2 中 command=bundle-uri 的响应报文
func ParseBundleURIResponse(data []byte) ([]string, error) {
	var uris []string
	scanner := bufio.NewScanner(bytes.NewReader(data))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		// 格式如 bundle.<id>.uri <uri> 或 key=val
		if strings.Contains(line, ".uri=") {
			parts := strings.SplitN(line, ".uri=", 2)
			if len(parts) == 2 && parts[1] != "" {
				uris = append(uris, parts[1])
			}
		} else if strings.Contains(line, ".uri ") {
			parts := strings.SplitN(line, ".uri ", 2)
			if len(parts) == 2 && parts[1] != "" {
				uris = append(uris, parts[1])
			}
		}
	}
	return uris, nil
}

// ApplyBundleURI 下载指定的 Bundle URI 并将其解包写入目标仓库
func ApplyBundleURI(r *repo.Repository, bundleURI string) (*pack.BundleHeader, error) {
	bundleData, err := DownloadBundle(bundleURI)
	if err != nil {
		return nil, fmt.Errorf("获取 bundle-uri 资源失败: %w", err)
	}

	header, packData, err := pack.ReadBundle(bytes.NewReader(bundleData))
	if err != nil {
		return nil, fmt.Errorf("解析 bundle 数据格式失败: %w", err)
	}

	if err := history.Unbundle(r, header, packData); err != nil {
		return nil, fmt.Errorf("解包 bundle 失败: %w", err)
	}

	return header, nil
}
