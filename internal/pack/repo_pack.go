package pack

import (
	"bufio"
	"bytes"
	"compress/zlib"
	"fmt"
	"gogit/internal/object"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// ReadObjectFromPacks 在仓库 objects/pack 目录下检索所有 .idx 索引文件，查找目标对象哈希并从对应 .pack 文件中按需解压还原。
func ReadObjectFromPacks(objectsDir string, h object.Hash) (*object.RawObject, error) {
	packDir := filepath.Join(objectsDir, "pack")
	entries, err := os.ReadDir(packDir)
	if err != nil {
		return nil, ErrNotFound
	}

	for _, d := range entries {
		if d.IsDir() || !strings.HasSuffix(d.Name(), ".idx") {
			continue
		}

		idxPath := filepath.Join(packDir, d.Name())
		idxFile, err := os.Open(idxPath)
		if err != nil {
			continue
		}

		idx, err := ParseIndexV2(idxFile)
		_ = idxFile.Close()
		if err != nil {
			continue
		}

		offset, found := idx.FindObject(h)
		if !found {
			continue
		}

		packPath := filepath.Join(packDir, strings.TrimSuffix(d.Name(), ".idx")+".pack")
		packFile, err := os.Open(packPath)
		if err != nil {
			return nil, fmt.Errorf("open packfile %s: %w", packPath, err)
		}
		defer packFile.Close()

		return readEntryAtOffset(packFile, offset, objectsDir)
	}

	return nil, ErrNotFound
}

// readEntryAtOffset 从 pack 文件指定偏移处流式读取单个条目并递归解析其 delta 链
func readEntryAtOffset(f *os.File, offset uint64, objectsDir string) (*object.RawObject, error) {
	if _, err := f.Seek(int64(offset), io.SeekStart); err != nil {
		return nil, err
	}

	br := bufio.NewReader(f)
	tracker := &entryTracker{r: br, br: br}

	typeCode, _, err := readEntryHeader(tracker)
	if err != nil {
		return nil, err
	}

	var baseOff uint64
	var baseOID object.Hash

	if typeCode == TypeOFSDelta {
		negOffset, err := readOFSDeltaOffset(tracker)
		if err != nil {
			return nil, err
		}
		baseOff = offset - negOffset
	} else if typeCode == TypeREFDelta {
		if _, err := io.ReadFull(tracker, baseOID[:]); err != nil {
			return nil, err
		}
	}

	// tracker 实现了 io.ByteReader，zlib 内部不会使用多余的预读
	zr, err := zlib.NewReader(tracker)
	if err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	if _, err := io.Copy(&buf, zr); err != nil {
		zr.Close()
		return nil, err
	}
	zr.Close()

	if typeCode != TypeOFSDelta && typeCode != TypeREFDelta {
		ot, err := ObjectTypeFromPackType(typeCode)
		if err != nil {
			return nil, err
		}
		return &object.RawObject{
			ObjType: ot,
			Content: buf.Bytes(),
		}, nil
	}

	// 递归解析基准对象
	var baseObj *object.RawObject
	if typeCode == TypeOFSDelta {
		baseObj, err = readEntryAtOffset(f, baseOff, objectsDir)
		if err != nil {
			return nil, fmt.Errorf("reading ofs_delta base at %d: %w", baseOff, err)
		}
	} else {
		// REF_DELTA: 递归查询
		baseObj, err = ReadObjectFromPacks(objectsDir, baseOID)
		if err != nil {
			// 若不在 pack 中，尝试从 loose 对象中加载
			hStr := baseOID.String()
			loosePath := filepath.Join(objectsDir, hStr[:2], hStr[2:])
			baseObj, err = object.ReadLooseObject(loosePath)
			if err != nil {
				return nil, fmt.Errorf("reading ref_delta base %s: %w", baseOID.String(), err)
			}
		}
	}

	applied, err := ApplyDelta(baseObj.Content, buf.Bytes())
	if err != nil {
		return nil, fmt.Errorf("applying delta: %w", err)
	}

	return &object.RawObject{
		ObjType: baseObj.ObjType,
		Content: applied,
	}, nil
}
