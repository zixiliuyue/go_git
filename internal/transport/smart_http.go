package transport

import (
	"bytes"
	"encoding/base64"
	"errors"
	"fmt"
	"gogit/internal/object"
	"io"
	"net/http"
	"strings"
	"time"
)

// HTTPTransport 提供 Git Smart HTTP 传输支持（兼容 Protocol v1 与 v2）
type HTTPTransport struct {
	Endpoint   *Endpoint
	Client     *http.Client
	AuthHeader string
}

func NewHTTPTransport(ep *Endpoint) *HTTPTransport {
	ht := &HTTPTransport{
		Endpoint: ep,
		Client: &http.Client{
			Timeout: 60 * time.Second,
		},
	}
	if ep.User != "" {
		// 基础认证
		auth := base64.StdEncoding.EncodeToString([]byte(ep.User + ":"))
		ht.AuthHeader = "Basic " + auth
	}
	return ht
}

// DiscoverUploadPack 发现远端引用的能力与列表（优先尝试 Git Protocol v2）
func (ht *HTTPTransport) DiscoverUploadPack() ([]RemoteRef, string, *RemoteCapabilities, error) {
	infoURL := fmt.Sprintf("%s/info/refs?service=git-upload-pack", strings.TrimSuffix(ht.Endpoint.Original, "/"))
	req, err := http.NewRequest("GET", infoURL, nil)
	if err != nil {
		return nil, "", nil, err
	}
	req.Header.Set("User-Agent", "git/2.39.0 (gogit)")
	req.Header.Set("Git-Protocol", "version=2")
	if ht.AuthHeader != "" {
		req.Header.Set("Authorization", ht.AuthHeader)
	}

	resp, err := ht.Client.Do(req)
	if err != nil {
		return nil, "", nil, fmt.Errorf("http request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, "", nil, fmt.Errorf("remote server returned HTTP %d", resp.StatusCode)
	}

	// 读取首个数据包以判断是 Protocol v1 还是 v2
	firstPayload, _, err := ReadPacket(resp.Body)
	if err != nil {
		return nil, "", nil, fmt.Errorf("reading first packet: %w", err)
	}

	firstLine := strings.TrimRight(string(firstPayload), "\n\r")

	if firstLine == "version 2" {
		// Protocol v2
		caps := &RemoteCapabilities{Version: 2, Symrefs: make(map[string]string)}
		for {
			payload, pt, err := ReadPacket(resp.Body)
			if err != nil || pt == PktFlush {
				break
			}
			line := strings.TrimSpace(string(payload))
			caps.Raw = append(caps.Raw, line)
			if line == "ls-refs" {
				// 支持 ls-refs
			}
		}

		// 发送 ls-refs 请求拉取全部引用
		refs, headSymref, err := ht.lsRefsV2()
		return refs, headSymref, caps, err
	}

	// Protocol v1
	if !strings.HasPrefix(firstLine, "# service=git-upload-pack") {
		return nil, "", nil, fmt.Errorf("unexpected first line: %s", firstLine)
	}

	// 消费紧随的 0000 刷新包
	_, flushType, err := ReadPacket(resp.Body)
	if err != nil || flushType != PktFlush {
		return nil, "", nil, errors.New("expected flush-pkt after service announcement in v1")
	}

	caps := &RemoteCapabilities{Version: 1, Symrefs: make(map[string]string)}
	var refs []RemoteRef
	var headSymref string
	isFirstRef := true

	for {
		payload, pt, err := ReadPacket(resp.Body)
		if err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			return nil, "", nil, err
		}
		if pt == PktFlush {
			break
		}

		line := string(payload)
		if isFirstRef {
			isFirstRef = false
			// 格式: <oid> <name>\0<capabilities>\n
			nullIdx := strings.Index(line, "\x00")
			if nullIdx != -1 {
				capStr := line[nullIdx+1:]
				line = line[:nullIdx]
				parseCapsV1(caps, capStr)
			}
		}

		line = strings.TrimRight(line, "\n\r")
		parts := strings.SplitN(line, " ", 2)
		if len(parts) == 2 {
			h, err := object.NewHashFromHex(parts[0])
			if err == nil {
				name := parts[1]
				if strings.HasSuffix(name, "^{}") {
					// peeled tag
					tagName := strings.TrimSuffix(name, "^{}")
					for i := range refs {
						if refs[i].Name == tagName {
							refs[i].Peeled = h
							break
						}
					}
				} else {
					refs = append(refs, RemoteRef{
						OID:  h,
						Name: name,
					})
				}
			}
		}
	}

	if headTarget, ok := caps.Symrefs["HEAD"]; ok {
		headSymref = headTarget
		for i := range refs {
			if refs[i].Name == "HEAD" {
				refs[i].Target = headTarget
			}
		}
	}

	return refs, headSymref, caps, nil
}

// lsRefsV2 发送 Git Protocol v2 的 ls-refs 命令
func (ht *HTTPTransport) lsRefsV2() ([]RemoteRef, string, error) {
	serviceURL := fmt.Sprintf("%s/git-upload-pack", strings.TrimSuffix(ht.Endpoint.Original, "/"))

	var body bytes.Buffer
	_ = WritePacketString(&body, "command=ls-refs\n")
	_ = WriteDelim(&body)
	_ = WritePacketString(&body, "peel\n")
	_ = WritePacketString(&body, "symrefs\n")
	_ = WriteFlush(&body)

	req, err := http.NewRequest("POST", serviceURL, &body)
	if err != nil {
		return nil, "", err
	}
	req.Header.Set("User-Agent", "git/2.39.0 (gogit)")
	req.Header.Set("Git-Protocol", "version=2")
	req.Header.Set("Content-Type", "application/x-git-upload-pack-request")
	if ht.AuthHeader != "" {
		req.Header.Set("Authorization", ht.AuthHeader)
	}

	resp, err := ht.Client.Do(req)
	if err != nil {
		return nil, "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, "", fmt.Errorf("remote ls-refs returned HTTP %d", resp.StatusCode)
	}

	var refs []RemoteRef
	var headSymref string

	for {
		payload, pt, err := ReadPacket(resp.Body)
		if err != nil || pt == PktFlush {
			break
		}

		line := strings.TrimRight(string(payload), "\n\r")
		parts := strings.Split(line, " ")
		if len(parts) >= 2 {
			h, err := object.NewHashFromHex(parts[0])
			if err != nil {
				continue
			}
			name := parts[1]
			ref := RemoteRef{OID: h, Name: name}

			// 解析 symref-target 与 peeled 属性
			for _, opt := range parts[2:] {
				if strings.HasPrefix(opt, "symref-target:") {
					ref.Target = strings.TrimPrefix(opt, "symref-target:")
					if name == "HEAD" {
						headSymref = ref.Target
					}
				} else if strings.HasPrefix(opt, "peeled:") {
					if pH, err := object.NewHashFromHex(strings.TrimPrefix(opt, "peeled:")); err == nil {
						ref.Peeled = pH
					}
				}
			}
			refs = append(refs, ref)
		}
	}

	return refs, headSymref, nil
}

// FetchPack 通过 HTTP 协议拉取 packfile
func (ht *HTTPTransport) FetchPack(wants []object.Hash, haves []object.Hash, caps *RemoteCapabilities) ([]byte, error) {
	serviceURL := fmt.Sprintf("%s/git-upload-pack", strings.TrimSuffix(ht.Endpoint.Original, "/"))

	var body bytes.Buffer

	if caps != nil && caps.Version == 2 {
		// Protocol v2 fetch
		_ = WritePacketString(&body, "command=fetch\n")
		_ = WriteDelim(&body)
		_ = WritePacketString(&body, "thin-pack\n")
		_ = WritePacketString(&body, "ofs-delta\n")
		for _, w := range wants {
			_ = WritePacketString(&body, fmt.Sprintf("want %s\n", w.String()))
		}
		for _, h := range haves {
			_ = WritePacketString(&body, fmt.Sprintf("have %s\n", h.String()))
		}
		_ = WritePacketString(&body, "done\n")
		_ = WriteFlush(&body)
	} else {
		// Protocol v1 fetch
		first := true
		for _, w := range wants {
			if first {
				first = false
				_ = WritePacketString(&body, fmt.Sprintf("want %s multi_ack_detailed side-band-64k ofs-delta agent=git/2.39.0\n", w.String()))
			} else {
				_ = WritePacketString(&body, fmt.Sprintf("want %s\n", w.String()))
			}
		}
		_ = WriteFlush(&body)
		for _, h := range haves {
			_ = WritePacketString(&body, fmt.Sprintf("have %s\n", h.String()))
		}
		_ = WritePacketString(&body, "done\n")
	}

	req, err := http.NewRequest("POST", serviceURL, &body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "git/2.39.0 (gogit)")
	if caps != nil && caps.Version == 2 {
		req.Header.Set("Git-Protocol", "version=2")
	}
	req.Header.Set("Content-Type", "application/x-git-upload-pack-request")
	if ht.AuthHeader != "" {
		req.Header.Set("Authorization", ht.AuthHeader)
	}

	resp, err := ht.Client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("fetch-pack returned HTTP %d", resp.StatusCode)
	}

	// 解复用 side-band-64k
	var packBuf bytes.Buffer
	if caps != nil && caps.Version == 2 {
		// 在 v2 中，先读取 section 头部直到 packfile 数据开始
		for {
			payload, pt, err := ReadPacket(resp.Body)
			if err != nil || pt == PktFlush {
				break
			}
			line := strings.TrimSpace(string(payload))
			if line == "packfile" {
				// 进入 packfile sideband 流
				break
			}
		}
	} else {
		// 在 v1 中，服务端在发送 sideband 前会先发送 ACK/NAK 握手包
		for {
			payload, pt, err := ReadPacket(resp.Body)
			if err != nil || pt == PktFlush {
				break
			}
			line := strings.TrimSpace(string(payload))
			if strings.HasPrefix(line, "NAK") || strings.HasPrefix(line, "ACK") {
				break
			}
		}
	}

	err = DemuxSideband(resp.Body, &packBuf, nil)
	if err != nil && !errors.Is(err, io.EOF) {
		return nil, fmt.Errorf("demuxing sideband: %w", err)
	}

	return packBuf.Bytes(), nil
}

// PushPack 通过 HTTP 协议向远端推送提交与引用更新
func (ht *HTTPTransport) PushPack(updates []RefUpdate, packData []byte) error {
	serviceURL := fmt.Sprintf("%s/git-receive-pack", strings.TrimSuffix(ht.Endpoint.Original, "/"))

	var body bytes.Buffer

	// 构建命令 pkt-line
	first := true
	for _, u := range updates {
		oldHex := u.OldOID.String()
		if u.OldOID.IsZero() {
			oldHex = strings.Repeat("0", 40)
		}
		newHex := u.NewOID.String()
		if u.NewOID.IsZero() {
			newHex = strings.Repeat("0", 40)
		}

		if first {
			first = false
			_ = WritePacketString(&body, fmt.Sprintf("%s %s %s\x00 report-status ofs-delta agent=git/2.39.0\n", oldHex, newHex, u.Name))
		} else {
			_ = WritePacketString(&body, fmt.Sprintf("%s %s %s\n", oldHex, newHex, u.Name))
		}
	}
	_ = WriteFlush(&body)

	// 追加 pack 数据
	if len(packData) > 0 {
		body.Write(packData)
	}

	req, err := http.NewRequest("POST", serviceURL, &body)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", "git/2.39.0 (gogit)")
	req.Header.Set("Content-Type", "application/x-git-receive-pack-request")
	if ht.AuthHeader != "" {
		req.Header.Set("Authorization", ht.AuthHeader)
	}

	resp, err := ht.Client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("push returned HTTP %d", resp.StatusCode)
	}

	// 消费 report-status
	for {
		payload, pt, err := ReadPacket(resp.Body)
		if err != nil || pt == PktFlush {
			break
		}
		line := string(payload)
		if strings.HasPrefix(line, "ng ") {
			return fmt.Errorf("remote rejected update: %s", strings.TrimSpace(line))
		}
	}

	return nil
}

func parseCapsV1(caps *RemoteCapabilities, raw string) {
	items := strings.Fields(raw)
	for _, it := range items {
		caps.Raw = append(caps.Raw, it)
		if it == "side-band-64k" {
			caps.Sideband64k = true
		} else if it == "ofs-delta" {
			caps.OfsDelta = true
		} else if strings.HasPrefix(it, "symref=") {
			// symref=HEAD:refs/heads/main
			pair := strings.TrimPrefix(it, "symref=")
			parts := strings.SplitN(pair, ":", 2)
			if len(parts) == 2 {
				caps.Symrefs[parts[0]] = parts[1]
			}
		}
	}
}
