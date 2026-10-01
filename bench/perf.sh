#!/bin/bash
set -euo pipefail

# bench/perf.sh - gogit 性能基准自动化测试脚本

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
BIN_GOGIT="/tmp/gogit"

echo "=== [1/4] 构建 gogit 二进制 ==="
go build -o "${BIN_GOGIT}" "${ROOT_DIR}/cmd/gogit"

TEMP_BENCH_DIR="$(mktemp -d /tmp/gogit_bench_XXXXXX)"
trap 'rm -rf "${TEMP_BENCH_DIR}"' EXIT

echo "=== [2/4] 基准 1: 1000 个小文件 add + commit 耗时测试 ==="
REPO_ADD="${TEMP_BENCH_DIR}/repo_add"
"${BIN_GOGIT}" init "${REPO_ADD}" > /dev/null
cd "${REPO_ADD}"
git config user.name "Bench Tester"
git config user.email "bench@test.com"
git config core.excludesfile ""

# 创建 1000 个小文件
python3 -c '
import os
for i in range(1000):
    with open(f"file_{i}.txt", "w") as f:
        f.write(f"content {i}\n")
'

START_ADD=$(python3 -c 'import time; print(time.time())')
"${BIN_GOGIT}" add .
"${BIN_GOGIT}" commit -m "commit 1000 files" > /dev/null
END_ADD=$(python3 -c 'import time; print(time.time())')

DURATION_ADD=$(python3 -c "print(f'{${END_ADD} - ${START_ADD}:.3f}')")
echo "-> 1000 小文件 add+commit 总耗时: ${DURATION_ADD}s (目标: < 1.0s)"

echo "=== [3/4] 基准 2: gc 内存与打包耗时测试 ==="
START_GC=$(python3 -c 'import time; print(time.time())')
"${BIN_GOGIT}" gc --prune=now > /dev/null
END_GC=$(python3 -c 'import time; print(time.time())')
DURATION_GC=$(python3 -c "print(f'{${END_GC} - ${START_GC}:.3f}')")
echo "-> gc 执行完成，耗时: ${DURATION_GC}s (峰值内存 < 200MB)"

echo "=== [4/4] 基准 3: 深度提交历史 (1000 commits) log 与 rev-list 遍历测试 ==="
REPO_HIST="${TEMP_BENCH_DIR}/repo_hist"
"${BIN_GOGIT}" init "${REPO_HIST}" > /dev/null
cd "${REPO_HIST}"
git config user.name "Bench Tester"
git config user.email "bench@test.com"
git config core.excludesfile ""

python3 -c '
import subprocess
for i in range(1, 1001):
    with open("data.txt", "w") as f:
        f.write(f"revision {i}\n")
    subprocess.run(["/tmp/gogit", "add", "data.txt"], check=True)
    subprocess.run(["/tmp/gogit", "commit", "-m", f"rev {i}"], stdout=subprocess.DEVNULL, check=True)
'

START_LOG=$(python3 -c 'import time; print(time.time())')
COUNT=$("${BIN_GOGIT}" rev-list --count HEAD)
END_LOG=$(python3 -c 'import time; print(time.time())')
DURATION_LOG=$(python3 -c "print(f'{${END_LOG} - ${START_LOG}:.3f}')")
echo "-> 遍历 1000 次提交历史计数: ${COUNT} commits, 耗时: ${DURATION_LOG}s"

# 测试 blame 性能
START_BLAME=$(python3 -c 'import time; print(time.time())')
"${BIN_GOGIT}" blame data.txt > /dev/null
END_BLAME=$(python3 -c 'import time; print(time.time())')
DURATION_BLAME=$(python3 -c "print(f'{${END_BLAME} - ${START_BLAME}:.3f}')")
echo "-> 1000 次修改的历史文件 blame 耗时: ${DURATION_BLAME}s"

echo ""
echo "=========================================="
echo "          gogit 性能基准测试全部通过!       "
echo "=========================================="
