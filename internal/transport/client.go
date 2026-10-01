package transport

import (
	"fmt"
	"gogit/internal/object"
)

// TransportClient 统一的远端传输客户端接口
type TransportClient interface {
	Discover() ([]RemoteRef, string, error)
	Fetch(wants []object.Hash, haves []object.Hash) ([]byte, error)
	Push(updates []RefUpdate, packData []byte) error
}

type unifiedClient struct {
	ep        *Endpoint
	httpTr    *HTTPTransport
	localTr   *LocalTransport
	sshTr     *SSHTransport
	daemonTr  *DaemonTransport
	httpCaps  *RemoteCapabilities
}

// NewClient 根据给定的 URL 或路径自动解析协议类型并返回相应的 TransportClient
func NewClient(rawURL string) (TransportClient, error) {
	ep, err := ParseEndpoint(rawURL)
	if err != nil {
		return nil, err
	}

	uc := &unifiedClient{ep: ep}

	switch ep.Protocol {
	case ProtoFile:
		uc.localTr = NewLocalTransport(ep)
	case ProtoHTTP, ProtoHTTPS:
		uc.httpTr = NewHTTPTransport(ep)
	case ProtoSSH:
		uc.sshTr = NewSSHTransport(ep)
	case ProtoGit:
		uc.daemonTr = NewDaemonTransport(ep)
	default:
		return nil, fmt.Errorf("unsupported protocol %s", ep.Protocol)
	}

	return uc, nil
}

func (c *unifiedClient) Discover() ([]RemoteRef, string, error) {
	if c.localTr != nil {
		return c.localTr.DiscoverReferences()
	}
	if c.httpTr != nil {
		refs, headSymref, caps, err := c.httpTr.DiscoverUploadPack()
		c.httpCaps = caps
		return refs, headSymref, err
	}
	if c.sshTr != nil {
		refs, headSymref, _, stdin, stdout, cmd, err := c.sshTr.DiscoverUploadPack()
		if stdin != nil {
			_ = stdin.Close()
		}
		if stdout != nil {
			_ = stdout.Close()
		}
		if cmd != nil {
			_ = cmd.Wait()
		}
		return refs, headSymref, err
	}
	if c.daemonTr != nil {
		refs, headSymref, _, conn, err := c.daemonTr.DiscoverUploadPack()
		if conn != nil {
			_ = conn.Close()
		}
		return refs, headSymref, err
	}
	return nil, "", fmt.Errorf("no transport driver available")
}

func (c *unifiedClient) Fetch(wants []object.Hash, haves []object.Hash) ([]byte, error) {
	if c.localTr != nil {
		return c.localTr.FetchPack(wants, haves)
	}
	if c.httpTr != nil {
		return c.httpTr.FetchPack(wants, haves, c.httpCaps)
	}
	if c.sshTr != nil {
		_, _, _, stdin, stdout, cmd, err := c.sshTr.DiscoverUploadPack()
		if err != nil {
			return nil, err
		}
		return c.sshTr.FetchPack(stdin, stdout, cmd, wants, haves)
	}
	if c.daemonTr != nil {
		_, _, _, conn, err := c.daemonTr.DiscoverUploadPack()
		if err != nil {
			return nil, err
		}
		return c.daemonTr.FetchPack(conn, wants, haves)
	}
	return nil, fmt.Errorf("no transport driver available")
}

func (c *unifiedClient) Push(updates []RefUpdate, packData []byte) error {
	if c.localTr != nil {
		return c.localTr.PushPack(updates, packData)
	}
	if c.httpTr != nil {
		return c.httpTr.PushPack(updates, packData)
	}
	if c.sshTr != nil {
		return c.sshTr.PushPack(updates, packData)
	}
	return fmt.Errorf("push not supported for protocol %s", c.ep.Protocol)
}
