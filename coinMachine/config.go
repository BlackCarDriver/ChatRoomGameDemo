package main

import (
	"crypto/rsa"
	"encoding/json"
	"fmt"
	"os"
	"strconv"
)

// ============================ 配置 ============================
//
// config/integration.json   聊天室下发的接入凭证，整份替换。
// config/plugin_config.json 本服务的运行参数与游戏参数。
//
// 优先级：命令行参数 > 环境变量 > integration.json > plugin_config.json > 内置默认值

const (
	defaultConfigPath      = "config/plugin_config.json"
	defaultIntegrationPath = "config/integration.json"
)

// Config 运行配置
type Config struct {
	Addr           string
	ChatroomBase   string
	ChatroomWSBase string
	RoomID         int
	IntegrationID  string
	BridgeSecret   string
	ExchangeSecret string
	SecretVersion  int64
	InstanceID     string
	PublicKeyPath  string
	PublicKey      *rsa.PublicKey
	DataDir        string
	Timezone       string

	// 游戏规则
	DailyGiftCoins  int64
	FreePlays       int
	FeeCoins        int64
	InterestRateBPS int64 // 100 = 1%
	LoanMultiplier  int64 // 100 = 最多借可用余额的 100 倍
	RevealMs        int64
	GraceSeconds    int

	// 派生
	GiftCents int64
	FeeCents  int64
}

// fileConfig plugin_config.json 的结构
type fileConfig struct {
	Addr            string `json:"addr"`
	InstanceID      string `json:"instanceId"`
	PublicKeyPath   string `json:"pubkey"`
	DataDir         string `json:"dataDir"`
	Timezone        string `json:"timezone"`
	DailyGiftCoins  int64  `json:"dailyGiftCoins"`
	FreePlays       int    `json:"freePlays"`
	FeeCoins        int64  `json:"feeCoins"`
	InterestRateBPS int64  `json:"interestRateBps"`
	LoanMultiplier  int64  `json:"loanMultiplier"`
	RevealMs        int64  `json:"revealMs"`
	GraceSeconds    int    `json:"graceSeconds"`

	// 兼容旧版：接入字段也可能写在 plugin_config.json 里
	Chatroom       string `json:"-"`
	RoomID         int    `json:"-"`
	IntegrationID  string `json:"-"`
	BridgeSecret   string `json:"-"`
	ExchangeSecret string `json:"-"`
	SecretVersion  int64  `json:"-"`
}

type legacyFileConfig struct {
	fileConfig
	Chatroom       string `json:"chatroom"`
	RoomID         int    `json:"roomId"`
	IntegrationID  string `json:"integrationId"`
	BridgeSecret   string `json:"bridgeSecret"`
	ExchangeSecret string `json:"exchangeSecret"`
	SecretVersion  int64  `json:"secretVersion"`
}

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
	if err := json.Unmarshal(raw, &legacy); err != nil {
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

func pickString(fileVal, envKey, def string) string {
	if fileVal != "" {
		def = fileVal
	}
	if v := os.Getenv(envKey); v != "" {
		def = v
	}
	return def
}

func pickInt(fileVal int, envKey string, def int) int {
	if fileVal != 0 {
		def = fileVal
	}
	if v := os.Getenv(envKey); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return def
}

func pickInt64(fileVal int64, envKey string, def int64) int64 {
	if fileVal != 0 {
		def = fileVal
	}
	if v := os.Getenv(envKey); v != "" {
		if n, err := strconv.ParseInt(v, 10, 64); err == nil {
			return n
		}
	}
	return def
}

var _ = fmt.Sprintf
