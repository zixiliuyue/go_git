# gogit 开发进展记录 (PROGRESS.md)

## 1. 总体目标与状态
- 目标：从零使用 Go 实现与真实 Git 2.39+ 在数据格式、CLI 语义、网络协议、性能量级四方面完全兼容的版本控制系统 `gogit`。
- 依赖：纯 Go 标准库实现，不依赖外部 Git 库（仅测试中用原生 git 作为 oracle 对比）。
- 当前阶段：**M4 - 传输与克隆（v1+v2）** (M1、M2、M3 已圆满验收通过并打 tag)。

## 2. 里程碑任务分解与状态 (TODO 清单)

- [x] **M1: 对象模型与最小工作流**
  - [x] `internal/object`: 基础对象模型（blob, tree, commit, tag）、哈希计算（SHA-1）、loose 对象读写（`<type> <size>\0` + zlib 压缩）
  - [x] `internal/index`: 索引格式 v2 读写、entry 解析与序列化、状态管理、checksum 计算
  - [x] `internal/tree`: Tree 结构序列化/反序列化，目录按 git 规范排序（带 `/`）
  - [x] `internal/refs`: 基础引用系统（HEAD, refs/heads/*, peeled tag, symbolic ref）
  - [x] `internal/repo`: 仓库发现、`.git` 目录结构、配置读取基础
  - [x] `internal/cli`: 命令行调度器，退出码与统一错误处理
  - [x] CLI 命令实现：`init`, `hash-object`, `cat-file`, `ls-tree`, `write-tree`, `commit-tree`, `add`, `commit`, `rev-parse`, `log`, `fsck`
  - [x] M1 阶段验证：与系统原生 git 互操作对比测试，通过 `git fsck --strict`。
  - [x] 打 Tag: `M1` 并提交。

- [x] **M2: 索引/状态/diff/log/分支**
  - [x] `internal/worktree`: 工作区扫描、`.gitignore` 完整规则匹配（通配符、取反 `!`、目录限定、`**`）、`.gitattributes` 基础
  - [x] `internal/diff`: Myers 经典差分算法实现、Unified Hunk 聚合与格式化、`--stat`, `--numstat`, `--name-status`, rename/copy 检测 (-M)
  - [x] `status` 命令：porcelain 格式逐行与 git 对齐、stat 缓存双通道比对
  - [x] `diff` 命令：worktree ↔ index ↔ HEAD 两两比较、`--cached`、`--stat`
  - [x] `branch` 命令：列表、创建、删除 (-d/-D)、重命名 (-m)
  - [x] `checkout` / `switch` 命令：分支切换、detached HEAD、工作区检出与索引重建
  - [x] `restore`, `clean` 命令（`-f`, `-d`, `-x` 支持）
  - [x] `log` 命令：`--graph` 分支拓扑图、`--all`、`-p`、`--stat`
  - [x] 索引 v3/v4 前缀压缩解析/序列化与 split index link 扩展
  - [x] M2 阶段验证与 Tag: `M2`。

- [x] **M3: 合并与历史操作**
  - [x] `internal/merge`: 三路合并算法、递归 LCA (merge-base)、冲突标记逐字一致
  - [x] `merge` 命令 (fast-forward, 真实三路合并，MERGE_HEAD / MERGE_MSG / MERGE_MODE 支持)
  - [x] `rebase` 命令 (基础, `--onto`, `-i` 交互式 todo 状态机)
  - [x] `cherry-pick`, `revert` (CHERRY_PICK_HEAD / REVERT_HEAD 维护)
  - [x] `stash` 全套 (push/pop/apply/list/drop/show)
  - [x] `tag` (lightweight, annotated `-a` 带 tagger 签名与对象剥离)
  - [x] `describe`, `blame` (Myers 行级归属与历史穿透), `bisect` (二分搜索与自动化 run), `notes`, `rerere` (预存镜像与自动回填)
  - [x] M3 阶段验证与 Tag: `M3`。

- [ ] **M4: 传输与克隆（v1+v2）**
  - [ ] `internal/pack`: pack v2 与 idx v2 解析与生成、OFS_DELTA / REF_DELTA 增量编解码、thin pack 修复
  - [ ] `internal/transport`: pkt-line 协议、side-band 多路复用
  - [ ] Smart HTTP v1 & v2 (info/refs 探测、ls-refs、upload-pack、fetch v2、receive-pack)
  - [ ] 本地协议 / SSH 协议（调用本地 ssh） / git daemon 协议 / dumb HTTP 兼容
  - [ ] `remote`, `fetch`, `pull`, `push`, `clone` 命令
  - [ ] M4 阶段验证与 Tag: `M4`。

- [ ] **M5: 存储维护、高级能力与总验收**
  - [ ] `gc`, `repack`, `prune`, `pack-refs`, reflog 过期
  - [ ] pack bitmap (v1) 与 multi-pack-index (MIDX)
  - [ ] `fsck` 完整校验 (--strict), `verify-pack`
  - [ ] shallow clone / partial clone / replace refs / sparse-checkout
  - [ ] 全套 hooks 支持
  - [ ] 最终验收清单 1-13 验证与性能基准测试
  - [ ] M5 阶段验证与 Tag: `M5`。

## 3. 关键设计决策记录
- 架构设计：严格分层，所有数据包与算法模块独立，支持单独单测。
- 字节兼容：对象 SHA-1 严格遵循 Git 规则：`sha1("<type> <size>\x00<content>")`。
- 目录排序：Tree 对象序列化时，目录排序遵循 Git 规则（目录名后虚拟追加 `/` 比较）。
- 差分算法：统一使用 Myers O(ND) 差分算法，Hunk 头部格式严格对齐 Git（单行省略 `,1`，空文件起始行置 0）。
- 忽略规则优先级：已跟踪文件永远不受 `.gitignore` 影响；未跟踪文件严格按 `.gitignore` 过滤，并在 porcelain 输出中最后按路径排序输出。
- 索引兼容：Index v4 采用 7-bit varint 路径前缀压缩，序列化后与 Git 2.39+ 双向兼容。
- 三路合并与冲突表示：严格使用三方共同祖先（LCA）基线，当两端产生差异且无法平滑重叠时，向暂存区写入 stage 1 (base)、stage 2 (ours/HEAD)、stage 3 (theirs/remote)，并在工作区生成符合 Git 规范的 `<<<<<<<`、`=======`、`>>>>>>>` 冲突标记。
- 冲突标记解决判定：当执行 `gogit add <path>` 写入 stage 0 索引条目时，原子性清除该路径对应的全部 stage 1/2/3 冲突条目，标记该文件冲突已解决；`gogit commit` 严防未解决的高 stage 冲突落盘。
- 历史回放机制：`cherry-pick` 与 `revert` 分别建立 `CHERRY_PICK_HEAD` 与 `REVERT_HEAD`，底层复用同一套三路树级合并逻辑；`rebase` 采用独立 `.git/rebase-apply` 运行目录与 todo list 状态机。

