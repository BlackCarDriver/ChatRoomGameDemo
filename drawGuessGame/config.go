package main

import (
	"encoding/json"
	"fmt"
	"os"
)

// ============================ 配置文件 ============================
//
// 与围棋示例相同的双文件约定：
//   config/integration.json   接入配置：聊天室下发的凭证与参数，整份替换。
//   config/plugin_config.json 运行配置：本服务自身的部署参数（端口、公钥路径、回合时限、词库路径等）。
//
// 优先级：命令行参数 > 环境变量 > integration.json > plugin_config.json > 内置默认值

const (
	defaultConfigPath      = "config/plugin_config.json"
	defaultIntegrationPath = "config/integration.json"
	defaultWordsPath       = "config/draw_words.json"
)

// fileConfig 运行配置，只放本服务自身的部署参数
type fileConfig struct {
	Addr          string `json:"addr"`
	InstanceID    string `json:"instanceId"`
	PublicKeyPath string `json:"pubkey"`
	WordsPath     string `json:"wordsPath"`
	// RoundSeconds 绘画回合时长（秒），默认 60
	RoundSeconds int `json:"roundSeconds"`
	// SelectSeconds 选题时长（秒），默认 10
	SelectSeconds int `json:"selectSeconds"`
	// ResultSeconds 回合结算展示时长（秒），默认 8
	ResultSeconds int `json:"resultSeconds"`
	// TotalRounds 单局总回合数，0 表示每人当一次画手
	TotalRounds int `json:"totalRounds"`
	// MaxPlayers 单局最大参与人数（超出者在游戏中进房则观战，下局自动加入），默认 12
	MaxPlayers int `json:"maxPlayers"`

	// 兼容旧版：允许把接入字段继续写在 plugin_config.json 里。
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
