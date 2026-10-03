package transport

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"gogit/internal/object"
	"gogit/internal/pack"
	"gogit/internal/repo"
)

// CollectObjects 收集从 wants 可达、但在 haves 集合中不存在的全部对象列表
func CollectObjects(r *repo.Repository, wants, haves []object.Hash) ([]pack.PackableObject, error) {
	haveSet := make(map[object.Hash]bool, len(haves))
	for _, h := range haves {
		haveSet[h] = true
	}

	visited := make(map[object.Hash]bool)
	var packables []pack.PackableObject

	var collectTree func(treeOID object.Hash) error
	collectTree = func(treeOID object.Hash) error {
		if visited[treeOID] || haveSet[treeOID] {
			return nil
		}
		visited[treeOID] = true

		raw, err := r.ReadObject(treeOID)
		if err != nil {
			return err
		}
		packables = append(packables, pack.PackableObject{
			OID:     treeOID,
			Type:    object.TypeTree,
			Content: raw.Content,
		})

		treeObj, err := object.ParseTree(raw.Content)
		if err != nil {
			return err
		}

		for _, e := range treeObj.Entries {
			if visited[e.OID] || haveSet[e.OID] {
				continue
			}
			if e.Mode == object.ModeDirectory {
				if err := collectTree(e.OID); err != nil {
					return err
				}
			} else {
				visited[e.OID] = true
				blobRaw, err := r.ReadObject(e.OID)
				if err != nil {
					return err
				}
				packables = append(packables, pack.PackableObject{
					OID:     e.OID,
					Type:    object.TypeBlob,
					Content: blobRaw.Content,
				})
			}
		}
		return nil
	}

	// 广度优先遍历 commit DAG 拓扑
	queue := make([]object.Hash, 0, len(wants))
	for _, w := range wants {
		if !haveSet[w] && !visited[w] {
			queue = append(queue, w)
		}
	}

	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]

		if visited[cur] || haveSet[cur] {
			continue
		}
		visited[cur] = true

		raw, err := r.ReadObject(cur)
		if err != nil {
			return nil, err
		}

		packables = append(packables, pack.PackableObject{
			OID:     cur,
			Type:    raw.Type(),
			Content: raw.Content,
		})

		switch raw.Type() {
		case object.TypeCommit:
			commitObj, err := object.ParseCommit(raw.Content)
			if err != nil {
				return nil, err
			}
			if err := collectTree(commitObj.Tree); err != nil {
				return nil, err
			}
			for _, p := range commitObj.Parents {
				if !haveSet[p] && !visited[p] {
					queue = append(queue, p)
				}
			}
		case object.TypeTree:
			if err := collectTree(cur); err != nil {
				return nil, err
			}
		case object.TypeTag:
			tagObj, err := object.ParseTag(raw.Content)
			if err != nil {
				return nil, err
			}
			if !haveSet[tagObj.Object] && !visited[tagObj.Object] {
				queue = append(queue, tagObj.Object)
			}
		}
	}

	return packables, nil
}

// SmartHTTPServer 提供标准兼容的 Git Smart HTTP 服务端 Handler（对齐 git-http-backend 规范）
type SmartHTTPServer struct {
	Repo      *repo.Repository
	AllowPush bool
}

// NewSmartHTTPServer 创建针对指定 Git 仓库的 Smart HTTP 服务端
func NewSmartHTTPServer(r *repo.Repository) *SmartHTTPServer {
	return &SmartHTTPServer{
		Repo:      r,
		AllowPush: true,
	}
}

// ServeHTTP 实现 http.Handler 接口，分发 Git 智能传输请求
func (s *SmartHTTPServer) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	path := req.URL.Path

	switch {
	case strings.HasSuffix(path, "/info/refs"):
		service := req.URL.Query().Get("service")
		switch service {
		case "git-upload-pack":
			s.handleInfoRefsUpload(w, req)
		case "git-receive-pack":
			s.handleInfoRefsReceive(w, req)
		default:
			http.Error(w, "invalid git service query parameter", http.StatusBadRequest)
		}
	case strings.HasSuffix(path, "/git-upload-pack"):
		s.handleUploadPack(w, req)
	case strings.HasSuffix(path, "/git-receive-pack"):
		s.handleReceivePack(w, req)
	default:
		http.NotFound(w, req)
	}
}

// handleInfoRefsUpload 处理 GET /info/refs?service=git-upload-pack 引用发现
func (s *SmartHTTPServer) handleInfoRefsUpload(w http.ResponseWriter, req *http.Request) {
	w.Header().Set("Content-Type", "application/x-git-upload-pack-advertisement")
	w.Header().Set("Cache-Control", "no-cache, no-store, max-age=0, must-revalidate")
	w.Header().Set("Pragma", "no-cache")

	proto := req.Header.Get("Git-Protocol")
	if strings.Contains(proto, "version=2") {
		_ = WritePacketString(w, "version 2\n")
		_ = WritePacketString(w, "ls-refs\n")
		_ = WritePacketString(w, "fetch=shallow\n")
		_ = WriteFlush(w)
		return
	}

	_ = WritePacketString(w, "# service=git-upload-pack\n")
	_ = WriteFlush(w)

	allRefs, _ := s.Repo.Refs.ListRefs("")
	headOID, _ := s.Repo.Refs.ResolveHEAD()
	headRef, _ := s.Repo.Refs.ReadHEAD()

	headSymref := ""
	if headRef != nil && headRef.IsSymref {
		headSymref = headRef.Target
	}

	if headOID.IsZero() && len(allRefs) == 0 {
		_ = WritePacketString(w, object.ZeroHash.String()+" capabilities^{}\x00multi_ack side-band-64k ofs-delta\n")
		_ = WriteFlush(w)
		return
	}

	caps := "multi_ack side-band-64k ofs-delta"
	if headSymref != "" {
		caps += " symref=HEAD:" + headSymref
	}

	if !headOID.IsZero() {
		_ = WritePacketString(w, fmt.Sprintf("%s HEAD\x00%s\n", headOID.String(), caps))
	}

	for refName, refOID := range allRefs {
		if refName == "HEAD" {
			continue
		}
		_ = WritePacketString(w, fmt.Sprintf("%s %s\n", refOID.String(), refName))

		// 附注标签剥离 Commit 标识
		if raw, err := s.Repo.ReadObject(refOID); err == nil && raw.Type() == object.TypeTag {
			if tagObj, err := object.ParseTag(raw.Content); err == nil {
				_ = WritePacketString(w, fmt.Sprintf("%s %s^{}\n", tagObj.Object.String(), refName))
			}
		}
	}
	_ = WriteFlush(w)
}

// handleUploadPack 处理 POST /git-upload-pack 拉取请求
func (s *SmartHTTPServer) handleUploadPack(w http.ResponseWriter, req *http.Request) {
	w.Header().Set("Content-Type", "application/x-git-upload-pack-result")
	w.Header().Set("Cache-Control", "no-cache, no-store, max-age=0, must-revalidate")
	w.Header().Set("Pragma", "no-cache")

	bodyBytes, err := io.ReadAll(req.Body)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	proto := req.Header.Get("Git-Protocol")
	if strings.Contains(proto, "version=2") || strings.Contains(string(bodyBytes), "command=ls-refs") || strings.Contains(string(bodyBytes), "command=fetch") {
		s.handleUploadPackV2(w, bodyBytes)
		return
	}

	s.handleUploadPackV1(w, bodyBytes)
}

func (s *SmartHTTPServer) handleUploadPackV1(w http.ResponseWriter, bodyBytes []byte) {
	r := bytes.NewReader(bodyBytes)
	var wants []object.Hash
	var haves []object.Hash
	useSideband := false

	for {
		payload, pType, err := ReadPacket(r)
		if err != nil || pType == PktFlush {
			if pType == PktFlush && len(wants) > 0 {
				continue
			}
			break
		}
		line := strings.TrimSpace(string(payload))
		if strings.HasPrefix(line, "want ") {
			parts := strings.Split(line, " ")
			if len(parts) >= 2 {
				oid, err := object.NewHashFromHex(parts[1])
				if err == nil {
					wants = append(wants, oid)
				}
			}
			if strings.Contains(line, "side-band-64k") || strings.Contains(line, "side-band") {
				useSideband = true
			}
		} else if strings.HasPrefix(line, "have ") {
			parts := strings.Split(line, " ")
			if len(parts) >= 2 {
				oid, err := object.NewHashFromHex(parts[1])
				if err == nil {
					haves = append(haves, oid)
				}
			}
		} else if line == "done" {
			break
		}
	}

	if len(wants) == 0 {
		return
	}

	_ = WritePacketString(w, "NAK\n")

	objs, err := CollectObjects(s.Repo, wants, haves)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	packData, _, _, err := pack.BuildPack(objs)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	if useSideband {
		sw := NewSidebandWriter(w, SidebandData)
		_, _ = sw.Write(packData)
		_ = WriteFlush(w)
	} else {
		_, _ = w.Write(packData)
	}
}

func (s *SmartHTTPServer) handleUploadPackV2(w http.ResponseWriter, bodyBytes []byte) {
	r := bytes.NewReader(bodyBytes)
	firstPkt, _, _ := ReadPacket(r)
	firstLine := string(firstPkt)

	if strings.Contains(firstLine, "command=ls-refs") {
		allRefs, _ := s.Repo.Refs.ListRefs("")
		headOID, _ := s.Repo.Refs.ResolveHEAD()
		headRef, _ := s.Repo.Refs.ReadHEAD()

		if !headOID.IsZero() {
			if headRef != nil && headRef.IsSymref {
				_ = WritePacketString(w, fmt.Sprintf("%s HEAD symref-target:%s\n", headOID.String(), headRef.Target))
			} else {
				_ = WritePacketString(w, fmt.Sprintf("%s HEAD\n", headOID.String()))
			}
		}
		for refName, refOID := range allRefs {
			_ = WritePacketString(w, fmt.Sprintf("%s %s\n", refOID.String(), refName))
		}
		_ = WriteFlush(w)
		return
	}

	if strings.Contains(firstLine, "command=fetch") {
		var wants []object.Hash
		var haves []object.Hash

		for {
			payload, pType, err := ReadPacket(r)
			if err != nil {
				break
			}
			if pType == PktFlush || pType == PktDelim {
				continue
			}
			line := strings.TrimSpace(string(payload))
			if strings.HasPrefix(line, "want ") {
				oidStr := strings.TrimPrefix(line, "want ")
				if oid, err := object.NewHashFromHex(oidStr); err == nil {
					wants = append(wants, oid)
				}
			} else if strings.HasPrefix(line, "have ") {
				oidStr := strings.TrimPrefix(line, "have ")
				if oid, err := object.NewHashFromHex(oidStr); err == nil {
					haves = append(haves, oid)
				}
			} else if line == "done" {
				break
			}
		}

		_ = WritePacketString(w, "packfile\n")
		objs, err := CollectObjects(s.Repo, wants, haves)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		packData, _, _, err := pack.BuildPack(objs)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		sw := NewSidebandWriter(w, SidebandData)
		_, _ = sw.Write(packData)
		_ = WriteFlush(w)
		return
	}
}

// handleInfoRefsReceive 处理 GET /info/refs?service=git-receive-pack 推送前引用发现
func (s *SmartHTTPServer) handleInfoRefsReceive(w http.ResponseWriter, req *http.Request) {
	if !s.AllowPush {
		http.Error(w, "push not permitted", http.StatusForbidden)
		return
	}
	w.Header().Set("Content-Type", "application/x-git-receive-pack-advertisement")
	w.Header().Set("Cache-Control", "no-cache, no-store, max-age=0, must-revalidate")
	w.Header().Set("Pragma", "no-cache")

	_ = WritePacketString(w, "# service=git-receive-pack\n")
	_ = WriteFlush(w)

	caps := "report-status delete-refs side-band-64k ofs-delta"
	allRefs, _ := s.Repo.Refs.ListRefs("")
	headOID, _ := s.Repo.Refs.ResolveHEAD()
	headRef, _ := s.Repo.Refs.ReadHEAD()
	if headRef != nil && headRef.IsSymref {
		caps += " symref=HEAD:" + headRef.Target
	}

	if headOID.IsZero() && len(allRefs) == 0 {
		_ = WritePacketString(w, object.ZeroHash.String()+" capabilities^{}\x00"+caps+"\n")
		_ = WriteFlush(w)
		return
	}

	if !headOID.IsZero() {
		_ = WritePacketString(w, fmt.Sprintf("%s HEAD\x00%s\n", headOID.String(), caps))
	}

	for refName, refOID := range allRefs {
		if refName == "HEAD" {
			continue
		}
		_ = WritePacketString(w, fmt.Sprintf("%s %s\n", refOID.String(), refName))
	}
	_ = WriteFlush(w)
}

type refCommand struct {
	oldOID  object.Hash
	newOID  object.Hash
	refName string
}

// handleReceivePack 处理 POST /git-receive-pack 客户端推送处理
func (s *SmartHTTPServer) handleReceivePack(w http.ResponseWriter, req *http.Request) {
	if !s.AllowPush {
		http.Error(w, "push not permitted", http.StatusForbidden)
		return
	}
	w.Header().Set("Content-Type", "application/x-git-receive-pack-result")
	w.Header().Set("Cache-Control", "no-cache, no-store, max-age=0, must-revalidate")
	w.Header().Set("Pragma", "no-cache")

	var commands []refCommand
	useSideband := false
	reportStatus := false

	for {
		payload, pType, err := ReadPacket(req.Body)
		if err != nil || pType == PktFlush {
			break
		}
		line := string(payload)
		nullIdx := strings.IndexByte(line, 0)
		var caps string
		if nullIdx != -1 {
			caps = line[nullIdx+1:]
			line = line[:nullIdx]
		}
		if strings.Contains(caps, "report-status") {
			reportStatus = true
		}
		if strings.Contains(caps, "side-band-64k") || strings.Contains(caps, "side-band") {
			useSideband = true
		}

		parts := strings.Split(strings.TrimSpace(line), " ")
		if len(parts) >= 3 {
			oldOID, _ := object.NewHashFromHex(parts[0])
			newOID, _ := object.NewHashFromHex(parts[1])
			refName := parts[2]
			commands = append(commands, refCommand{
				oldOID:  oldOID,
				newOID:  newOID,
				refName: refName,
			})
		}
	}

	// 读取后续 packfile 载荷
	packData, err := io.ReadAll(req.Body)
	unpackOK := true
	unpackErr := ""
	if err != nil {
		unpackOK = false
		unpackErr = err.Error()
	} else if len(packData) > 0 {
		if _, err := pack.SavePackAndIndex(s.Repo.ObjectsDir, packData); err != nil {
			unpackOK = false
			unpackErr = err.Error()
		}
	}

	type cmdResult struct {
		refName string
		ok      bool
		errMsg  string
	}
	results := make([]cmdResult, len(commands))
	committer := s.Repo.CommitterSignature()

	for i, cmd := range commands {
		if !unpackOK {
			results[i] = cmdResult{refName: cmd.refName, ok: false, errMsg: unpackErr}
			continue
		}

		if cmd.newOID.IsZero() {
			refPath := filepath.Join(s.Repo.GitDir, cmd.refName)
			_ = os.Remove(refPath)
			results[i] = cmdResult{refName: cmd.refName, ok: true}
		} else {
			if err := s.Repo.Refs.UpdateRef(cmd.refName, cmd.newOID, committer, "push"); err != nil {
				results[i] = cmdResult{refName: cmd.refName, ok: false, errMsg: err.Error()}
			} else {
				results[i] = cmdResult{refName: cmd.refName, ok: true}
			}
		}
	}

	if reportStatus {
		var out bytes.Buffer
		if unpackOK {
			_ = WritePacketString(&out, "unpack ok\n")
		} else {
			_ = WritePacketString(&out, fmt.Sprintf("unpack %s\n", unpackErr))
		}
		for _, res := range results {
			if res.ok {
				_ = WritePacketString(&out, fmt.Sprintf("ok %s\n", res.refName))
			} else {
				_ = WritePacketString(&out, fmt.Sprintf("ng %s %s\n", res.refName, res.errMsg))
			}
		}
		_ = WriteFlush(&out)

		if useSideband {
			sw := NewSidebandWriter(w, SidebandData)
			_, _ = sw.Write(out.Bytes())
			_ = WriteFlush(w)
		} else {
			_, _ = w.Write(out.Bytes())
		}
	}
}
