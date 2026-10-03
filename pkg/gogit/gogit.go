// Package gogit 提供符合 Google 工程规范的通用纯 Go Git 客户端/服务端 SDK
package gogit

import (
	root "gogit"
)

// Repository 仓库句柄类型别名
type Repository = root.Repository

// Commit 提交对象类型别名
type Commit = root.Commit

// Reference 引用对象类型别名
type Reference = root.Reference

// Tag 标签对象类型别名
type Tag = root.Tag

// Status 仓库工作区状态类型别名
type Status = root.Status

// Signature 身份签名类型别名
type Signature = root.Signature

// VerificationResult 签名校验报告类型别名
type VerificationResult = root.VerificationResult

// InitOption 初始化选项类型别名
type InitOption = root.InitOption

// CommitOption 提交选项类型别名
type CommitOption = root.CommitOption

// TagOption 标签选项类型别名
type TagOption = root.TagOption

// CloneOption 克隆选项类型别名
type CloneOption = root.CloneOption

// ServerOption 服务端选项类型别名
type ServerOption = root.ServerOption

// 导出顶层核心公共函数
var (
	// Open 打开本地已存在的 Git 仓库
	Open = root.Open
	// Init 在本地初始化全新 Git 仓库
	Init = root.Init
	// Clone 从远端 URL 克隆仓库
	Clone = root.Clone
	// NewServer 构建轻量 Smart HTTP 服务端 Handler
	NewServer = root.NewServer

	// 配置选项构造器
	WithBare            = root.WithBare
	WithDefaultBranch   = root.WithDefaultBranch
	WithObjectFormat    = root.WithObjectFormat
	WithAuthor          = root.WithAuthor
	WithCommitter       = root.WithCommitter
	WithParents         = root.WithParents
	WithSign            = root.WithSign
	WithTagAnnotated    = root.WithTagAnnotated
	WithTagSign         = root.WithTagSign
	WithTagMessage      = root.WithTagMessage
	WithTagTarget       = root.WithTagTarget
	WithCloneBranch     = root.WithCloneBranch
	WithCloneBare       = root.WithCloneBare
	WithServerAllowPush = root.WithServerAllowPush
)
