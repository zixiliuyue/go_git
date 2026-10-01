package transport

import (
	"bytes"
	"errors"
	"fmt"
	"gogit/internal/object"
	"io"
	"net"
	"strings"
	"time"
)

// DaemonTransport 实现通过原生 git:// 守护进程协议进行通信传输
type DaemonTransport struct {
	Endpoint *Endpoint
}

func NewDaemonTransport(ep *Endpoint) *DaemonTransport {
	return &DaemonTransport{Endpoint: ep}
}

// DiscoverUploadPack 连接 git 守护进程并读取初始引用与能力
func (dt *DaemonTransport) DiscoverUploadPack() ([]RemoteRef, string, *RemoteCapabilities, net.Conn, error) {
	port := dt.Endpoint.Port
	if port == "" {
		port = "9418"
	}
	addr := net.JoinHostPort(dt.Endpoint.Host, port)

	conn, err := net.DialTimeout("tcp", addr, 10*time.Second)
	if err != nil {
		return nil, "", nil, nil, fmt.Errorf("connecting to git daemon at %s: %w", addr, err)
	}

	// 1. 发送初始请求包：git-upload-pack /path\0host=...\0
	reqStr := fmt.Sprintf("git-upload-pack %s\x00host=%s\x00", dt.Endpoint.Path, dt.Endpoint.Host)
	if err := WritePacketString(conn, reqStr); err != nil {
		_ = conn.Close()
		return nil, "", nil, nil, err
	}

	caps := &RemoteCapabilities{Version: 1, Symrefs: make(map[string]string)}
	var refs []RemoteRef
	var headSymref string
	isFirstRef := true

	for {
		payload, pt, err := ReadPacket(conn)
		if err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			_ = conn.Close()
			return nil, "", nil, nil, err
		}
		if pt == PktFlush {
			break
		}

		line := string(payload)
		if isFirstRef {
			isFirstRef = false
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

	return refs, headSymref, caps, conn, nil
}

// FetchPack 通过既有的 TCP 套接字发送 want/have 并拉取 packfile
func (dt *DaemonTransport) FetchPack(conn net.Conn, wants []object.Hash, haves []object.Hash) ([]byte, error) {
	defer conn.Close()

	first := true
	for _, w := range wants {
		if first {
			first = false
			_ = WritePacketString(conn, fmt.Sprintf("want %s multi_ack_detailed side-band-64k ofs-delta agent=git/2.39.0\n", w.String()))
		} else {
			_ = WritePacketString(conn, fmt.Sprintf("want %s\n", w.String()))
		}
	}
	_ = WriteFlush(conn)
	for _, h := range haves {
		_ = WritePacketString(conn, fmt.Sprintf("have %s\n", h.String()))
	}
	_ = WritePacketString(conn, "done\n")

	var packBuf bytes.Buffer
	err := DemuxSideband(conn, &packBuf, nil)
	if err != nil && !errors.Is(err, io.EOF) {
		return nil, fmt.Errorf("demuxing sideband: %w", err)
	}

	return packBuf.Bytes(), nil
}
