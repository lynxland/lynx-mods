// Package migrations 内嵌本模块的迁移链（独立 goose 版本表，契约④）。
package migrations

import "embed"

// FS 是模块迁移目录：goosekit.Up/Down 消费。
//
//go:embed *.sql
var FS embed.FS
