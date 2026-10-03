package pack

import (
	"bytes"
	"crypto/sha1"
	"encoding/binary"
	"errors"
	"fmt"
	"gogit/internal/object"
	"os"
	"sort"
)

var (
	// CommitGraphMagic 是 Git commit-graph 文件的 4 字节魔数 "CGPH"
	CommitGraphMagic = [4]byte{'C', 'G', 'P', 'H'}
)

// CommitGraphEntry 表示 commit-graph 中记录的单个提交项
type CommitGraphEntry struct {
	OID        object.Hash
	Tree       object.Hash
	Parents    []object.Hash
	Generation uint32
	CommitTime uint64
}

// CommitGraphInput 写入 commit-graph 所需的原始提交数据
type CommitGraphInput struct {
	OID        object.Hash
	Tree       object.Hash
	Parents    []object.Hash
	CommitTime uint64
}

// CommitGraph 表示内存中解析出的提交图缓存结构
type CommitGraph struct {
	NumCommits uint32
	OIDLookup  []object.Hash
	Entries    []CommitGraphEntry
	lookupMap  map[object.Hash]int
}

// Get 快速通过 OID 检索提交图中的提交元数据（O(1) 检索或二分）
func (cg *CommitGraph) Get(oid object.Hash) (*CommitGraphEntry, bool) {
	if cg.lookupMap != nil {
		if idx, ok := cg.lookupMap[oid]; ok {
			return &cg.Entries[idx], true
		}
		return nil, false
	}
	idx := sort.Search(len(cg.OIDLookup), func(i int) bool {
		return bytes.Compare(cg.OIDLookup[i][:], oid[:]) >= 0
	})
	if idx < len(cg.OIDLookup) && cg.OIDLookup[idx] == oid {
		return &cg.Entries[idx], true
	}
	return nil, false
}

// ReadCommitGraph 从指定文件路径读取并解析 Git 标准 commit-graph 文件
func ReadCommitGraph(path string) (*CommitGraph, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	if len(data) < 8+12+20 {
		return nil, errors.New("commit-graph 文件体积过小，不符合格式规范")
	}

	contentLen := len(data) - 20
	expectedChecksum := sha1.Sum(data[:contentLen])
	if !bytes.Equal(expectedChecksum[:], data[contentLen:]) {
		return nil, errors.New("commit-graph 校验和损坏 (SHA-1 mismatch)")
	}

	if !bytes.Equal(data[0:4], CommitGraphMagic[:]) {
		return nil, fmt.Errorf("非法的 commit-graph 魔数: %v", data[0:4])
	}
	version := data[4]
	if version != 1 {
		return nil, fmt.Errorf("不支持的 commit-graph 版本: %d (仅支持 v1)", version)
	}
	hashVer := data[5]
	if hashVer != 1 {
		return nil, fmt.Errorf("不支持的哈希版本: %d (仅支持 SHA-1)", hashVer)
	}
	chunkCount := int(data[6])

	type chunkInfo struct {
		id     [4]byte
		offset uint64
	}
	chunks := make([]chunkInfo, chunkCount+1)
	tocOffset := 8
	for i := 0; i <= chunkCount; i++ {
		if tocOffset+12 > contentLen {
			return nil, errors.New("commit-graph TOC 块超出内容范围")
		}
		copy(chunks[i].id[:], data[tocOffset:tocOffset+4])
		chunks[i].offset = binary.BigEndian.Uint64(data[tocOffset+4 : tocOffset+12])
		tocOffset += 12
	}

	getChunkSlice := func(id [4]byte) ([]byte, error) {
		for i := 0; i < chunkCount; i++ {
			if chunks[i].id == id {
				start := chunks[i].offset
				end := chunks[i+1].offset
				if start > end || end > uint64(contentLen) {
					return nil, fmt.Errorf("chunk %s 偏移区间非法: [%d, %d]", string(id[:]), start, end)
				}
				return data[start:end], nil
			}
		}
		return nil, nil
	}

	oidfData, err := getChunkSlice([4]byte{'O', 'I', 'D', 'F'})
	if err != nil || len(oidfData) != 256*4 {
		return nil, errors.New("缺少合法 OIDF chunk")
	}
	numCommits := binary.BigEndian.Uint32(oidfData[255*4 : 256*4])

	oidlData, err := getChunkSlice([4]byte{'O', 'I', 'D', 'L'})
	if err != nil || len(oidlData) != int(numCommits)*20 {
		return nil, errors.New("缺少合法 OIDL chunk 或提交数不匹配")
	}

	cdatData, err := getChunkSlice([4]byte{'C', 'D', 'A', 'T'})
	if err != nil || len(cdatData) != int(numCommits)*36 {
		return nil, errors.New("缺少合法 CDAT chunk 或记录尺寸不匹配")
	}

	edgeData, _ := getChunkSlice([4]byte{'E', 'D', 'G', 'E'})

	cg := &CommitGraph{
		NumCommits: numCommits,
		OIDLookup:  make([]object.Hash, numCommits),
		Entries:    make([]CommitGraphEntry, numCommits),
		lookupMap:  make(map[object.Hash]int, numCommits),
	}

	for i := uint32(0); i < numCommits; i++ {
		copy(cg.OIDLookup[i][:], oidlData[i*20:(i+1)*20])
		cg.lookupMap[cg.OIDLookup[i]] = int(i)
	}

	for i := uint32(0); i < numCommits; i++ {
		rec := cdatData[i*36 : (i+1)*36]
		var treeHash object.Hash
		copy(treeHash[:], rec[:20])

		p1 := binary.BigEndian.Uint32(rec[20:24])
		p2 := binary.BigEndian.Uint32(rec[24:28])
		genTime := binary.BigEndian.Uint64(rec[28:36])

		gen := uint32(genTime >> 34)
		cTime := genTime & 0x00000003ffffffff

		var parents []object.Hash
		if p1 != 0x70000000 {
			if p1 < numCommits {
				parents = append(parents, cg.OIDLookup[p1])
			}
			if p2 != 0x70000000 {
				if (p2 & 0x80000000) != 0 {
					edgeIdx := int(p2 & 0x7fffffff)
					for edgeIdx*4+4 <= len(edgeData) {
						edgeVal := binary.BigEndian.Uint32(edgeData[edgeIdx*4 : (edgeIdx+1)*4])
						pIdx := edgeVal & 0x7fffffff
						if pIdx < numCommits {
							parents = append(parents, cg.OIDLookup[pIdx])
						}
						edgeIdx++
						if (edgeVal & 0x80000000) != 0 {
							break
						}
					}
				} else if p2 < numCommits {
					parents = append(parents, cg.OIDLookup[p2])
				}
			}
		}

		cg.Entries[i] = CommitGraphEntry{
			OID:        cg.OIDLookup[i],
			Tree:       treeHash,
			Parents:    parents,
			Generation: gen,
			CommitTime: cTime,
		}
	}

	return cg, nil
}

// EncodeCommitGraph 将提交数据列表编码为标准 Git commit-graph 字节流
func EncodeCommitGraph(inputs []CommitGraphInput) ([]byte, error) {
	if len(inputs) == 0 {
		return nil, errors.New("inputs 不能为空")
	}

	numCommits := len(inputs)
	commitMap := make(map[object.Hash]CommitGraphInput, numCommits)
	for _, in := range inputs {
		commitMap[in.OID] = in
	}

	sortedHashes := make([]object.Hash, 0, numCommits)
	for h := range commitMap {
		sortedHashes = append(sortedHashes, h)
	}
	sort.Slice(sortedHashes, func(i, j int) bool {
		return bytes.Compare(sortedHashes[i][:], sortedHashes[j][:]) < 0
	})

	hashToPos := make(map[object.Hash]uint32, numCommits)
	for i, h := range sortedHashes {
		hashToPos[h] = uint32(i)
	}

	genMap := make(map[object.Hash]uint32, numCommits)
	visiting := make(map[object.Hash]bool)
	var calcGen func(h object.Hash) uint32
	calcGen = func(h object.Hash) uint32 {
		if g, ok := genMap[h]; ok {
			return g
		}
		if visiting[h] {
			return 1
		}
		visiting[h] = true
		in, exists := commitMap[h]
		if !exists || len(in.Parents) == 0 {
			genMap[h] = 1
			return 1
		}
		maxGen := uint32(0)
		for _, p := range in.Parents {
			pg := calcGen(p)
			if pg > maxGen {
				maxGen = pg
			}
		}
		res := maxGen + 1
		if res >= (1 << 30) {
			res = (1 << 30) - 1
		}
		genMap[h] = res
		return res
	}

	for _, h := range sortedHashes {
		calcGen(h)
	}

	// 1. OIDF 块: 256 * 4 字节
	oidf := make([]byte, 256*4)
	counts := make([]uint32, 256)
	for _, h := range sortedHashes {
		counts[h[0]]++
	}
	accum := uint32(0)
	for i := 0; i < 256; i++ {
		accum += counts[i]
		binary.BigEndian.PutUint32(oidf[i*4:(i+1)*4], accum)
	}

	// 2. OIDL 块: numCommits * 20 字节
	oidl := make([]byte, numCommits*20)
	for i, h := range sortedHashes {
		copy(oidl[i*20:(i+1)*20], h[:])
	}

	// 3. CDAT 与 EDGE 块
	cdat := make([]byte, numCommits*36)
	var edgeBytes []byte

	for i, h := range sortedHashes {
		in := commitMap[h]
		rec := cdat[i*36 : (i+1)*36]

		copy(rec[:20], in.Tree[:])

		p1 := uint32(0x70000000)
		p2 := uint32(0x70000000)

		if len(in.Parents) == 1 {
			if pos, ok := hashToPos[in.Parents[0]]; ok {
				p1 = pos
			}
		} else if len(in.Parents) == 2 {
			if pos, ok := hashToPos[in.Parents[0]]; ok {
				p1 = pos
			}
			if pos, ok := hashToPos[in.Parents[1]]; ok {
				p2 = pos
			}
		} else if len(in.Parents) > 2 {
			if pos, ok := hashToPos[in.Parents[0]]; ok {
				p1 = pos
			}
			edgeOffset := uint32(len(edgeBytes) / 4)
			p2 = 0x80000000 | edgeOffset

			for pIdx := 1; pIdx < len(in.Parents); pIdx++ {
				posVal := uint32(0)
				if pos, ok := hashToPos[in.Parents[pIdx]]; ok {
					posVal = pos
				}
				if pIdx == len(in.Parents)-1 {
					posVal |= 0x80000000
				}
				buf := make([]byte, 4)
				binary.BigEndian.PutUint32(buf, posVal)
				edgeBytes = append(edgeBytes, buf...)
			}
		}

		binary.BigEndian.PutUint32(rec[20:24], p1)
		binary.BigEndian.PutUint32(rec[24:28], p2)

		gen := genMap[h]
		genTime := (uint64(gen) << 34) | (in.CommitTime & 0x00000003ffffffff)
		binary.BigEndian.PutUint64(rec[28:36], genTime)
	}

	hasEdge := len(edgeBytes) > 0
	numChunks := 3
	if hasEdge {
		numChunks = 4
	}

	tocLen := (numChunks + 1) * 12
	currentOffset := uint64(8 + tocLen)

	var tocBuf bytes.Buffer

	// OIDF
	tocBuf.Write([]byte("OIDF"))
	_ = binary.Write(&tocBuf, binary.BigEndian, currentOffset)
	currentOffset += uint64(len(oidf))

	// OIDL
	tocBuf.Write([]byte("OIDL"))
	_ = binary.Write(&tocBuf, binary.BigEndian, currentOffset)
	currentOffset += uint64(len(oidl))

	// CDAT
	tocBuf.Write([]byte("CDAT"))
	_ = binary.Write(&tocBuf, binary.BigEndian, currentOffset)
	currentOffset += uint64(len(cdat))

	if hasEdge {
		tocBuf.Write([]byte("EDGE"))
		_ = binary.Write(&tocBuf, binary.BigEndian, currentOffset)
		currentOffset += uint64(len(edgeBytes))
	}

	tocBuf.Write([]byte{0, 0, 0, 0})
	_ = binary.Write(&tocBuf, binary.BigEndian, currentOffset)

	var fileBuf bytes.Buffer
	fileBuf.Write(CommitGraphMagic[:])
	fileBuf.WriteByte(1)
	fileBuf.WriteByte(1)
	fileBuf.WriteByte(byte(numChunks))
	fileBuf.WriteByte(0)

	fileBuf.Write(tocBuf.Bytes())
	fileBuf.Write(oidf)
	fileBuf.Write(oidl)
	fileBuf.Write(cdat)
	if hasEdge {
		fileBuf.Write(edgeBytes)
	}

	trailerChecksum := sha1.Sum(fileBuf.Bytes())
	fileBuf.Write(trailerChecksum[:])

	return fileBuf.Bytes(), nil
}
