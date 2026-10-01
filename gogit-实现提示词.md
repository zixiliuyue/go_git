# 用 Go 从零实现一个与 Git 兼容的版本控制系统（命令名：gogit）

> 这是一次**长时间、一次性完成**的编码任务：所有里程碑连续执行，中途不停顿、不征求确认，
> 直到全部功能实现且最终验收全部通过才交付。

## 一、任务目标

从零实现一个与真实 Git（2.x 系，以 git 2.39+ 为基准）在**数据格式、CLI 语义、网络协议、性能量级**四方面兼容的版本控制系统 gogit。
不允许依赖任何现成 Git 实现库。

最终验收：gogit 与真实 git 可在同一仓库上交替操作、可互相 clone/fetch/push，数据无损、`git fsck --strict` 零错误；
**协议 v2 全能力可用**；**git.git 全量仓库（完整历史）可正常 clone/fetch/log/blame，性能与真实 git 同数量级**。

## 二、执行模式要求（长任务 · 一次完成）

1. **一次性完成**：按 M1→M5 连续推进，不得中途停下来等待确认、不得在每个阶段结束后向用户汇报后再继续。只有提示词明确要求提问时才提问（见第 3 条）。
2. **全程任务清单**：开工即建立 TODO 清单（按里程碑与模块拆解），每完成一项立即更新状态；任何时刻都能说出"当前进度、剩余工作、下一步"。
3. **自行解决一切可解问题**：编译错误、格式细节、算法选择、测试失败等一律自行修复后继续；仅当遇到**必须外部输入**的阻塞（如需要真实账号凭证、访问受限资源、需要用户拍板的不可逆外部操作）才提问。
4. **上下文与断点续跑**：在仓库内维护 `PROGRESS.md`，持续记录：已完成模块、关键设计决策、各阶段验收命令与输出、已知问题。若上下文被截断或任务被中断，优先读 PROGRESS.md 与 TODO 续跑，不重复已完成的实现。
5. **源码自管**：gogit 源码用**真实 git** 管理，每个里程碑完成时提交一次并打 tag（M1…M5），提交信息含该里程碑验收结果摘要。
6. **预期耗时**：本任务规模大（完整 Git 实现），预计持续数小时以上。按模块独立开发、先写接口再实现、核心算法配单测，避免返工。
7. **每阶段验收立即执行**：每个里程碑结束立即运行该阶段的验收脚本并把真实输出追加到 `ACCEPTANCE.md`，不得跳过或后补。

## 三、硬性约束

1. 语言：Go（≥1.21），标准库优先（compress/zlib、crypto/sha1、net/http、os/exec、bufio、sync），单二进制，不用 cgo。
2. 禁止使用 go-git / libgit2 / dulwich 等实现库；**仅允许在测试代码中引入真实 git 作为"预言机（oracle）"做对比**，业务代码必须独立实现。
3. 所有落盘格式（loose object、pack、idx、bitmap、multi-pack-index、index、refs、packed-refs、reflog、shallow、replace、config）逐字节兼容；对象 SHA-1 必须与真实 git 对相同内容算出的值完全一致。
4. CLI：命令名与常用参数和 git 对齐；机器可解析输出（--porcelain、--format、--name-only、--numstat、rev-parse）必须与 git 一致；退出码语义一致（0 成功 / 1 一般错误 / 128 致命错误）。
5. 分阶段推进但**不停顿**：每个阶段仍有独立验收门，验收通过后立即进入下一阶段，最终一次性交付。

## 四、功能范围（全部必须实现，不允许声明"不支持"）

### P0 核心

- 对象模型：blob / tree / commit / tag(annotated)；loose 对象读写（头 `<type> <len>\0` + 内容，zlib 压缩）；对象哈希与 git 一致
- 索引 index v2/v3/v4：add / rm / mv / update-index / status（staged/unstaged/untracked 判定与 git 一致，含 stat 缓存判定）；**split index（link 扩展）**
- refs 与 HEAD：refs/heads、refs/tags、refs/remotes、HEAD symref、**detached HEAD**、packed-refs（含 peeled `^` 行）、reflog（@{n}、@{upstream}、@{push}）
- config：INI 格式（含 **include/includeIf**）、user.name/email、remote、core.*、protocol.*、credential.*
- 工作区：init / add / rm / mv / status / commit（author/committer 及 GIT_AUTHOR_* 等全套环境变量）/ restore / clean；**.gitignore 完整语义（pattern、`!` 取反、目录限定、锚定、双星）**
- 对象浏览：log（--oneline/--graph/--all/-p/--format/--since/--author/--grep 等修订与过滤）、show、cat-file、ls-tree、rev-parse（SHA 缩写、`^`/`~` 语法、@{n}、HEAD、范围 A..B / A...B）
- diff：worktree↔index↔HEAD 两两比较；--stat/--cached/--name-status/--numstat；rename/copy 检测（-M/-C）；word-diff、--diff-filter
- 合并：merge-base（递归 LCA、--independent/--octopus）、merge（fast-forward + 真实三路合并，冲突标记与 git 逐字一致）、branch / checkout / switch
- pack：pack v2 + idx v2 读写；REF_DELTA / OFS_DELTA 编解码；thin pack 修复（fetch 收到缺 base 的 pack 时补 base）；**pack bitmap（.bitmap v1，含 MIDX bitmap）**；**multi-pack-index（MIDX 读写与查找）**
- 传输与协议：
  - smart HTTP **v1 与 v2**：`info/refs?service=`（v1）/ `info/refs`（v2）、git-upload-pack/receive-pack、pkt-line、side-band、want/have/done 协商（v1）、**ls-refs / fetch v2 / object-info / capability 协商 / server-options（v2）**
  - push：v1 完整；**v2 push（如基准 git 支持）**，至少保证 v1 与 git 完全互操作
  - SSH：os/exec 调本地 ssh 执行 `git-upload-pack`/`git-receive-pack`（不自己实现 SSH 协议）
  - git:// daemon（9418，v1/v2 协商）
  - **dumb HTTP**（loose 对象 + info/packs 方式）读取兼容
- 存储维护：gc（repack -ad、prune、pack-refs、reflog expire 完整语义）、fsck（--strict/--full）、verify-pack、count-objects、garbage 清理

### P1 常用功能

- rebase（含 --onto、--interactive、--autosquash、--rebase-merges 基础）、cherry-pick、revert
- stash（push/list/show/pop/apply/drop/branch）、tag（-a；-s 签名生成与验证）、describe（--tags/--contains）
- submodule（clone --recursive、submodule update --init --recursive、add/status/foreach）、worktree（add/list/lock/move/remove/repair）
- archive（tar/tgz/zip、--format、--prefix、pathspec 过滤）、bundle（v2/v3 读写）、blame（**-L 行范围、-C/-M 移动复制检测**）、bisect（含 bisect run）、notes（tree 结构、add/list/show/merge）、rerere（完整）
- .mailmap、--no-optional-locks、advice 提示、color.ui 基础

### P2 高级功能（同样全部实现）

- shallow clone（--depth/--shallow-since/--shallow-exclude、fetch --unshallow、shallow 文件格式）
- partial clone（--filter=blob:none / tree:0 / sparse:oid=…，promisor remote、按需 fetch 缺失对象）
- replace refs（git replace --graft/--edit 等，读写 refs/replace/* 与 replace 对象）
- 复杂 refspec（`+` 强制、多 refspec、`refs/heads/*:refs/remotes/origin/*` glob、refspec 解析与匹配）
- credential helper 对接（内置 store/cache，以及外部 helper 的 stdout 协议 `get/store/erase`）
- .gitattributes 完整语义（attr 定义、eol/ident/whitespace、`merge=ours`/`merge=union` 等 attribute 驱动 merge/diff、filter 驱动 clean/smudge）
- hooks 全套标准钩子执行（applypatch-msg/commit-msg/post-commit/post-receive/pre-commit/pre-push/pre-rebase/update 等，退出码与阻断语义与 git 一致）
- sparse-checkout（cone 模式与完整 pattern 模式）、split index 完整、index v4
- protocol v2 全能力 + fetch 并行请求（--negotiate-only 等协商扩展）
- 其余 Git 2.39 功能面中的标准命令：fsck、prune、repack、pack-objects、index-pack、receive-pack、upload-pack、rev-list、rev-parse、verify-commit/tag、show-ref、for-each-ref、symbolic-ref、update-ref、count-objects、grep（基础正则）、init --bare/--shared、clone --bare/--mirror/--no-checkout 等

## 五、关键兼容性要点（动手前逐条核对）

- loose object：`<type> <size>\0<content>` 整体 zlib 后落盘，路径 `.git/objects/xx/yyyy…`
- tree entry：`<八进制 mode> <name>\0<20B sha>`；mode 支持 100644 / 100755 / 120000（symlink 内容=目标路径）/ 160000（gitlink）；条目按 name 字节序排序（目录名视作带尾部 `/`）
- commit：`tree <sha>`、`parent <sha>`*、`author <name> <email> <ts> <tz>`、`committer …`、空行、message（保留原换行）；gpgsig 头（tag/commit 签名时）
- index：签名 `DIRC`、version 2/3/4、定长 entry 头 + 变长 name（v4 用前缀压缩）、扩展区（TREE 缓存、link=split index、EOIE）
- pack v2：`PACK` 签名 + version 2 + 对象数；entry 头可变长 size（MSB 连续位）；OFS_DELTA 负偏移可变长编码；**结尾 1 个 20B pack checksum**
- idx v2：magic `\377tOc` + version 2 + 256 项 fanout + SHA 表 + CRC32 表 + offset 表（大偏移 8 字节扩展）+ 结尾 pack checksum 与 idx checksum 两个 20B
- bitmap v1：`BITM` 签名、version 1、commit 索引（基于 idx fanout 的顺序）、trailer（haves）、ewah 压缩位图
- multi-pack-index：`MIDX` 签名、version 1、对象偏移表、fanout、可选 bitmap
- refs：loose 文件；packed-refs 带 `^` peeled 行；HEAD 内容为 `ref: refs/heads/x`
- 传输：pkt-line（4 位十六进制长度 + 内容，`0000` flush，`0001` delim）；side-band channel 1=pack / 2=progress / 3=error
  - v1 协商：`want <sha> <caps>`、`have`、`done`，服务端 ACK/NAK
  - **v2 协商：`command=ls-refs` / `command=fetch` / `command=object-info`，capability 行（agent/symref/want-ref/shallow 等），`0001` 分隔**
- 换行与编码：默认 autocrlf=false；提交信息按 UTF-8 原样存储；二进制文件不进入 diff 文本处理

## 六、架构要求

- 模块化包结构（可微调，必须分层）：
  - `internal/object`（对象模型与 loose 读写）
  - `internal/pack`（pack/idx/bitmap/MIDX/索引-pack）
  - `internal/index`（索引 v2/v3/v4、split index）
  - `internal/refs`（refs/packed-refs/reflog/replace）
  - `internal/repo`（config、仓库状态、shallow）
  - `internal/worktree`（工作区、ignore、attributes）
  - `internal/diff`（xdiff 风格算法）
  - `internal/merge`（三路合并、LCA、rename 检测、rerere）
  - `internal/rev`（rev-parse、rev-list、log 图遍历）
  - `internal/transport`（http v1/v2、daemon、ssh 子进程、credential）
  - `internal/cli`（命令注册、参数解析、退出码、帮助）
- 锁与原子性：ref 更新、index 写入采用 `.lock` + 原子 rename，语义与 git 一致
- 核心算法（diff、delta、LCA、pack 编码、图遍历）独立成包并配专门测试；禁止单文件几千行
- 性能设计（对应 git.git 级性能目标）：
  - pack 生成：默认窗口/深度与 git 相当（window=10、depth=50），delta 搜索可用并发，写 pack 全程缓冲避免逐对象 syscall
  - log/rev-list：图遍历用拓扑排序与生成集（generation number）加速，禁止 O(n²) 逐提交扫描
  - 大对象：diff/blame 对大文件（≥1 万行）不整文件反复复制，用流式与增量算法
  - 索引：pack 查找走 fanout 二分，禁止线性扫描

## 七、分阶段里程碑与验收门（连续执行，不停顿）

- **M1 对象模型与最小工作流**：init/add/commit/cat-file/ls-tree/hash-object/log(基础)/fsck
  → 验收：gogit 建仓提交后，真实 git `fsck --strict` 零错误；`cat-file -p` 输出与 git 一致；`rev-parse HEAD` 与 git 一致
- **M2 索引/状态/diff/log/分支**：status/restore/diff/log --graph/branch/checkout/switch + ignore/attributes
  → 验收：同一仓库交替用 gogit 与 git 修改，`status --porcelain` 与 diff 输出逐行一致
- **M3 合并与历史操作**：merge/merge-base/rebase/cherry-pick/revert/stash/tag/describe/blame/bisect
  → 验收：无冲突/冲突/rename/criss-cross 四类场景，合并结果与冲突标记与 git 逐字一致
- **M4 传输与克隆（v1+v2）**：remote/fetch/pull/push/smart HTTP v1&v2/SSH(子进程)/daemon/dumb HTTP/credential
  → 验收：gogit clone 本地 bare 与 GitHub 仓库成功（v1 与 v2 各一次），对象集与 git clone 完全一致；gogit push 到 bare 后 git clone 成功；git push 到 gogit 端成功
- **M5 收尾与总验收**：gc/repack/bitmap/MIDX/verify-pack/性能与模糊测试/文档
  → 验收：跑完第八节全部验收清单；`make verify` 一键通过

## 八、最终验收清单（交付时逐条可运行，全部通过才算完成）

| # | 验收项 | 验证方式 | 通过标准 |
|---|--------|----------|----------|
| 1 | 哈希一致 | gogit vs `git hash-object` 同一文件 | SHA-1 相等 |
| 2 | 仓库互读 | gogit 建仓提交后 `git fsck --strict` | 0 错误 |
| 3 | 仓库互写 | git 建仓 → gogit 修改 → git fsck | 0 错误 |
| 4 | clone 兼容 | gogit clone 与 git clone 同一源（v1 与 v2 各一次） | 对象集一致、fsck 通过 |
| 5 | push 兼容 | gogit push → git clone 回读；git push → gogit 接收 | 内容一致、fsck 通过 |
| 6 | 合并兼容 | 四类合并场景对比 | 结果逐字一致 |
| 7 | 协议兼容 | 对 GitHub smart HTTP v1/v2 完成 fetch/push；git:// daemon 互通 | 成功 |
| 8 | 高级能力 | shallow clone、--filter=blob:none、replace、sparse-checkout、credential、hooks、attributes 各构造场景 | 行为与 git 一致 |
| 9 | **git.git 级性能** | 对 git.git 全量仓库（完整历史）：gogit clone --bare 本地镜像 vs git clone --bare | 总耗时 ≤ git 的 2 倍 |
| 10 | pack 质量 | 比较 gogit 与 git 生成的 pack | gogit pack 大小 ≤ git pack 的 1.5 倍 |
| 11 | 大历史操作 | gogit log --graph --all 全历史、blame 大文件（≥1 万行） | 完成耗时 ≤ git 的 3 倍；不超时、不 OOM |
| 12 | 健壮性 | pack/idx/index/bitmap/MIDX 损坏输入 fuzz | 不 panic、可报错、不落盘损坏数据 |
| 13 | 回归基线 | 以 git.git 为基线语料，每次改动后重跑 fsck + 哈希对比 | 恒为 0 错误 |

## 九、测试策略

- 单元：diff/delta/hash/LCA/pack 边界（空文件、大文件、二进制、UTF-8、CRLF、symlink、目录排序、树冲突）
- 集成：以真实 git 为 oracle 的对比测试（fixture 仓库 + 场景生成器，覆盖 v1/v2、shallow/partial/replace/sparse）
- 互操作：自动化脚本跑第八节清单 1–13
- 模糊：pack/idx/index/bitmap/MIDX 输入 fuzz，保证不 panic、不落盘损坏数据
- 性能：性能基准脚本（`bench/perf.sh`）固定跑 git.git 全量 clone/log/blame，记录耗时与 pack 大小，纳入验收
- 回归：git.git 为基线语料，每次改动后重跑 fsck + 哈希对比

## 十、交付物

- `README.md`：架构图、支持矩阵（格式版本、协议版本、命令覆盖）、快速开始、与 git 的差异说明
- `PROGRESS.md`：执行过程记录（模块完成情况、设计决策、验收输出、已知问题），支持断点续跑
- `ACCEPTANCE.md`：第八节清单 1–13 逐条的命令与真实输出（含耗时与 pack 大小数据）
- `bench/perf.sh`：性能基准脚本；`make verify` 一键跑完全部验收
- 每个里程碑的 git 提交与 tag（M1…M5）

## 十一、非目标（明确不做，其余 git 功能面均须实现）

- 不做 GUI / gitk / git gui 类图形界面
- 不做 git LFS、不做文件系统级 watch（--watchman 类）
- 不要求与 git 提示文案逐字符一致（错误信息可自行措辞），但机器可解析输出必须一致
- 不做 git svn（依赖外部 Subversion 服务器）与 filter-branch（可用 filter-repo 语义替代，非必须）
- 冷门边缘参数不强制逐一覆盖，但本表第四、五节列出的全部功能与格式必须完整实现
