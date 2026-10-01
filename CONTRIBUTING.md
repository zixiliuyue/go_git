# 贡献指南 (Contributing Guide)

感谢你关注并有意为 `gogit` 做出贡献！本项目致力于打造一个**完全基于 Go 标准库、与 Git 2.39+ 严格双向兼容**的高性能版本控制系统。

---

## 1. 行为准则 (Code of Conduct)

参与本项目的所有贡献者均需遵守我们的 [行为准则](CODE_OF_CONDUCT.md)。请尊重所有参与者，保持开放、包容和友善的沟通氛围。

---

## 2. 核心架构与贡献原则

1. **零第三方 Git 依赖**：本项目禁止引入任何第三方 Git 库（如 `go-git`, `libgit2` 等），所有对象编码、增量压缩、传输协议、索引结构等必须基于 Go 标准库自行实现。
2. **字节级兼容**：针对对象（Blob, Tree, Commit, Tag）、索引（Index v2/v3/v4）、打包（Pack v2, Idx v2, MIDX, Bitmap）等数据结构，必须与官方原生 Git 保持 100% 字节布局或语义兼容。
3. **原生 Git 严格校验**：任何新增或修改的功能，必须经由原生 `git fsck --strict` 校验通过，确保零警告、零错误。
4. **测试覆盖**：
   - 每一个内部模块与命令行功能均需配备对应的 `_test.go` 测试用例。
   - 提交 PR 前必须在本地运行并通过 `make test` 与 `make verify`。
5. **代码规范与注释**：
   - 遵循标准 Go 编码规范（`gofmt` / `go vet`）。
   - 关键算法（Myers 差分、LCA 三路合并、LibXDiff 增量编解码、EWAH 位图等）与边界处理必须附有详细的中文说明注释，阐明“为什么这样做”。

---

## 3. 开发环境配置与构建

### 环境要求
- Go 1.23+（推荐 Go 1.24+）
- Git 2.39+（用于本地自动化交叉对比测试）
- Make / Bash

### 快速构建与验证
```bash
# 1. 克隆仓库
git clone git@github.com:zixiliuyue/go_git.git
cd go_git

# 2. 编译二进制
make build

# 3. 运行全套单元测试
make test

# 4. 运行性能基准测试
make bench

# 5. 运行 1~13 端到端兼容性验证
make verify
```

---

## 4. 提交规范 (Commit Guidelines)

本项目严格遵循 [Conventional Commits](https://www.conventionalcommits.org/) 规范，建议提交信息格式如下：

```text
【类型】简短描述（不超过50字）

【详细说明】
* 背景：说明为什么要做这个改动
* 变更内容：
  * 修改点1
  * 修改点2
* 影响范围：
  * 涉及模块/服务
* 风险评估：
  * 潜在风险点与回滚方案

【测试说明】
* 测试方式与测试结果
```

常用类型说明：
- `feat`: 新增特性或 CLI 命令
- `fix`: 修复 bug 或兼容性缺陷
- `perf`: 性能优化
- `refactor`: 代码重构（不改变现有行为）
- `test`: 补全或改进测试用例
- `docs`: 文档变动

---

## 5. Pull Request 流程

1. Fork 本仓库并基于最新 `main` 分支创建特性分支（如 `feature/your-feature-name` 或 `fix/issue-description`）。
2. 在本地完成编码与测试，确保 `make test` 和 `make verify` 100% 通过。
3. 提交代码并推送到你的 Fork 仓库。
4. 在 GitHub 提出 Pull Request，并在描述中详述改动背景、测试结果与影响范围。
5. 等待 CI 检查与 Maintainer Code Review。
