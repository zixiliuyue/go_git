package transport

import (
	"fmt"
	"gogit/internal/object"
	"gogit/internal/pack"
	"gogit/internal/repo"
	"strings"
)

// RefUpdate 描述一次引用更新请求
type RefUpdate struct {
	Name    string
	OldOID  object.Hash
	NewOID  object.Hash
	IsForce bool
}

// LocalTransport 实现对本地文件系统仓库的传输操作
type LocalTransport struct {
	Endpoint *Endpoint
}

func NewLocalTransport(ep *Endpoint) *LocalTransport {
	return &LocalTransport{Endpoint: ep}
}

// DiscoverReferences 获取本地仓库的全部引用列表与 HEAD 符号引用
func (lt *LocalTransport) DiscoverReferences() ([]RemoteRef, string, error) {
	r, err := repo.FindRepository(lt.Endpoint.Path)
	if err != nil {
		return nil, "", fmt.Errorf("open local repository at %s: %w", lt.Endpoint.Path, err)
	}

	rawRefs, err := r.Refs.ListRefs("")
	if err != nil {
		return nil, "", err
	}

	headRef, err := r.Refs.ReadHEAD()
	var headSymref string
	var headOID object.Hash

	if err == nil && headRef != nil {
		if headRef.IsSymref {
			headSymref = headRef.Target
			if targetOID, err := r.Refs.ResolveRef(headRef.Target); err == nil {
				headOID = targetOID
			}
		} else {
			headOID = headRef.Hash
		}
	}

	var results []RemoteRef
	if !headOID.IsZero() {
		results = append(results, RemoteRef{
			OID:    headOID,
			Name:   "HEAD",
			Target: headSymref,
		})
	}

	for name, oid := range rawRefs {
		rr := RemoteRef{
			OID:  oid,
			Name: name,
		}
		// 若为附注标签，尝试剥离
		if strings.HasPrefix(name, "refs/tags/") {
			if raw, err := r.ReadObject(oid); err == nil && raw.ObjType == object.TypeTag {
				if tagObj, err := object.ParseTag(raw.Content); err == nil {
					rr.Peeled = tagObj.Object
				}
			}
		}
		results = append(results, rr)
	}

	return results, headSymref, nil
}

// FetchPack 从本地仓库中检索从 wants 开始、在 haves 处截断的全部对象，打包为 packfile 字节流返回
func (lt *LocalTransport) FetchPack(wants []object.Hash, haves []object.Hash) ([]byte, error) {
	r, err := repo.FindRepository(lt.Endpoint.Path)
	if err != nil {
		return nil, err
	}

	haveSet := make(map[object.Hash]bool, len(haves))
	for _, h := range haves {
		haveSet[h] = true
	}

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

		for _, e := range treeObj.Entries {
			if visitedObjects[e.OID] || haveSet[e.OID] {
				continue
			}
			if e.Mode == object.ModeDirectory {
				if err := collectTree(e.OID); err != nil {
					return err
				}
			} else {
				visitedObjects[e.OID] = true
				blobRaw, err := r.ReadObject(e.OID)
				if err != nil {
					return err
				}
				packableList = append(packableList, pack.PackableObject{
					OID:     e.OID,
					Type:    object.TypeBlob,
					Content: blobRaw.Content,
				})
			}
		}
		return nil
	}

	// 遍历 commits
	queue := make([]object.Hash, 0, len(wants))
	for _, w := range wants {
		if !haveSet[w] && !visitedObjects[w] {
			queue = append(queue, w)
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
			return nil, err
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
				return nil, err
			}
			if err := collectTree(commitObj.Tree); err != nil {
				return nil, err
			}
			for _, p := range commitObj.Parents {
				if !haveSet[p] && !visitedObjects[p] {
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
			if !haveSet[tagObj.Object] && !visitedObjects[tagObj.Object] {
				queue = append(queue, tagObj.Object)
			}
		}
	}

	packBytes, _, _, err := pack.BuildPack(packableList)
	if err != nil {
		return nil, fmt.Errorf("building pack: %w", err)
	}

	return packBytes, nil
}

// PushPack 将 pack 数据和引用更新推送到本地目标仓库
func (lt *LocalTransport) PushPack(updates []RefUpdate, packData []byte) error {
	r, err := repo.FindRepository(lt.Endpoint.Path)
	if err != nil {
		return err
	}

	// 1. 若携带 pack 数据，落盘并索引
	if len(packData) > 0 {
		if _, err := pack.SavePackAndIndex(r.ObjectsDir, packData); err != nil {
			return fmt.Errorf("saving pack to local repository: %w", err)
		}
	}

	// 2. 应用引用更新
	for _, u := range updates {
		if u.NewOID.IsZero() {
			// 删除引用
			_ = r.Refs.DeleteRef(u.Name)
		} else {
			if err := r.Refs.UpdateRef(u.Name, u.NewOID, r.AuthorSignature(), "push"); err != nil {
				return fmt.Errorf("updating ref %s: %w", u.Name, err)
			}
		}
	}

	return nil
}
