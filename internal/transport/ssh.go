package transport

import (
	"bytes"
	"errors"
	"fmt"
	"gogit/internal/object"
	"io"
	"os/exec"
	"strings"
)

// SSHTransport 通过系统 ssh 客户端子进程代理调用远端的 git-upload-pack 与 git-receive-pack
type SSHTransport struct {
	Endpoint *Endpoint
}

func NewSSHTransport(ep *Endpoint) *SSHTransport {
	return &SSHTransport{Endpoint: ep}
}

// DiscoverUploadPack 通过 ssh 启动远端 git-upload-pack 并获取引用与能力
func (st *SSHTransport) DiscoverUploadPack() ([]RemoteRef, string, *RemoteCapabilities, io.WriteCloser, io.ReadCloser, *exec.Cmd, error) {
	cmdArgs := []string{}
	if st.Endpoint.Port != "" {
		cmdArgs = append(cmdArgs, "-p", st.Endpoint.Port)
	}

	target := st.Endpoint.Host
	if st.Endpoint.User != "" {
		target = st.Endpoint.User + "@" + st.Endpoint.Host
	}
	cmdArgs = append(cmdArgs, target, fmt.Sprintf("git-upload-pack '%s'", st.Endpoint.Path))

	cmd := exec.Command("ssh", cmdArgs...)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, "", nil, nil, nil, nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		_ = stdin.Close()
		return nil, "", nil, nil, nil, nil, err
	}

	if err := cmd.Start(); err != nil {
		_ = stdin.Close()
		_ = stdout.Close()
		return nil, "", nil, nil, nil, nil, fmt.Errorf("starting ssh process: %w", err)
	}

	caps := &RemoteCapabilities{Version: 1, Symrefs: make(map[string]string)}
	var refs []RemoteRef
	var headSymref string
	isFirstRef := true

	for {
		payload, pt, err := ReadPacket(stdout)
		if err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			_ = stdin.Close()
			_ = stdout.Close()
			_ = cmd.Wait()
			return nil, "", nil, nil, nil, nil, err
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

	return refs, headSymref, caps, stdin, stdout, cmd, nil
}

// FetchPack 通过既有的 SSH 管道发送 want/have 并拉取 packfile
func (st *SSHTransport) FetchPack(stdin io.WriteCloser, stdout io.ReadCloser, cmd *exec.Cmd, wants []object.Hash, haves []object.Hash) ([]byte, error) {
	defer stdin.Close()
	defer stdout.Close()
	defer cmd.Wait()

	first := true
	for _, w := range wants {
		if first {
			first = false
			_ = WritePacketString(stdin, fmt.Sprintf("want %s multi_ack_detailed side-band-64k ofs-delta agent=git/2.39.0\n", w.String()))
		} else {
			_ = WritePacketString(stdin, fmt.Sprintf("want %s\n", w.String()))
		}
	}
	_ = WriteFlush(stdin)
	for _, h := range haves {
		_ = WritePacketString(stdin, fmt.Sprintf("have %s\n", h.String()))
	}
	_ = WritePacketString(stdin, "done\n")

	var packBuf bytes.Buffer
	err := DemuxSideband(stdout, &packBuf, nil)
	if err != nil && !errors.Is(err, io.EOF) {
		return nil, fmt.Errorf("demuxing sideband: %w", err)
	}

	return packBuf.Bytes(), nil
}

// PushPack 通过 SSH 管道调用 git-receive-pack 执行推送
func (st *SSHTransport) PushPack(updates []RefUpdate, packData []byte) error {
	cmdArgs := []string{}
	if st.Endpoint.Port != "" {
		cmdArgs = append(cmdArgs, "-p", st.Endpoint.Port)
	}

	target := st.Endpoint.Host
	if st.Endpoint.User != "" {
		target = st.Endpoint.User + "@" + st.Endpoint.Host
	}
	cmdArgs = append(cmdArgs, target, fmt.Sprintf("git-receive-pack '%s'", st.Endpoint.Path))

	cmd := exec.Command("ssh", cmdArgs...)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		_ = stdin.Close()
		return err
	}

	if err := cmd.Start(); err != nil {
		_ = stdin.Close()
		_ = stdout.Close()
		return fmt.Errorf("starting ssh process: %w", err)
	}
	defer cmd.Wait()
	defer stdout.Close()

	// 消费远端初始引用广告
	for {
		_, pt, err := ReadPacket(stdout)
		if err != nil || pt == PktFlush {
			break
		}
	}

	// 发送更新指令
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
			_ = WritePacketString(stdin, fmt.Sprintf("%s %s %s\x00 report-status ofs-delta agent=git/2.39.0\n", oldHex, newHex, u.Name))
		} else {
			_ = WritePacketString(stdin, fmt.Sprintf("%s %s %s\n", oldHex, newHex, u.Name))
		}
	}
	_ = WriteFlush(stdin)

	// 写入 packfile
	if len(packData) > 0 {
		_, _ = stdin.Write(packData)
	}
	_ = stdin.Close()

	// 读取 report-status
	for {
		payload, pt, err := ReadPacket(stdout)
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
