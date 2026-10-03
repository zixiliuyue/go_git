package history

import (
	"bytes"
	"fmt"
	"gogit/internal/object"
	"gogit/internal/pack"
	"gogit/internal/repo"
	"strings"
)

// CreateBundle 在给定仓库中根据引用范围打包生成 Git Bundle 二进制数据
func CreateBundle(r *repo.Repository, refArgs []string, prereqOIDs []object.Hash) ([]byte, *pack.BundleHeader, error) {
	if len(refArgs) == 0 {
		return nil, nil, fmt.Errorf("必须至少指定一个打包引用")
	}

	header := &pack.BundleHeader{
		Version: 2,
	}

	var positiveOIDs []object.Hash
	haveSet := make(map[object.Hash]bool)
	for _, p := range prereqOIDs {
		haveSet[p] = true
		header.Prerequisites = append(header.Prerequisites, pack.BundleRef{
			OID: p,
		})
	}

	// 1. 解析目标正向引用
	for _, arg := range refArgs {
		var oid object.Hash
		var refName string

		if arg == "HEAD" {
			headRef, err := r.Refs.ReadHEAD()
			if err != nil {
				return nil, nil, fmt.Errorf("读取 HEAD 失败: %w", err)
			}
			oid = headRef.Hash
			refName = "HEAD"
		} else {
			ref, err := r.Refs.GetRef(arg)
			if err == nil {
				oid = ref.Hash
				refName = ref.Name
			} else {
				h, err := r.Refs.ResolveRef(arg)
				if err == nil {
					oid = h
					refName = arg
				} else {
					h, parseErr := object.NewHashFromHex(arg)
					if parseErr != nil {
						return nil, nil, fmt.Errorf("无法解析引用或哈希 %s: %w", arg, err)
					}
					oid = h
					refName = arg
				}
			}
		}

		if oid.IsZero() {
			return nil, nil, fmt.Errorf("引用 %s 指向空提交", arg)
		}

		positiveOIDs = append(positiveOIDs, oid)
		header.References = append(header.References, pack.BundleRef{
			OID:  oid,
			Name: refName,
		})
	}

	// 2. 收集所有可达对象（遇到 haveSet 中的提交即剪枝停止）
	visitedObjects := make(map[object.Hash]bool)
	var packableList []pack.PackableObject

	var collectTree func(treeOID object.Hash) error
	collectTree = func(treeOID object.Hash) error {
		if visitedObjects[treeOID] || haveSet[treeOID] {
			return nil
		}
		visitedObjects[treeOID] = true

		raw, err := r.ReadObject(treeOID)
		if err != nil {
			return err
		}
		packableList = append(packableList, pack.PackableObject{
			OID:     treeOID,
			Type:    object.TypeTree,
			Content: raw.Content,
		})

		treeObj, err := object.ParseTree(raw.Content)
		if err != nil {
			return err
		}
		for _, entry := range treeObj.Entries {
			if visitedObjects[entry.OID] || haveSet[entry.OID] {
				continue
			}
			if entry.Mode == object.ModeDirectory {
				if err := collectTree(entry.OID); err != nil {
					return err
				}
			} else {
				visitedObjects[entry.OID] = true
				blobRaw, err := r.ReadObject(entry.OID)
				if err != nil {
					return err
				}
				packableList = append(packableList, pack.PackableObject{
					OID:     entry.OID,
					Type:    object.TypeBlob,
					Content: blobRaw.Content,
				})
			}
		}
		return nil
	}

	queue := make([]object.Hash, 0, len(positiveOIDs))
	for _, oid := range positiveOIDs {
		if !haveSet[oid] && !visitedObjects[oid] {
			queue = append(queue, oid)
		}
	}

	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]

		if visitedObjects[cur] || haveSet[cur] {
			continue
		}
		visitedObjects[cur] = true

		raw, err := r.ReadObject(cur)
		if err != nil {
			return nil, nil, fmt.Errorf("读取对象 %s 失败: %w", cur, err)
		}

		packableList = append(packableList, pack.PackableObject{
			OID:     cur,
			Type:    raw.ObjType,
			Content: raw.Content,
		})

		switch raw.ObjType {
		case object.TypeCommit:
			commitObj, err := object.ParseCommit(raw.Content)
			if err != nil {
				return nil, nil, err
			}
			if err := collectTree(commitObj.Tree); err != nil {
				return nil, nil, err
			}
			for _, p := range commitObj.Parents {
				if !haveSet[p] && !visitedObjects[p] {
					queue = append(queue, p)
				}
			}
		case object.TypeTree:
			if err := collectTree(cur); err != nil {
				return nil, nil, err
			}
		case object.TypeTag:
			tagObj, err := object.ParseTag(raw.Content)
			if err != nil {
				return nil, nil, err
			}
			if !haveSet[tagObj.Object] && !visitedObjects[tagObj.Object] {
				queue = append(queue, tagObj.Object)
			}
		}
	}

	// 3. 将收集到的对象打包为 packfile
	packBytes, _, _, err := pack.BuildPack(packableList)
	if err != nil {
		return nil, nil, fmt.Errorf("构建 pack 数据失败: %w", err)
	}

	// 4. 将 Header 与 Packfile 合并序列化为完整 bundle
	var buf bytes.Buffer
	if err := pack.WriteBundle(&buf, header, packBytes); err != nil {
		return nil, nil, err
	}

	return buf.Bytes(), header, nil
}

// VerifyBundle 校验本地仓库是否满足 Bundle 的先决提交条件
func VerifyBundle(r *repo.Repository, header *pack.BundleHeader) error {
	var missing []string
	for _, p := range header.Prerequisites {
		if !r.HasObject(p.OID) {
			missing = append(missing, p.OID.String())
		}
	}

	if len(missing) > 0 {
		return fmt.Errorf("缺少先决提交: %s", strings.Join(missing, ", "))
	}

	if len(header.References) == 0 {
		return fmt.Errorf("bundle 文件中未包含任何有效引用")
	}

	return nil
}

// Unbundle 将 Bundle 中的 packfile 导入本地仓库并生成 .idx 索引
func Unbundle(r *repo.Repository, header *pack.BundleHeader, packData []byte) error {
	// 1. 校验先决条件
	if err := VerifyBundle(r, header); err != nil {
		return err
	}

	// 2. 将 packfile 保存到 objects/pack 并生成 .idx 索引文件
	if len(packData) > 0 {
		if _, err := pack.SavePackAndIndex(r.ObjectsDir, packData); err != nil {
			return fmt.Errorf("保存 packfile 与生成索引失败: %w", err)
		}
	}

	return nil
}
