package main

import (
	"encoding/json"
	"fmt"
	"os"
)

// ============================ 配置文件 ============================
//
// 分两个文件，职责不同：
//
//   config/integration.json   接入配置：聊天室下发的凭证与参数。
//                             内容与后台「复制配置」完全一致，替换整个文件即可。
//
//   config/plugin_config.json 运行配置：本服务自身的部署参数（端口、公钥路径、回合时限）。
//
// 优先级：命令行参数 > 环境变量 > integration.json > plugin_config.json > 内置默认值

const (
	defaultConfigPath      = "config/plugin_config.json"
	defaultIntegrationPath = "config/integration.json"
)

// fileConfig 运行配置，只放本服务自身的部署参数
type fileConfig struct {
	Addr          string `json:"addr"`
	InstanceID    string `json:"instanceId"`
	PublicKeyPath string `json:"pubkey"`
	// TurnSeconds 每回合思考时限（秒），0 表示不计时
	TurnSeconds int `json:"turnSeconds"`

	// 兼容旧版：允许把接入字段继续写在 plugin_config.json 里。
	// 同时存在时以 integration.json 为准。
	Chatroom       string `json:"-"`
	RoomID         int    `json:"-"`
	IntegrationID  string `json:"-"`
	BridgeSecret   string `json:"-"`
	ExchangeSecret string `json:"-"`
	SecretVersion  int64  `json:"-"`
}

// legacyFileConfig 旧版单文件配置，用于平滑迁移
type legacyFileConfig struct {
	fileConfig
	Chatroom       string `json:"chatroom"`
	RoomID         int    `json:"roomId"`
	IntegrationID  string `json:"integrationId"`
	BridgeSecret   string `json:"bridgeSecret"`
	ExchangeSecret string `json:"exchangeSecret"`
	SecretVersion  int64  `json:"secretVersion"`
}

// loadFileConfig 读取运行配置。文件不存在视为正常情况。
func loadFileConfig(path string) (fileConfig, error) {
	var legacy legacyFileConfig
	raw, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return fileConfig{}, nil
		}
		return fileConfig{}, err
	}
	if len(raw) == 0 {
		return fileConfig{}, nil
	}
	if err = json.Unmarshal(raw, &legacy); err != nil {
		return fileConfig{}, err
	}
	cfg := legacy.fileConfig
	cfg.Chatroom = legacy.Chatroom
	cfg.RoomID = legacy.RoomID
	cfg.IntegrationID = legacy.IntegrationID
	cfg.BridgeSecret = legacy.BridgeSecret
	cfg.ExchangeSecret = legacy.ExchangeSecret
	cfg.SecretVersion = legacy.SecretVersion
	return cfg, nil
}

// pickString 按 文件 > 环境变量 > 内置默认 的顺序取值
func pickString(fileVal, envKey, def string) string {
	if fileVal != "" {
		def = fileVal
	}
	if v := os.Getenv(envKey); v != "" {
		def = v
	}
	return def
}

// pickInt 同上，用于整型配置
func pickInt(fileVal int, envKey string, def int) int {
	if fileVal != 0 {
		def = fileVal
	}
	if v := os.Getenv(envKey); v != "" {
		if n, err := parseInt(v); err == nil {
			return n
		}
	}
	return def
}

// parseInt 解析整型，失败返回错误
func parseInt(s string) (int, error) {
	var n int
	_, err := fmt.Sscan(s, &n)
	return n, err
}
