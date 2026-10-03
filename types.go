package gogit

import (
	"time"
)

// Signature 表示 Git 提交者或作者的身份与时间戳
type Signature struct {
	Name  string
	Email string
	When  time.Time
}

// Commit 表示解析后的 Git 提交对象公开数据模型
type Commit struct {
	Hash      string
	TreeHash  string
	Parents   []string
	Author    Signature
	Committer Signature
	Message   string
	GPGSig    string
}

// Reference 表示 Git 引用（如分支、HEAD、标签引用）
type Reference struct {
	Name     string
	Hash     string
	Target   string
	IsSymref bool
}

// Tag 表示 Git 标签信息（支持轻量标签与附注/签名标签）
type Tag struct {
	Name       string
	Hash       string
	TargetHash string
	Tagger     Signature
	Message    string
	IsSigned   bool
}

// TreeEntry 描述树对象下的单个条目
type TreeEntry struct {
	Mode string
	Name string
	Hash string
}

// Tree 表示解析后的目录树对象
type Tree struct {
	Hash    string
	Entries []TreeEntry
}

// Status 描述仓库工作区与暂存区的状态
type Status struct {
	Branch    string
	IsClean   bool
	Staged    []string
	Modified  []string
	Untracked []string
	Deleted   []string
}

// VerificationResult 表示密码学数字签名验证的结论报告
type VerificationResult struct {
	Valid       bool
	Type        string // 签名协议: "ssh" 或 "gpg"
	Signer      string // 签名者身份
	KeyID       string // 密钥标识
	Fingerprint string // 公钥指纹
	Message     string // 诊断说明
}

// --- 配置项选项参数 ---

type initOptions struct {
	bare          bool
	defaultBranch string
	objectFormat  string
}

// InitOption 用于定制仓库初始化
type InitOption func(*initOptions)

// WithBare 指定是否创建裸仓库（Bare Repository）
func WithBare(bare bool) InitOption {
	return func(o *initOptions) {
		o.bare = bare
	}
}

// WithDefaultBranch 指定默认主分支名（如 main, master）
func WithDefaultBranch(branch string) InitOption {
	return func(o *initOptions) {
		o.defaultBranch = branch
	}
}

// WithObjectFormat 指定对象哈希算法（sha1 或 sha256）
func WithObjectFormat(format string) InitOption {
	return func(o *initOptions) {
		o.objectFormat = format
	}
}

type commitOptions struct {
	author    *Signature
	committer *Signature
	parents   []string
	sign      bool
}

// CommitOption 用于定制提交行为
type CommitOption func(*commitOptions)

// WithAuthor 指定本次提交的作者
func WithAuthor(sig Signature) CommitOption {
	return func(o *commitOptions) {
		o.author = &sig
	}
}

// WithCommitter 指定本次提交的提交者
func WithCommitter(sig Signature) CommitOption {
	return func(o *commitOptions) {
		o.committer = &sig
	}
}

// WithParents 显式指定父提交哈希列表
func WithParents(parents ...string) CommitOption {
	return func(o *commitOptions) {
		o.parents = parents
	}
}

// WithSign 指定是否对本次提交进行密码学数字签名
func WithSign(sign bool) CommitOption {
	return func(o *commitOptions) {
		o.sign = sign
	}
}

type tagOptions struct {
	annotated bool
	sign      bool
	message   string
	target    string
}

// TagOption 用于定制标签创建行为
type TagOption func(*tagOptions)

// WithTagAnnotated 指定创建附注标签
func WithTagAnnotated(annotated bool) TagOption {
	return func(o *tagOptions) {
		o.annotated = annotated
	}
}

// WithTagSign 指定对标签进行数字签名
func WithTagSign(sign bool) TagOption {
	return func(o *tagOptions) {
		o.sign = sign
		o.annotated = true
	}
}

// WithTagMessage 指定标签附注文案
func WithTagMessage(msg string) TagOption {
	return func(o *tagOptions) {
		o.message = msg
		o.annotated = true
	}
}

// WithTagTarget 指定标签指向的目标修订版本或对象哈希
func WithTagTarget(target string) TagOption {
	return func(o *tagOptions) {
		o.target = target
	}
}

type cloneOptions struct {
	branch string
	bare   bool
}

// CloneOption 用于定制克隆行为
type CloneOption func(*cloneOptions)

// WithCloneBranch 指定克隆时检出的远端分支
func WithCloneBranch(branch string) CloneOption {
	return func(o *cloneOptions) {
		o.branch = branch
	}
}

// WithCloneBare 指定克隆为裸仓库
func WithCloneBare(bare bool) CloneOption {
	return func(o *cloneOptions) {
		o.bare = bare
	}
}

type serverOptions struct {
	allowPush bool
}

// ServerOption 用于配置 Smart HTTP 服务端 Handler
type ServerOption func(*serverOptions)

// WithServerAllowPush 配置服务端是否允许接收 push 推送
func WithServerAllowPush(allow bool) ServerOption {
	return func(o *serverOptions) {
		o.allowPush = allow
	}
}
