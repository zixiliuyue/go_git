#!/usr/bin/env bash
# scripts/generate_provenance.sh - 生成符合 SLSA v1.0 与 in-toto 格式的构建产物出处元数据
# 用于本地或受限 CI 环境下的防篡改供应链审计

set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
OUTPUT_DIR="${ROOT_DIR}/build"
PROV_FILE="${OUTPUT_DIR}/provenance.slsa.json"
CHECKSUM_FILE="${OUTPUT_DIR}/SHA256SUMS"

mkdir -p "${OUTPUT_DIR}"

BIN_FILE="${ROOT_DIR}/bin/gogit"
if [ ! -f "${BIN_FILE}" ]; then
    echo "未找到二进制文件 ${BIN_FILE}，请先执行 make build"
    exit 1
fi

# 计算产物 SHA256 校验和
if command -v sha256sum >/dev/null 2>&1; then
    BIN_SHA256=$(sha256sum "${BIN_FILE}" | awk '{print $1}')
elif command -v shasum >/dev/null 2>&1; then
    BIN_SHA256=$(shasum -a 256 "${BIN_FILE}" | awk '{print $1}')
fi

# 写入标准 SHA256SUMS 校验清单
echo "${BIN_SHA256}  bin/gogit" > "${CHECKSUM_FILE}"

GIT_COMMIT="$(git rev-parse HEAD 2>/dev/null || echo "unknown")"
BUILD_TIME="$(date -u +"%Y-%m-%dT%H:%M:%SZ")"
GO_VERSION="$(go version 2>/dev/null || echo "unknown")"

cat <<EOF > "${PROV_FILE}"
{
  "_type": "https://in-toto.io/Statement/v1",
  "subject": [
    {
      "name": "bin/gogit",
      "digest": {
        "sha256": "${BIN_SHA256}"
      }
    }
  ],
  "predicateType": "https://slsa.dev/provenance/v1",
  "predicate": {
    "buildDefinition": {
      "buildType": "https://slsa.dev/projects/gogit/pure-go-build/v1",
      "externalParameters": {
        "source": {
          "uri": "git+https://github.com/google-engineering/go_git",
          "digest": {
            "gitCommit": "${GIT_COMMIT}"
          }
        },
        "entryPoint": "cmd/gogit"
      },
      "internalParameters": {
        "compiler": "${GO_VERSION}",
        "cgoEnabled": "0",
        "flags": ["-trimpath"]
      }
    },
    "runDetails": {
      "builder": {
        "id": "https://github.com/google-engineering/gogit/.github/workflows/slsa.yml"
      },
      "metadata": {
        "invocationId": "build-$(date +%s)",
        "startedOn": "${BUILD_TIME}",
        "finishedOn": "${BUILD_TIME}"
      }
    }
  }
}
EOF

echo "✓ 校验清单生成成功: ${CHECKSUM_FILE}"
echo "✓ SLSA v1.0 Provenance 出处证明生成成功: ${PROV_FILE}"
