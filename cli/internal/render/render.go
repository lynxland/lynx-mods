// Package render 把模板目录树渲染到目标目录：全部文件过 text/template
// （数据：AppName/ModulePath/EnvPrefix），已存在文件默认跳过（shadcn 语义）。
package render

import (
	"fmt"
	"go/format"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"text/template"
)

// Data 是渲染变量。
type Data struct {
	AppName    string
	ModulePath string
	EnvPrefix  string
	// DBName 是数据库/用户名形态的应用名（中划线转下划线）——
	// Postgres 标识符不允许中划线。
	DBName string
	// DefaultDSN 是 configs/config.yaml 缺省 DSN 的同款值——cmd/migrate
	// 的旗标缺省用它兜底（与配置文件出自同一数据源，不会漂移）。
	DefaultDSN string
}

// NewData 从应用名与 module path 构造渲染变量。
func NewData(appName, modulePath string) Data {
	dbName := strings.ReplaceAll(appName, "-", "_")
	return Data{
		AppName:    appName,
		ModulePath: modulePath,
		EnvPrefix:  strings.ToUpper(strings.ReplaceAll(appName, "-", "_")),
		DBName:     dbName,
		DefaultDSN: fmt.Sprintf("postgres://%s:%s@127.0.0.1:5432/%s?sslmode=disable", dbName, dbName, dbName),
	}
}

// Options 控制渲染行为。
type Options struct {
	Overwrite bool
	DryRun    bool
	// Template 启用 text/template 渲染。scaffold 骨架是模板（true）；
	// 模块安装是普通源码（false）——必须逐字节复制后仅做 Rewrite，
	// 不得过模板引擎（Go 复合字面量的 `{{` 会撞模板语法）。
	Template bool
	// Skip 匹配的相对路径跳过（如模块目录里的 module.json）。
	Skip func(rel string) bool
	// Rewrite 在模板执行（或原样复制）后对内容做最终变换（如安装时把
	// 模块规范 module path 改写为应用 module path）。rel 用于按后缀过滤。
	Rewrite func(rel string, content []byte) []byte
	// Relocate 把源相对路径映射为目标相对路径（nil = 原样）。模块安装
	// 用它做布局重定位（domain/<m>/** → modules/<m>/domain/** 等）。
	// Skip/Rewrite 收到源路径；Created/Skipped 与写盘用目标路径——
	// 产物路径空间（manifest Files/基线/update 合并）与落盘一致。
	Relocate func(rel string) (string, error)
	// Report 打印渲染动作（默认 fmt.Printf）。
	Report func(format string, args ...any)
}

// Result 汇总一次渲染的动作。
type Result struct {
	Created []string
	Skipped []string
}

// Tree 渲染 srcDir 整棵树到 dstDir。
func Tree(srcDir, dstDir string, data Data, opts Options) (*Result, error) {
	report := opts.Report
	if report == nil {
		report = func(string, ...any) {}
	}
	res := &Result{}
	err := filepath.WalkDir(srcDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(srcDir, path)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		if opts.Skip != nil && opts.Skip(rel) {
			return nil
		}
		targetRel := rel
		if opts.Relocate != nil {
			mapped, err := opts.Relocate(rel)
			if err != nil {
				return fmt.Errorf("relocate %s: %w", rel, err)
			}
			targetRel = mapped
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		var content []byte
		if opts.Template {
			tmpl, err := template.New(rel).Parse(string(raw))
			if err != nil {
				return fmt.Errorf("parse template %s: %w", rel, err)
			}
			var buf strings.Builder
			if err := tmpl.Execute(&buf, data); err != nil {
				return fmt.Errorf("execute template %s: %w", rel, err)
			}
			content = []byte(buf.String())
		} else {
			content = raw
		}
		if opts.Rewrite != nil {
			content = opts.Rewrite(rel, content)
		}
		if strings.HasSuffix(rel, ".go") {
			if formatted, err := format.Source(content); err == nil {
				content = formatted
			} else {
				return fmt.Errorf("gofmt %s: %w", rel, err)
			}
		}
		target := filepath.Join(dstDir, filepath.FromSlash(targetRel))
		if _, statErr := os.Stat(target); statErr == nil && !opts.Overwrite {
			res.Skipped = append(res.Skipped, targetRel)
			report("skip   %s (exists, -o to overwrite)", targetRel)
			return nil
		}
		if opts.DryRun {
			res.Created = append(res.Created, targetRel)
			report("dryrun %s", targetRel)
			return nil
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(target, content, 0o644); err != nil {
			return err
		}
		res.Created = append(res.Created, targetRel)
		report("write  %s", targetRel)
		return nil
	})
	return res, err
}
