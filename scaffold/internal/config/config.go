// Package config 定义应用基础层的配置段（server/data）。模块的配置段各自
// 治理（契约③），不在此集中——本包只覆盖骨架自己需要的部分。
package config

import (
	"fmt"
	"strings"

	"github.com/lynxland/lynx"
	"github.com/spf13/pflag"
)

// Config 是应用基础层配置。
type Config struct {
	Server ServerConfig
	Data   DataConfig
}

// ServerConfig 是服务监听配置。
type ServerConfig struct {
	GRPCAddr   string `mapstructure:"grpc_addr"`
	HTTPAddr   string `mapstructure:"http_addr"`
	// CORS 是浏览器跨源配置——SaaS 前端（浏览器直调 API）必配，nil=不
	// 启用（非浏览器客户端无感）。AllowedOrigins 精确匹配（不支持
	// 通配符 *——凭据场景下通配是被浏览器拒绝的）。
	CORS *CORSConfig `mapstructure:"cors"`
	// AccessLog 开请求访问日志（默认 false——审计模块已记录全部 RPC，
	// 访问日志是流量画像的补充）。
	AccessLog bool `mapstructure:"access_log"`
}

// CORSConfig 是跨源资源共享配置。
type CORSConfig struct {
	AllowedOrigins []string `mapstructure:"allowed_origins"`
}

// DataConfig 是数据层配置。
type DataConfig struct {
	Database DatabaseConfig `mapstructure:"database"`
}

// DatabaseConfig 是 Postgres 连接配置。
type DatabaseConfig struct {
	Source       string `mapstructure:"source"`
	MaxOpenConns int    `mapstructure:"max_open_conns"`
}

// Load 从 lynx.Config 读取基础层配置（带默认值）。
func Load(c lynx.Config) (*Config, error) {
	cfg := &Config{
		Server: ServerConfig{GRPCAddr: ":9060", HTTPAddr: ":8080"},
		Data:   DataConfig{Database: DatabaseConfig{MaxOpenConns: 8}},
	}
	if err := c.UnmarshalKey("server", &cfg.Server, lynx.WithErrorUnused()); err != nil {
		return nil, fmt.Errorf("config server: %w", err)
	}
	if err := c.UnmarshalKey("data", &cfg.Data, lynx.WithErrorUnused()); err != nil {
		return nil, fmt.Errorf("config data: %w", err)
	}
	if cfg.Data.Database.Source == "" {
		return nil, fmt.Errorf("config data.database.source is required")
	}
	return cfg, nil
}

// BindConfig 是 lynx.WithBindConfigFunc 的绑定函数：默认文件查找 +
// 环境变量前缀 {{.EnvPrefix}}（点/横线 → 下划线，如
// {{.EnvPrefix}}_DATA_DATABASE_SOURCE）。
func BindConfig(f *pflag.FlagSet, c lynx.ConfigSource) error {
	if err := lynx.DefaultBindConfigFunc(f, c); err != nil {
		return err
	}
	// 骨架的配置文件在 configs/ 下；DefaultBindConfigFunc 只加 "."，必须
	// 显式补上，否则 configs/config.yaml 静默不被发现（应用跑在默认值+
	// 环境变量上——恰好与配置片段同值时会完全掩盖该缺陷）。
	if cf, _ := f.GetString("config"); cf == "" {
		if cd, _ := f.GetString("config-dir"); cd == "" {
			c.AddSearchPath("configs")
		}
	}
	c.SetEnvPrefix("{{.EnvPrefix}}")
	c.SetEnvKeyReplacer(strings.NewReplacer(".", "_", "-", "_"))
	c.AutomaticEnv()
	return nil
}
