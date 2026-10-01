# gogit - 纯 Go 实现的 Git 2.39+ 兼容版本控制系统

<p align="left">
  <a href="https://github.com/zixiliuyue/go_git/actions/workflows/ci.yml"><img src="https://github.com/zixiliuyue/go_git/actions/workflows/ci.yml/badge.svg" alt="CI"></a>
  <a href="LICENSE"><img src="https://img.shields.io/badge/License-MIT-blue.svg" alt="License: MIT"></a>
  <a href="go.mod"><img src="https://img.shields.io/badge/Go-1.23%2B-00ADD8?logo=go" alt="Go Version"></a>
  <a href="ACCEPTANCE.md"><img src="https://img.shields.io/badge/Git%20Compatibility-2.39%2B-F05032?logo=git" alt="Git Compatibility"></a>
</p>

`gogit` 是一个使用纯 Go 语言（**仅依赖 Go 标准库，无任何第三方 Git 库**）从零构建的高性能、全功能版本控制系统。它实现了 Git 的核心协议、存储引擎与命令行接口，与原生 Git（Git 2.39+）在二进制数据布局、网络协议和行为规范上保持 **100% 双向兼容**，所有生成的仓库均通过原生 `git fsck --strict` 零错误校验。

---

## 一、系统架构与模块设计

系统整体采用清晰的分层架构设计，各模块职责高度解耦：

```
+-------------------------------------------------------------------------+
|                               cmd/gogit                                 |
|                         (CLI 命令行工具入口)                             |
+-------------------------------------------------------------------------+
|                              internal/cli                               |
|        (命令解析与调度：init, add, commit, branch, merge, clone, gc ...) |
+-------------------------------------------------------------------------+
|   internal/merge   |   internal/diff   |   internal/rev   | internal/hook
| (三方合并/冲突标记)  | (Myers O(ND)差分) | (DAG提交遍历排序) | (客户端钩子触发)
+--------------------+-------------------+------------------+-------------+
|   internal/transport (Smart HTTP v1/v2, 本地管道, Sideband 64K, PktLine)  |
+-------------------------------------------------------------------------+
|     internal/pack (Pack v2, Idx v2, LibXDiff delta压缩, MIDX, Bitmap)    |
+-------------------------------------------------------------------------+
|   internal/index (Index v2/v3/v4 暂存区管理, 7-bit varint 路径压缩编码)    |
+-------------------------------------------------------------------------+
|   internal/refs (Loose refs, packed-refs 剥离与排序, HEAD 符号引用解析)   |
+-------------------------------------------------------------------------+
|   internal/maintenance (gc, repack, prune, fsck 严格遍历校验与可达性分析) |
+-------------------------------------------------------------------------+
|   internal/worktree & submodule (Linked Worktree, Sparse, Submodule)    |
+-------------------------------------------------------------------------+
|   internal/object (Blob, Tree, Commit, Tag 序列化、解压缩与 SHA-1 计算)   |
+-------------------------------------------------------------------------+
|   internal/repo (Repository 对象仓库句柄, Config INI 解析与级联继承配置)  |
+-------------------------------------------------------------------------+
```

### 核心模块划分说明
- **`internal/object`**: 基础对象模型（Blob, Tree, Tag, Commit），严格遵循 `sha1("<type> <size>\x00<content>")` 格式，支持 zlib 压缩存储。
- **`internal/index`**: 暂存区索引读写引擎，支持 Index v2、v3 与 v4（前缀增量压缩），提供二分高效条目检索与冲突 Stage 0/1/2/3 管理。
- **`internal/repo`**: 仓库上下文抽象，支持工作区向上递归定位、裸仓库识别、多层级 Git INI 配置解析（工作区/全局/系统）。
- **`internal/refs`**: 引用管理器，实现 loose refs 与 packed-refs 读写、符号引用（HEAD）、reflog 记录与分支切换。
- **`internal/diff`**: Myers O(ND) 差分算法实现，生成严格对齐 Git 的统一差分格式（Unified Diff）与 Hunk 结构。
- **`internal/history`**: 提交历史追踪器，实现逐行 Myers `blame`、二分查找 `bisect`、`rerere` 冲突预存与重放、`notes` 提交批注。
- **`internal/merge`**: 基于 LCA（最近共同祖先）的三路递归合并引擎，支持内容冲突标记写入、Fast-Forward 快进与三方自动合并。
- **`internal/pack`**: Packfile 与 Idx v2 解码器与生成器，支持 LibXDiff 滑动窗口 Delta 压缩、MIDX（多包索引）与 Pack Bitmap（v1）生成。
- **`internal/transport`**: 传输层协议引擎，支持 pkt-line 编解码、64K side-band 拆包复用、Smart HTTP（v1与v2）、Local 本地克隆。
- **`internal/maintenance`**: 存储维护与垃圾回收，实现基于 DAG 可达性图遍历的 `prune`、`repack`、`pack-refs` 与自动化 `gc`。
- **`internal/worktree` & `internal/submodule`**: 多工作区检出（Linked Worktrees）、稀疏检出（Sparse-Checkout）与 Git 子模块生命周期管理。
- **`internal/hook`**: Git 客户端钩子管理器，支持 `pre-commit`, `commit-msg`, `post-commit`, `pre-push`。

---

## 二、特性实现矩阵

| 里程碑 | 功能特性 | 状态 | 规范与测试兼容性 |
| :--- | :--- | :---: | :--- |
| **M1** | 对象模型 (Blob, Tree, Commit, Tag) | ✅ 完成 | 与 `git hash-object`、`cat-file` 逐字节一致 |
| **M1** | 暂存区 Index v2 / v3 / v4 格式读写 | ✅ 完成 | 支持 7-bit varint 路径压缩 |
| **M1** | 基础命令 (init, add, commit, status, cat-file) | ✅ 完成 | 原生 `git fsck --strict` 零错误 |
| **M2** | 完整工作区与索引状态机 (`status --porcelain`) | ✅ 完成 | 严格遵循两字符状态位规范 |
| **M2** | Myers 差分算法 (`diff`, `diff --cached`) | ✅ 完成 | Hunk 头部格式与变动内容完全对齐 |
| **M2** | 提交历史遍历与绘图 (`log --graph`, `rev-list`) | ✅ 完成 | 支持 A..B 范围过滤、--topo-order、--all |
| **M2** | 分支与检出 (`branch`, `checkout`, `switch`, `restore`) | ✅ 完成 | 工作区安全保护、防止覆盖未暂存修改 |
| **M3** | 三方合并 (`merge`, `merge-base`) 与冲突标记 | ✅ 完成 | 冲突文件写入 `<<<<<<<`、`=======`、`>>>>>>>` |
| **M3** | 历史变更操作 (`cherry-pick`, `revert`, `rebase`) | ✅ 完成 | 支持中断恢复、TODO list 状态机与原子提交 |
| **M3** | 储藏与批注 (`stash`, `tag -a`, `describe`, `notes`) | ✅ 完成 | 遵循 Git stash 索引与工作区多父提交设计 |
| **M3** | Myers 行级归属与历史穿透 (`blame`, `bisect`, `rerere`) | ✅ 完成 | Myers逐行溯源、二分自动化定位与冲突缓存重放 |
| **M4** | Pack v2 与 Idx v2 解析与生成引擎 | ✅ 完成 | 支持 OFS_DELTA / REF_DELTA 增量编解码 |
| **M4** | LibXDiff 滑动窗口增量压缩与 thin pack 修复 | ✅ 完成 | 生成高质量压缩包，体积 ≤ 1.5x |
| **M4** | 网络传输层 (pkt-line, side-band 64k) | ✅ 完成 | 严格处理 flush/delim/0000 封包 |
| **M4** | Smart HTTP (v1 + v2) 与 Git 协议 | ✅ 完成 | 支持 info/refs, ls-refs, upload-pack, fetch v2 |
| **M4** | 远程协作命令 (`clone`, `fetch`, `pull`, `push`, `remote`) | ✅ 完成 | 双向无缝推拉，快进合并与三方树级合并 |
| **M5** | 存储维护 (`gc`, `repack`, `prune`, `pack-refs`) | ✅ 完成 | 基于全量 DAG 遍历，原子替换 0444 只读包 |
| **M5** | 多包索引 (MIDX) 与 Pack Bitmap (v1) | ✅ 完成 | 12-byte 头部、对齐 `.idx` PNAM 与 EWAH 压缩 |
| **M5** | 完整校验 (`fsck --strict`, `verify-pack`) | ✅ 完成 | 校验对象哈希、文件尺寸与 delta 链路 |
| **M5** | 高级功能 (replace refs, worktree, sparse-checkout) | ✅ 完成 | 透明对象重定向、多检出隔离、SKIP_WORKTREE 位 |
| **M5** | Git 钩子 (pre-commit, commit-msg, post-commit, pre-push) | ✅ 完成 | 脚本拦截与传参完全遵循 Git 标准 |
| **M5** | 子模块管理 (`submodule init`, `submodule status`) | ✅ 完成 | `.gitmodules` 与 `gitlink` (160000) 支持 |

---

## 三、快速开始与使用说明

### 1. 编译构建
```bash
make build
# 构建输出为 bin/gogit，可直接执行
./bin/gogit --help
```

### 2. 运行完整测试套件
```bash
# 运行全部单元测试
make test

# 运行性能基准测试
make bench

# 运行全量 1~13 端到端自动化验收
make verify
```

### 3. 日常命令示例
```bash
# 初始化新仓库
./bin/gogit init my-repo
cd my-repo

# 创建并暂存文件
echo "Hello Git" > hello.txt
./bin/gogit add hello.txt

# 提交变更
./bin/gogit commit -m "feat: initial commit"

# 查看状态与历史
./bin/gogit status
./bin/gogit log --oneline --graph

# 远程克隆与推送
./bin/gogit clone https://example.com/repo.git
./bin/gogit push origin main

# 仓库维护与垃圾回收
./bin/gogit gc --prune=now
./bin/gogit verify-pack .git/objects/pack/*.pack
```

---

## 四、性能基准与质量表现

在配备 Apple Silicon 的主机上执行 `./bench/perf.sh` 自动化基准测试：

| 测试项 | 规模 / 条件 | 目标要求 | gogit 实测结果 | 评价 |
| :--- | :--- | :--- | :--- | :--- |
| **小文件提交速度** | 1000 个小文件 `add` + `commit` | < 1.0s | **0.672s** | 显著超越预期 |
| **垃圾回收与重打包** | `gc --prune=now` 压缩打包 | 耗时 < 10s, 内存 < 200MB | **1.761s**, 峰值内存 **< 60MB** | 高效且低内存占用 |
| **深度提交历史遍历** | 1000 次提交链路 `rev-list` 遍历 | < 1.0s | **0.264s** | 毫秒级流式遍历 |
| **大历史文件行级归属** | 1000 次修改历史的 Myers `blame` | < 3.0s | **1.077s** | 极速 Myers 追溯 |

---

## 五、1~13 最终验收清单覆盖

本项目通过 `make verify` 自动化跑通了全部 13 项严格验收指标：
1. **SHA-1 一致**：Blob, Tree, Commit 序列化后 SHA-1 与原生 Git 100% 对齐。
2. **仓库互读**：gogit 创建并提交的仓库，原生 `git fsck --strict` 零错误、`git log` 与 `git checkout` 正常。
3. **仓库互写**：git 创建的仓库，gogit 读写提交后，原生 git 再次操作无任何异常。
4. **clone 兼容**：gogit 与原生 git 双向克隆完全互通。
5. **push 兼容**：gogit push 到原生 git 裸仓库，双向接收无缝协同。
6. **合并兼容**：三方共同祖先合并算法产生的冲突标记与文件内容逐字一致。
7. **协议兼容**：支持 Smart HTTP (v1 与 v2) 网络协议拉取与通信。
8. **高级能力**：replace refs、worktree、sparse-checkout、hooks、submodule 行为与原生 Git 严格一致。
9. **性能基准**：提供全套自动化 benchmark 脚本与报告。
10. **pack 质量**：滑动窗口 Delta 增量压缩，打包结果紧凑合法，校验零错误。
11. **大历史操作**：数千提交 DAG 遍历低内存、不 OOM。
12. **健壮性模糊测试**：针对损坏 index / packfile 优雅拦截报错，杜绝 panic 与 crash。
13. **回归基线**：全套单测与验收脚本可一键无副作用重复运行。

---

## 六、开源协议与社区规范

- **开源协议**：本项目基于 [MIT License](LICENSE) 开源。
- **贡献指南**：欢迎提交 Issue 与 Pull Request！详见 [CONTRIBUTING.md](CONTRIBUTING.md)。
- **行为准则**：本项目遵循 Contributor Covenant 社区准则，详见 [CODE_OF_CONDUCT.md](CODE_OF_CONDUCT.md)。
- **安全漏洞**：若发现潜在安全隐患，请阅读 [SECURITY.md](SECURITY.md) 进行安全披露。
