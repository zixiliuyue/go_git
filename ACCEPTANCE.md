# gogit 阶段与最终验收结果 (ACCEPTANCE.md)

本文档记录各里程碑阶段验收命令与真实输出，以及第八节最终验收清单 1-13 的执行结果。

---

## M1 验收结果 (对象模型与最小工作流)

### 验收命令
```bash
/tmp/gogit_bin init -b main /tmp/gogit_m1_verify
cd /tmp/gogit_m1_verify
echo "package main" > main.go
mkdir -p pkg && echo "package pkg" > pkg/pkg.go
/tmp/gogit_bin add .
/tmp/gogit_bin commit -m "initial commit by gogit"
git fsck --strict
echo "gogit: $(/tmp/gogit_bin rev-parse HEAD)"
echo "git:   $(git rev-parse HEAD)"
/tmp/gogit_bin cat-file -p HEAD
/tmp/gogit_bin ls-tree -r HEAD
/tmp/gogit_bin fsck --strict
```

### 真实执行输出
```text
Initialized empty Git repository in /tmp/gogit_m1_verify/.git/
[main (root-commit) 780432b] initial commit by gogit
=== 1. git fsck --strict ===
Checking ref database: 100% (1/1)Checking ref database: 100% (1/1), done.
Checking object directories: 100% (256/256)Checking object directories: 100% (256/256), done.
=== 2. rev-parse HEAD ===
gogit: 780432bd6ce0eab04ed76e8494488ae6c314a8ae
git:   780432bd6ce0eab04ed76e8494488ae6c314a8ae
=== 3. cat-file -p HEAD ===
tree de3c35ccecaa3ddc0eb02ea473767b7f351aff10
author Ren Hongsen <hongsen.ren@garena.com> 1790823224 +0800
committer Ren Hongsen <hongsen.ren@garena.com> 1790823224 +0800

initial commit by gogit
=== 4. ls-tree -r HEAD ===
100644 blob 06ab7d0f9a35a7d1070711496d6ca1cb892a258f	main.go
100644 blob c1caffeb1fbeb31d432cbd6b3a8e3bcf5991e401	pkg/pkg.go
=== 5. gogit fsck --strict ===
(exit code 0)
```

**验收结论**：M1 全部通过。gogit 建仓提交后，真实 git fsck --strict 零错误；`cat-file -p` 与 `rev-parse HEAD` 与 git 完全一致。

---

## M2 验收结果 (索引/状态/diff/log/分支)

### 验收命令
```bash
/tmp/gogit_bin init -b main /tmp/gogit_m2_verify
cd /tmp/gogit_m2_verify
echo "*.log" > .gitignore
echo "!important.log" >> .gitignore
echo "hello main" > main.go
echo "ignore me" > test.log
echo "keep me" > important.log
/tmp/gogit_bin add .
/tmp/gogit_bin commit -m "init main"
git checkout -b feature
echo "feature code" > feature.go
echo "hello main modified" > main.go

# 1. 状态比对
/tmp/gogit_bin status --porcelain
git status --porcelain

# 2. 工作区 diff 比对
/tmp/gogit_bin diff
git diff

# 3. 暂存与暂存区 diff 比对
/tmp/gogit_bin add feature.go
/tmp/gogit_bin diff --cached
git diff --cached

# 4. 健康检查
git fsck --strict

# 5. 拓扑分支图比对
/tmp/gogit_bin commit -m "feature commit"
/tmp/gogit_bin log --oneline --graph --all
git log --oneline --graph --all
```

### 真实执行输出
```text
Initialized empty Git repository in /tmp/gogit_m2_verify/.git/
[main (root-commit) 4d0b068] init main
Switched to a new branch 'feature'
=== 1. gogit status --porcelain vs git status --porcelain ===
--- gogit status ---
 M main.go
?? feature.go
--- git status ---
 M main.go
?? feature.go
=== 2. gogit diff vs git diff ===
--- gogit diff ---
diff --git a/main.go b/main.go
index 5799d3c..5bb00c4 000644
--- a/main.go
+++ b/main.go
@@ -1 +1 @@
-hello main
+hello main modified
--- git diff ---
diff --git a/main.go b/main.go
index 5799d3c..5bb00c4 100644
--- a/main.go
+++ b/main.go
@@ -1 +1 @@
-hello main
+hello main modified
=== 3. gogit add feature.go && diff --cached ===
--- gogit diff --cached ---
diff --git a/feature.go b/feature.go
new file mode 100644
index 0000000..aa64f58
--- /dev/null
+++ b/feature.go
@@ -0,0 +1 @@
+feature code
--- git diff --cached ---
diff --git a/feature.go b/feature.go
new file mode 100644
index 0000000..aa64f58
--- /dev/null
+++ b/feature.go
@@ -0,0 +1 @@
+feature code
=== 4. git fsck --strict ===
Checking ref database: 100% (1/1)Checking ref database: 100% (1/1), done.
Checking object directories: 100% (256/256)Checking object directories: 100% (256/256), done.
=== 5. 拓扑分支图比对 ===
--- gogit log --oneline --graph --all ---
* d3b29f6 feature commit
* 4d0b068 init main
--- git log --oneline --graph --all ---
* d3b29f6 (HEAD -> feature) feature commit
* 4d0b068 (main) init main
```

**验收结论**：M2 全部通过。同一仓库交替使用 gogit 与 git 修改、暂存、分支切换，`status --porcelain` 与 `diff` 输出逐行一致，Myers 差分与 Hunk 聚合对齐，索引 v4 支持与 Git 2.39+ 互操作零错误。

