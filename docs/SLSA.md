# Google SLSA 供应链安全与构建产物出处规范 (Level 3 Compliant)

本项目严格对齐 Google 主导的 **SLSA (Supply-chain Levels for Software Artifacts)** 3 级规范以及 CNCF 安全最佳实践，构建了不可篡改、防注入、端到端可验证的软件供应链体系。

---

## 1. 核心保障与安全级别定义

| SLSA 级别 | 要求标准 | gogit 实现机制 |
| :--- | :--- | :--- |
| **SLSA 1** | 构建过程脚本化，提供基础 Provenance | 自动化 Makefile、`scripts/generate_provenance.sh` 声明式构建 |
| **SLSA 2** | 使用托管构建服务，生成由构建平台签名的出处元数据 | 基于 GitHub-hosted runners 隔离环境构建，无外部直接磁盘写入 |
| **SLSA 3** | **防篡改与隔离构建环境**，出处证明不可由构建逻辑伪造 | 引入 `slsa-framework/slsa-github-generator` 独立可重用工作流，采用 Sigstore OIDC 无密钥（Keyless）签名机制生成不可篡改证明 |

---

## 2. 软件物料清单 (SPDX 2.3 SBOM)

每次构建均同步生成符合国际 ISO/IEC 5962:2021 (SPDX 2.3) 标准的 JSON 格式 SBOM 文件（`build/sbom.spdx.json`）：

- **编译器与工具链指纹**：记录 Go 编译器确切版本号、操作系统、芯片架构与构建时间。
- **纯标准库零外部依赖认证**：验证根包仅链接 Go 官方标准库，无未授权三方黑盒依赖包。
- **产物文件级数字摘要**：针对构建生成的每一个跨平台可执行二进制记录 SHA-256 校验和。

本地或离线生成 SBOM：
```bash
# 生成 build/sbom.spdx.json
./scripts/generate_sbom.sh
```

---

## 3. 构建产物出处证明 (in-toto Provenance Attestation)

在发布流程中，SLSA 3 级生成器在与构建任务物理隔离的容器中运行，提取待签名产物的 `SHA256SUMS`，通过 Sigstore Fulcio 申请短期证书，并记录至 Rekor 透明日志中，生成以 `.intoto.jsonl` 为扩展名的 Attestation 文件：

- 声明了源码真实仓库 URL 与不可伪造的 Git Commit SHA。
- 声明了构建环境触发的 Trigger Workflow 与外部参数。
- 包含不可篡改的加密数字签名，任何第三方修改二进制产物均会导致验签失败。

---

## 4. 离线与在线验证指南

### 4.1 本地完整性验证
```bash
# 验证构建产物哈希、出处证明与 SBOM 清单一致性
./scripts/verify_provenance.sh
```

### 4.2 使用 Google 官方 `slsa-verifier` 验证 Release 二进制
对于从 GitHub Release 下载的二进制产物与 `gogit.intoto.jsonl`，推荐使用 Google 官方 `slsa-verifier` 进行防伪溯源验证：

```bash
# 安装官方验证工具
go install github.com/slsa-framework/slsa-verifier/v2/cli/slsa-verifier@latest

# 验证二进制真实来自该官方仓库及特定标签
slsa-verifier verify-artifact ./gogit-linux-amd64 \
  --provenance-path ./gogit.intoto.jsonl \
  --source-uri github.com/google-engineering/go_git \
  --source-tag v1.0.0
```
验证通过后，终端输出 `PASSED: Verified SLSA provenance`，证明该二进制确由指定源码在安全的隔离云端流水线中构建而成，杜绝任何中间人篡改或恶意投毒。
