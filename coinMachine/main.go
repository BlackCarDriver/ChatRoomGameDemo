package main

import (
	"crypto/rsa"
	"embed"
	"flag"
	"io/fs"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"
	_ "time/tzdata"

	"github.com/BlackCarDriver/nichijouSDK"
)

//go:embed public/*
var publicFS embed.FS

func noCache(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-cache")
		next.ServeHTTP(w, r)
	})
}

func main() {
	cfg := &Config{}

	configPath := defaultConfigPath
	if v := os.Getenv("DEMO_CONFIG"); v != "" {
		configPath = v
	}
	fileCfg, err := loadFileConfig(configPath)
	if err != nil {
		log.Fatalf("读取运行配置失败 %s: %v", configPath, err)
	}
	if fileCfg != (fileConfig{}) {
		log.Printf("[start] 已加载运行配置 %s", configPath)
	}

	integrationPath := defaultIntegrationPath
	if v := os.Getenv("DEMO_INTEGRATION"); v != "" {
		integrationPath = v
	}
	intCfg, err := nichijouSDK.LoadIntegrationConfig(integrationPath)
	if err != nil {
		log.Fatalf("读取接入配置失败 %s: %v", integrationPath, err)
	}
	intCfg = nichijouSDK.MergeIntegration(intCfg, nichijouSDK.IntegrationConfig{
		Chatroom:       fileCfg.Chatroom,
		RoomID:         fileCfg.RoomID,
		IntegrationID:  fileCfg.IntegrationID,
		BridgeSecret:   fileCfg.BridgeSecret,
		ExchangeSecret: fileCfg.ExchangeSecret,
		SecretVersion:  fileCfg.SecretVersion,
	})
	if intCfg != (nichijouSDK.IntegrationConfig{}) {
		log.Printf("[start] 已加载接入配置 %s（房间 %d）", integrationPath, intCfg.RoomID)
	}

	flag.StringVar(&cfg.Addr, "addr", pickString(fileCfg.Addr, "DEMO_ADDR", ":8091"), "本服务监听地址")
	flag.StringVar(&cfg.ChatroomBase, "chatroom", pickString(intCfg.Chatroom, "CHATROOM", "http://127.0.0.1:4430"), "聊天室 HTTP 基地址")
	flag.IntVar(&cfg.RoomID, "room-id", pickInt(intCfg.RoomID, "ROOM_ID", 0), "插件房房间号（留空则仅提供页面服务）")
	flag.StringVar(&cfg.IntegrationID, "integration-id", pickString(intCfg.IntegrationID, "INTEGRATION_ID", ""), "接入方 ID（JWT 的 aud）")
	flag.StringVar(&cfg.BridgeSecret, "bridge-secret", pickString(intCfg.BridgeSecret, "BRIDGE_SECRET", ""), "桥接密钥")
	flag.StringVar(&cfg.ExchangeSecret, "exchange-secret", pickString(intCfg.ExchangeSecret, "EXCHANGE_SECRET", ""), "票据兑换凭证")
	flag.Int64Var(&cfg.SecretVersion, "secret-version", pickInt64(intCfg.SecretVersion, "SECRET_VERSION", 1), "桥接密钥版本")
	flag.StringVar(&cfg.InstanceID, "instance-id", pickString(fileCfg.InstanceID, "INSTANCE_ID", "coin-1"), "实例标识")
	flag.StringVar(&cfg.PublicKeyPath, "pubkey", pickString(fileCfg.PublicKeyPath, "PUBKEY", "config/plugin_jwt.pub.pem"), "聊天室公钥 PEM 路径")
	flag.StringVar(&cfg.DataDir, "data", pickString(fileCfg.DataDir, "DATA_DIR", "data"), "存档目录")
	flag.StringVar(&cfg.Timezone, "tz", pickString(fileCfg.Timezone, "TZ_NAME", "Asia/Shanghai"), "结算时区")
	flag.Int64Var(&cfg.DailyGiftCoins, "gift-coins", pickInt64(fileCfg.DailyGiftCoins, "GIFT_COINS", 10), "每日赠送金币")
	flag.IntVar(&cfg.FreePlays, "free-plays", pickInt(fileCfg.FreePlays, "FREE_PLAYS", 10), "每日免费押注次数")
	flag.Int64Var(&cfg.FeeCoins, "fee-coins", pickInt64(fileCfg.FeeCoins, "FEE_COINS", 1), "免费次数用完后每次押注手续费")
	flag.Int64Var(&cfg.InterestRateBPS, "interest-bps", pickInt64(fileCfg.InterestRateBPS, "INTEREST_BPS", 100), "日利率（基点，100=1%）")
	flag.Int64Var(&cfg.LoanMultiplier, "loan-multiplier", pickInt64(fileCfg.LoanMultiplier, "LOAN_MULTIPLIER", 100), "贷款额度倍数")
	flag.Int64Var(&cfg.RevealMs, "reveal-ms", pickInt64(fileCfg.RevealMs, "REVEAL_MS", 2500), "开奖动画时长（毫秒）")
	flag.IntVar(&cfg.GraceSeconds, "grace-seconds", pickInt(fileCfg.GraceSeconds, "GRACE_SECONDS", 3), "断线自动提款宽限（秒）")
	logFilePath := pickString("", "LOG_FILE", "")
	logMaxKB := pickInt(0, "LOG_MAX_KB", 200)
	flag.StringVar(&logFilePath, "log-file", logFilePath, "日志文件路径（用于自动裁剪，留空则不裁剪）")
	flag.IntVar(&logMaxKB, "log-max-kb", logMaxKB, "日志文件超过该大小（KB）时裁剪旧内容")
	flag.Parse()

	cfg.GiftCents = cfg.DailyGiftCoins * 100
	cfg.FeeCents = cfg.FeeCoins * 100

	loc, err := time.LoadLocation(cfg.Timezone)
	if err != nil {
		log.Fatalf("加载时区失败 %s: %v", cfg.Timezone, err)
	}

	bridgeReady := cfg.RoomID != 0 && cfg.IntegrationID != "" && cfg.BridgeSecret != ""
	cfg.ChatroomWSBase = strings.Replace(
		strings.Replace(cfg.ChatroomBase, "https://", "wss://", 1),
		"http://", "ws://", 1,
	)

	var pub *rsa.PublicKey
	if loaded, err := nichijouSDK.LoadPublicKey(cfg.PublicKeyPath); err == nil {
		pub = loaded
	} else if bridgeReady {
		log.Fatalf("加载公钥失败: %v", err)
	} else {
		log.Printf("[start] 未加载公钥（%v），游戏连接将不可用", err)
	}
	cfg.PublicKey = pub

	store := NewStore(cfg.DataDir)
	if err := store.Load(); err != nil {
		log.Fatalf("加载存档失败: %v", err)
	}

	gameHub := newHub(cfg, store, loc)
	// 服务器重启：全部强制结束并自动提款
	gameHub.ForceSettleAllOnStartup()

	var bridge *nichijouSDK.Bridge
	if bridgeReady {
		bridge = nichijouSDK.NewBridge(nichijouSDK.Config{
			ChatroomWSBase: cfg.ChatroomWSBase,
			RoomID:         cfg.RoomID,
			IntegrationID:  cfg.IntegrationID,
			BridgeSecret:   cfg.BridgeSecret,
			SecretVersion:  cfg.SecretVersion,
			InstanceID:     cfg.InstanceID,
			PublicKey:      pub,
		}, nichijouSDK.Hooks{
			States: func() map[string]nichijouSDK.State {
				return gameHub.BridgeStates()
			},
			RoomLP:       func() string { return gameHub.RoomLP() },
			OnUserLeave:  func(nick string) { gameHub.OnUserLeave(nick) },
			OnChat:       func(nick, text string, timestamp int64) {},
			OnRoomClosed: func() { gameHub.OnRoomClosed() },
		})
		gameHub.setBridge(bridge)
		go bridge.Run()
	} else {
		missing := make([]string, 0, 3)
		if cfg.RoomID == 0 {
			missing = append(missing, "-room-id")
		}
		if cfg.IntegrationID == "" {
			missing = append(missing, "-integration-id")
		}
		if cfg.BridgeSecret == "" {
			missing = append(missing, "-bridge-secret")
		}
		log.Printf("[start] 仅提供页面服务（不连桥接通道），缺少：%s", strings.Join(missing, "、"))
	}

	stop := make(chan struct{})
	go gameHub.Run(stop)

	handler := &Handler{cfg: cfg, pub: pub, hub: gameHub}

	mux := http.NewServeMux()
	var staticHandler http.Handler
	if _, statErr := os.Stat("./public"); statErr == nil {
		log.Printf("页面资源：磁盘模式 ./public（开发热生效）")
		staticHandler = http.FileServer(http.Dir("./public"))
	} else {
		log.Printf("页面资源：内嵌模式（发布单文件）")
		publicRoot, err := fs.Sub(publicFS, "public")
		if err != nil {
			log.Fatalf("加载内嵌页面失败: %v", err)
		}
		staticHandler = http.FileServer(http.FS(publicRoot))
	}
	// 页面资源每次加载都回源校验，避免聊天室 iframe 缓存旧版 app.js
	mux.Handle("/", noCache(staticHandler))
	mux.HandleFunc("/ws", handler.ServeWS)
	mux.Handle("/exchange", nichijouSDK.NewExchangeHandler(nichijouSDK.ExchangeConfig{
		ChatroomBase:   cfg.ChatroomBase,
		ExchangeSecret: cfg.ExchangeSecret,
	}))

	srv := &http.Server{
		Addr:              cfg.Addr,
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
	}

	startLogTrim(logFilePath, logMaxKB)

	log.Printf("梭哈模拟器启动于 %s", cfg.Addr)
	log.Printf("  房间号   room-id       = %d", cfg.RoomID)
	log.Printf("  接入方   integration-id= %s", cfg.IntegrationID)
	log.Printf("  聊天室   chatroom      = %s", cfg.ChatroomBase)
	log.Printf("  时区     timezone      = %s", cfg.Timezone)
	log.Printf("  规则     每日赠 %d 金币 · 前 %d 次免费 · 之后手续费 %d/%d/%d…（每 10 局升一档）· 日息 %d bp · 贷款 %d 倍 · 开奖 %dms · 宽限 %ds",
		cfg.DailyGiftCoins, cfg.FreePlays, cfg.FeeCoins, cfg.FeeCoins*2, cfg.FeeCoins*3,
		cfg.InterestRateBPS, cfg.LoanMultiplier, cfg.RevealMs, cfg.GraceSeconds)

	errCh := make(chan error, 1)
	go func() {
		errCh <- srv.ListenAndServe()
	}()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	select {
	case sig := <-sigCh:
		log.Printf("[shutdown] 收到信号 %v", sig)
	case err := <-errCh:
		if err != nil && err != http.ErrServerClosed {
			log.Printf("[http] 服务退出: %v", err)
		}
	}

	close(stop)
	gameHub.Shutdown()
	_ = srv.Close()
}
