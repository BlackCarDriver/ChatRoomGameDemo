package main

import (
	"crypto/rsa"
	"encoding/json"
	"log"
	"net/http"
	"time"

	"github.com/BlackCarDriver/nichijouSDK"
	"github.com/gorilla/websocket"
)

var upgrader = websocket.Upgrader{
	ReadBufferSize:  4096,
	WriteBufferSize: 4096,
	// 身份由 JWT 校验，这里不额外校验 Origin。正式部署可在反向代理层限制来源。
	CheckOrigin: func(r *http.Request) bool { return true },
}

// Handler 页面服务与游戏 WebSocket 入口。
type Handler struct {
	cfg *Config
	pub *rsa.PublicKey
	hub *hub
}

func (s *Handler) ServeWS(w http.ResponseWriter, r *http.Request) {
	if s.pub == nil {
		http.Error(w, "identity not configured", http.StatusServiceUnavailable)
		return
	}
	token := r.URL.Query().Get("token")
	if token == "" {
		http.Error(w, "missing token", http.StatusUnauthorized)
		return
	}
	claims, err := nichijouSDK.VerifyJWT(token, s.pub, s.cfg.IntegrationID, s.cfg.RoomID)
	if err != nil {
		log.Printf("[ws] 令牌校验失败: %v", err)
		http.Error(w, "invalid token", http.StatusUnauthorized)
		return
	}

	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}

	// 房间已关闭：直接关闭，不再接纳
	if s.hub.isRoomClosed() {
		_ = conn.WriteControl(
			websocket.CloseMessage,
			websocket.FormatCloseMessage(4001, "room closed"),
			time.Now().Add(time.Second),
		)
		_ = conn.Close()
		return
	}

	// 结算窗口内：直接告知客户端并关闭，不创建账号、不发每日金币。
	if s.hub.windowView().Phase == windowClosed {
		data, _ := json.Marshal(s.hub.windowMessage())
		_ = conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
		_ = conn.WriteMessage(websocket.TextMessage, data)
		_ = conn.WriteControl(
			websocket.CloseMessage,
			websocket.FormatCloseMessage(4000, "settlement"),
			time.Now().Add(time.Second),
		)
		_ = conn.Close()
		return
	}

	c := &client{
		conn:   conn,
		nick:   claims.Sub,
		send:   make(chan []byte, 128),
		remote: r.RemoteAddr,
		roleID: claims.RoleID,
		avatar: claims.Avatar,
		frame:  claims.Frame,
		done:   make(chan struct{}),
	}
	go c.writePump()
	s.hub.activateClient(c, claims)
	s.readPump(c)
}

func (s *Handler) readPump(c *client) {
	defer s.hub.handleDisconnect(c)

	c.conn.SetReadLimit(64 * 1024)
	var lastMsg time.Time
	for {
		_, data, err := c.conn.ReadMessage()
		if err != nil {
			return
		}
		// 简单限频：单连接每秒最多 20 条消息
		now := time.Now()
		if !lastMsg.IsZero() && now.Sub(lastMsg) < 50*time.Millisecond {
			continue
		}
		lastMsg = now

		var msg struct {
			Type        string `json:"type"`
			AmountCents int64  `json:"amountCents"`
			RequestID   string `json:"requestId"`
		}
		if err := json.Unmarshal(data, &msg); err != nil {
			continue
		}

		switch msg.Type {
		case "bet":
			s.hub.Bet(c.nick, msg.RequestID, msg.AmountCents)
		case "cashout":
			s.hub.Cashout(c.nick, msg.RequestID)
		case "reveal":
			s.hub.Reveal(c.nick, msg.RequestID)
		case "borrow":
			s.hub.Borrow(c.nick, msg.RequestID, msg.AmountCents)
		case "repay":
			s.hub.Repay(c.nick, msg.RequestID, msg.AmountCents)
		case "leaderboard":
			s.hub.SendLeaderboard(c.nick)
		case "sync":
			s.hub.SendState(c.nick)
		case "ping":
			c.sendJSON(map[string]interface{}{
				"type":       "pong",
				"serverTime": s.hub.now().UnixMilli(),
			})
		default:
			s.hub.sendError(c.nick, msg.RequestID, "未知操作")
		}
	}
}
