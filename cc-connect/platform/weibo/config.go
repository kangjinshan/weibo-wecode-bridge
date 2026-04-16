package weibo

import (
	"fmt"

	"github.com/spf13/viper"
)

const (
	PlatformType = "weibo"
)

// Config 微博平台配置
type Config struct {
	AppID          string `mapstructure:"app_id"`
	AppSecret string `mapstructure:"app_secret"`
	TokenURL       string `mapstructure:"token_url"`
	WSURL          string `mapstructure:"ws_url"`
}

// ParseConfig 从 viper 解析配置
func ParseConfig(v *viper.Viper) (*Config, error) {
	cfg := &Config{}

	if err := v.Unmarshal(cfg); err != nil {
		return nil, fmt.Errorf("unmarshal weibo config: %w", err)
	}

	if cfg.AppID == "" {
		return nil, fmt.Errorf("weibo app_id is required")
	}

	if cfg.AppSecret == "" {
		return nil, fmt.Errorf("weibo app_secret is required")
	}

	// 设置默认值
	if cfg.TokenURL == "" {
		cfg.TokenURL = DefaultTokenURL
	}

	if cfg.WSURL == "" {
		cfg.WSURL = DefaultWSURL
	}

	return cfg, nil
}

// Validate 验证配置
func (c *Config) Validate() error {
	if c.AppID == "" {
		return fmt.Errorf("app_id is required")
	}

	if c.AppSecret == "" {
		return fmt.Errorf("app_secret is required")
	}

	return nil
}
