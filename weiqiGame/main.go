package main

import (
	"crypto/rsa"
	"embed"
	"encoding/json"
	"flag"
	"fmt"
	"io/fs"
	"log"
	"net/http"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	"ChatRoomGameDemo/nichijouSDK"
	"github.com/gorilla/websocket"
)

//go:embed public/*
var publicFS embed.FS

// Config 运行配置
type Config struct {
	// Addr 本服务监听地址
	Addr string
	// ChatroomBase 聊天室 HTTP 基地址，如 http://127.0.0.1:8080
	ChatroomBase string
	// ChatroomWSBase 聊天室 WebSocket 基地址，由 ChatroomBase 推导
	ChatroomWSBase string
	// RoomID 房间号
	RoomID int
	// IntegrationID 接入方 ID（= JWT 的 aud）
	IntegrationID string
	// BridgeSecret 桥接密钥
	BridgeSecret string
	// ExchangeSecret 兑换凭证
	ExchangeSecret string
	// SecretVersion 密钥版本
	SecretVersion int64
	// InstanceID 实例标识，仅用于排障
	InstanceID string
	// PublicKeyPath 聊天室公钥 PEM
	PublicKeyPath string
	// PublicKey 已加载的公钥
	PublicKey *rsa.PublicKey
	// TurnSeconds 每回合思考时限（秒），0 表示不计时
	TurnSeconds int
}

// state 用户状态
type state struct {
	State string
	Text  string
}

// client 游戏连接
type client struct {
	conn   *websocket.Conn
	nick   string
	send   chan []byte
	remote string
	// 展示资料：来自聊天室签发的 JWT 声明，服务端可信，可直接下发给同房间客户端
	roleID  int
	avatar  string
	frameID int
	frame   string
}

// Hub 房间内所有连接与对局状态
type Hub struct {
	lock    sync.Mutex
	clients map[string]*client
	seats   map[int]string
	game    *Game
	bridge  *nichijouSDK.Bridge

	// mode 当前玩法：go / gomoku（房间级设置，非对局期间可切换）
	mode string
	// turnSeconds 每回合思考时限（秒），0 表示不计时
	turnSeconds int
	// turnTimer 当前回合倒计时器
	turnTimer *time.Timer
	// turnSeq 计时器代号：每次重置自增，旧计时器回调据此自废
	turnSeq int64

	// seatReconnectGrace 游戏连接断开后保留座位的宽限期：
	// 期间同昵称重连自动恢复座位，不丢座；超时未归才释放座位。
	seatReconnectGrace time.Duration
	// pendingRelease 待释放座位：nick -> 计划释放时刻（unix 毫秒）。
	// 仅用于断开后的宽限期；离座/离房/换座等主动操作不走这里。
	pendingRelease map[string]int64
}

var gameHub = NewHub(nil)

// NewHub 创建 hub
func NewHub(bridge *nichijouSDK.Bridge) *Hub {
	return &Hub{
		clients:            map[string]*client{},
		seats:              map[int]string{},
		mode:               ModeGo,
		turnSeconds:        60,
		seatReconnectGrace: 30 * time.Second,
		pendingRelease:     map[string]int64{},
		bridge:             bridge,
	}
}

// SetBridge 注入桥接客户端
func (h *Hub) SetBridge(b *nichijouSDK.Bridge) {
	h.lock.Lock()
	h.bridge = b
	h.lock.Unlock()
}

// AllStates 全量状态快照
func (h *Hub) AllStates() map[string]*state {
	h.lock.Lock()
	defer h.lock.Unlock()
	out := make(map[string]*state, len(h.clients))
	for nick := range h.clients {
		out[nick] = h.stateOf(nick)
	}
	return out
}

// stateOf 计算某用户当前状态（调用方需持锁）
func (h *Hub) stateOf(nick string) *state {
	if h.game != nil && h.game.Phase == PhasePlaying {
		if nick == h.game.Black || nick == h.game.White {
			return &state{State: "playing"}
		}
	}
	if h.seats[ColorBlack] == nick || h.seats[ColorWhite] == nick {
		return &state{State: "waiting"}
	}
	return &state{State: "spectating"}
}

// report 某用户状态变化后重推全量状态。
// 之所以重推全量而不是只报该用户：聊天室侧对单条上报做了节流，短时间内连续上报
// 不同用户会丢掉后一条（例如开局时只报到黑方、白方漏报）。全量快照即权威，
// 合并重推后所有人的状态都一致。
func (h *Hub) report(nick string) {
	h.reportAll()
}

// reportAll 上报全部用户状态（合并式全量重推）
func (h *Hub) reportAll() {
	if h.bridge != nil {
		h.bridge.ScheduleFullReport()
	}
}

// snapshot 生成下发给前端的完整快照
func (h *Hub) snapshot() map[string]interface{} {
	h.lock.Lock()
	defer h.lock.Unlock()

	seats := map[string]string{
		"black": h.seats[ColorBlack],
		"white": h.seats[ColorWhite],
	}
	mode := h.mode
	if mode == "" {
		mode = ModeGo
	}
	users := make([]map[string]interface{}, 0, len(h.clients))
	for nick, c := range h.clients {
		st := h.stateOf(nick)
		users = append(users, map[string]interface{}{
			"nick":    nick,
			"state":   st.State,
			"text":    st.Text,
			"roleId":  c.roleID,
			"avatar":  c.avatar,
			"frameId": c.frameID,
			"frame":   c.frame,
		})
	}
	// map 遍历顺序随机，排序保证各客户端渲染顺序一致
	sort.Slice(users, func(i, j int) bool {
		return users[i]["nick"].(string) < users[j]["nick"].(string)
	})

	var game map[string]interface{}
	if h.game != nil {
		game = map[string]interface{}{
			"mode":     h.game.Mode,
			"board":    h.game.Board.Cells,
			"size":     h.game.Board.Size,
			"turn":     h.game.Turn,
			"phase":    h.game.Phase,
			"black":    h.game.Black,
			"white":    h.game.White,
			"moves":    h.game.Moves,
			"winner":   h.game.Winner,
			"result":   h.game.Result,
			"captured": h.game.Captured,
			// abandoned 为 true 表示对局因棋手离房中止，没有胜方
			"abandoned": h.game.Abandoned,
			// 本回合截止时间（服务端毫秒），前端配合 serverNow 换算剩余秒数
			"turnDeadline": h.game.TurnDeadline,
			// startedAt 开局时刻（服务端毫秒），前端据此展示本局用时
			"startedAt": h.game.StartedAt,
		}
	}
	return map[string]interface{}{
		"type":  "state",
		"seats": seats,
		"users": users,
		"game":  game,
		// mode 当前玩法；size 随玩法固定（围棋 19 路 / 五子棋 15 路），
		// 等待阶段也要下发，前端才能先画出棋盘
		"mode": mode,
		"size": BoardSizeForMode(mode),
		// serverNow 用于消除客户端与服务器的时钟偏差（倒计时）
		"serverNow":   time.Now().UnixMilli(),
		"seatNo":      2,
		"turnSeconds": h.turnSeconds,
	}
}

// Broadcast 广播快照给所有连接
func (h *Hub) Broadcast() {
	data, err := json.Marshal(h.snapshot())
	if err != nil {
		return
	}
	h.lock.Lock()
	clients := make([]*client, 0, len(h.clients))
	for _, c := range h.clients {
		clients = append(clients, c)
	}
	h.lock.Unlock()
	for _, c := range clients {
		select {
		case c.send <- data:
		default:
		}
	}
}

// BroadcastChat 把聊天室转来的游戏内聊天广播给游戏内玩家。
// ts 为聊天室给出的发送时间（Unix 秒），缺失时用收到的时刻补上。
func (h *Hub) BroadcastChat(nick, text string, ts int64) {
	if ts <= 0 {
		ts = time.Now().Unix()
	}
	h.lock.Lock()
	avatar, frame := "", ""
	if c, ok := h.clients[nick]; ok {
		avatar, frame = c.avatar, c.frame
	}
	clients := make([]*client, 0, len(h.clients))
	for _, c := range h.clients {
		clients = append(clients, c)
	}
	h.lock.Unlock()

	data, _ := json.Marshal(map[string]interface{}{
		"type":      "chat",
		"nick":      nick,
		"text":      text,
		"avatar":    avatar,
		"frame":     frame,
		"timestamp": ts,
	})
	for _, c := range clients {
		select {
		case c.send <- data:
		default:
		}
	}
}

// OnUserLeaveRoom 聊天室通知该用户已离开房间
func (h *Hub) OnUserLeaveRoom(nick string) {
	h.lock.Lock()
	// 离开房间属于主动离开：清掉待释放标记，座位立即释放
	delete(h.pendingRelease, nick)
	if c, ok := h.clients[nick]; ok {
		close(c.send)
		_ = c.conn.Close()
		delete(h.clients, nick)
	}
	for color, seatNick := range h.seats {
		if seatNick == nick {
			delete(h.seats, color)
		}
	}
	// 棋手离开则本局作废，避免出现幽灵对局
	if h.game != nil && h.game.Phase == PhasePlaying && (h.game.Black == nick || h.game.White == nick) {
		h.game.Phase = PhaseEnded
		h.game.Abandoned = true
		h.game.Result = fmt.Sprintf("%s 离开房间，对局中止", nick)
	}
	// 对局已结束：停掉倒计时
	h.armTurnTimerLocked()
	h.lock.Unlock()

	if h.bridge != nil {
		h.bridge.EventLog(fmt.Sprintf("【%s】离开了房间", nick))
	}
	// 全量重推：离开的用户已不在快照中，聊天室会把它重置为 idle
	h.syncRoomLP()
	h.reportAll()
	h.Broadcast()
}

// roomLP 计算房间卡片副文案（调用方无需持锁）
func (h *Hub) roomLP() string {
	h.lock.Lock()
	defer h.lock.Unlock()
	return h.roomLPLocked()
}

// roomLPLocked 按当前对局/座位状态计算卡片副文案（调用方需持锁）
func (h *Hub) roomLPLocked() string {
	if h.game != nil && h.game.Phase == PhasePlaying {
		return "对局中-" + h.modeLabel()
	}
	if h.seats[ColorBlack] != "" || h.seats[ColorWhite] != "" {
		return "等待对手中"
	}
	return "对局待开始"
}

// modeLabel 当前玩法的中文名
func (h *Hub) modeLabel() string {
	if h.mode == ModeGomoku {
		return "五子棋"
	}
	return "围棋"
}

// syncRoomLP 向聊天室同步一次卡片副文案
func (h *Hub) syncRoomLP() {
	if h.bridge != nil {
		h.bridge.SetRoomLP(h.roomLP())
	}
}

// StartGame 开始对局
func (h *Hub) StartGame() error {
	h.lock.Lock()
	black, white := h.seats[ColorBlack], h.seats[ColorWhite]
	if black == "" || white == "" {
		h.lock.Unlock()
		return fmt.Errorf("需要两位棋手就座才能开始")
	}
	mode := h.mode
	if mode == "" {
		mode = ModeGo
	}
	h.game = NewGame(BoardSizeForMode(mode), black, white)
	h.game.Mode = mode
	// 黑方先行，开局即开始计时
	h.armTurnTimerLocked()
	h.lock.Unlock()

	if h.bridge != nil {
		h.bridge.EventLog(fmt.Sprintf("对局开始：%s 执黑 vs %s 执白", black, white))
	}
	h.syncRoomLP()
	h.reportAll()
	h.Broadcast()
	return nil
}

// abortGameOnSeatChangeLocked 对局进行中，棋手离座/换座则本局中止（调用方需持锁）。
// 返回是否中止以及结果文案。
func (h *Hub) abortGameOnSeatChangeLocked(nick string) (bool, string) {
	if h.game == nil || h.game.Phase != PhasePlaying {
		return false, ""
	}
	if h.game.Black != nick && h.game.White != nick {
		return false, ""
	}
	h.game.Phase = PhaseEnded
	h.game.Abandoned = true
	h.game.Result = fmt.Sprintf("%s 离座，对局中止", nick)
	h.armTurnTimerLocked()
	return true, h.game.Result
}

// ---------- 断开宽限期补座 ----------

// markSeatPendingRelease 游戏连接断开：把该昵称的座位标记为“待释放”，
// 给同昵称重连留宽限期。有座位或正在对局的昵称才需要标记。
func (h *Hub) markSeatPendingRelease(nick string) {
	h.lock.Lock()
	if h.seats[ColorBlack] != nick && h.seats[ColorWhite] != nick &&
		(h.game == nil || (h.game.Black != nick && h.game.White != nick)) {
		h.lock.Unlock()
		return
	}
	grace := h.seatReconnectGrace
	releaseAt := time.Now().Add(grace).UnixMilli()
	h.pendingRelease[nick] = releaseAt
	h.lock.Unlock()

	// 宽限期到后若仍未重连，则释放座位（对局中棋手按掉线中止）
	time.AfterFunc(grace, func() {
		h.releasePendingSeat(nick, releaseAt)
	})
}

// releasePendingSeat 宽限期到期：释放该昵称的座位。
// releaseAt 用于匹配：被更新的待释放标记或已重连（标记被删除）时不动作。
func (h *Hub) releasePendingSeat(nick string, releaseAt int64) {
	h.lock.Lock()
	if h.pendingRelease[nick] != releaseAt {
		h.lock.Unlock()
		return
	}
	delete(h.pendingRelease, nick)
	result, aborted := h.releaseSeatLocked(nick)
	h.armTurnTimerLocked()
	h.lock.Unlock()

	if aborted && result != "" && h.bridge != nil {
		h.bridge.EventLog("对局结束：" + result)
	}
	h.syncRoomLP()
	h.reportAll()
	h.Broadcast()
}

// releaseSeatLocked 释放某昵称的座位；对局进行中且该昵称是本局棋手时，
// 按“掉线”中止对局（调用方需持锁）。返回结果文案与是否中止。
func (h *Hub) releaseSeatLocked(nick string) (string, bool) {
	aborted := false
	for color, seatNick := range h.seats {
		if seatNick == nick {
			delete(h.seats, color)
		}
	}
	if h.game != nil && h.game.Phase == PhasePlaying && (h.game.Black == nick || h.game.White == nick) {
		h.game.Phase = PhaseEnded
		h.game.Abandoned = true
		h.game.Result = fmt.Sprintf("%s 掉线，对局中止", nick)
		aborted = true
	}
	result := ""
	if h.game != nil {
		result = h.game.Result
	}
	return result, aborted
}

// ResetGame 结束并清空当前对局，回到等待开始（保留座位，可重新开局）。
// 仅允许在非对局进行中由棋手操作，避免对局中被用来逃避劣势。
func (h *Hub) ResetGame(nick string) error {
	h.lock.Lock()
	if h.game == nil {
		h.lock.Unlock()
		return fmt.Errorf("当前没有已结束的对局")
	}
	if h.game.Phase == PhasePlaying {
		h.lock.Unlock()
		return fmt.Errorf("对局进行中，不能重置")
	}
	if h.seats[ColorBlack] != nick && h.seats[ColorWhite] != nick {
		h.lock.Unlock()
		return fmt.Errorf("只有本局棋手可以重置对局")
	}
	h.game = nil
	// 停表（game 为 nil 时 armTurnTimerLocked 只做停止）
	h.armTurnTimerLocked()
	h.lock.Unlock()

	if h.bridge != nil {
		h.bridge.EventLog(fmt.Sprintf("【%s】重置了对局", nick))
	}
	h.syncRoomLP()
	h.reportAll()
	h.Broadcast()
	return nil
}

// SetMode 切换玩法（围棋 / 五子棋）。
// 对局进行中不允许切换；切换时清空已结束的对局，因为旧棋盘尺寸/棋形不再适用。
func (h *Hub) SetMode(mode string) error {
	if mode != ModeGo && mode != ModeGomoku {
		return fmt.Errorf("不支持的玩法")
	}
	h.lock.Lock()
	if h.game != nil && h.game.Phase == PhasePlaying {
		h.lock.Unlock()
		return fmt.Errorf("对局进行中，不能切换玩法")
	}
	if h.mode == mode {
		h.lock.Unlock()
		return nil
	}
	h.mode = mode
	h.game = nil
	h.armTurnTimerLocked()
	h.lock.Unlock()

	if h.bridge != nil {
		h.bridge.EventLog("玩法已切换为" + ModeLabel(mode))
	}
	h.reportAll()
	h.Broadcast()
	return nil
}

// ---------- 回合倒计时 ----------

// armTurnTimerLocked 重置本回合倒计时（调用方必需持锁）。
// 未开局、对局结束或关闭计时（turnSeconds<=0）时只停表。
func (h *Hub) armTurnTimerLocked() {
	h.turnSeq++
	seq := h.turnSeq
	if h.turnTimer != nil {
		h.turnTimer.Stop()
		h.turnTimer = nil
	}
	if h.game == nil {
		return
	}
	if h.game.Phase != PhasePlaying || h.turnSeconds <= 0 {
		h.game.TurnDeadline = 0
		return
	}
	timeout := time.Duration(h.turnSeconds) * time.Second
	h.game.TurnDeadline = time.Now().Add(timeout).UnixMilli()
	// 旧计时器回调根据 seq 自废，避免重置前的回调误落子
	h.turnTimer = time.AfterFunc(timeout, func() {
		h.onTurnTimeout(seq)
	})
}

// onTurnTimeout 回合超时：视为放弃一手，直接轮到对手
func (h *Hub) onTurnTimeout(seq int64) {
	h.lock.Lock()
	if seq != h.turnSeq || h.game == nil || h.game.Phase != PhasePlaying {
		h.lock.Unlock()
		return
	}
	game := h.game
	nick := game.Timeout()
	finished := game.Phase == PhaseEnded
	result := game.Result
	// 停手后重新计时（对局结束则自动停表）
	h.armTurnTimerLocked()
	h.lock.Unlock()

	if nick != "" && h.bridge != nil {
		h.bridge.EventLog(fmt.Sprintf("【%s】思考超时，视为放弃一手", nick))
	}
	if finished {
		if h.bridge != nil && result != "" {
			h.bridge.EventLog("对局结束：" + result)
		}
		h.syncRoomLP()
		h.reportAll()
	}
	h.Broadcast()
}

// ---------- HTTP ----------

var upgrader = websocket.Upgrader{
	ReadBufferSize:  4096,
	WriteBufferSize: 4096,
	CheckOrigin:     func(r *http.Request) bool { return true },
}

// Handler 服务入口
type Handler struct {
	cfg    *Config
	pub    *rsa.PublicKey
	bridge *nichijouSDK.Bridge
}

// ServeWS 浏览器连接游戏服务的入口，凭 JWT 鉴权
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

	c := &client{
		conn:    conn,
		nick:    claims.Sub,
		send:    make(chan []byte, 64),
		remote:  r.RemoteAddr,
		roleID:  claims.RoleID,
		avatar:  claims.Avatar,
		frameID: claims.FrameID,
		frame:   claims.Frame,
	}
	gameHub.lock.Lock()
	if old, ok := gameHub.clients[claims.Sub]; ok {
		// 同一昵称以最新连接为准（旧连接被顶掉）
		log.Printf("[ws] %s 已有连接（来自 %s），被新连接 %s 顶替", claims.Sub, old.remote, r.RemoteAddr)
		close(old.send)
		_ = old.conn.Close()
	}
	gameHub.clients[claims.Sub] = c
	// 重连到达：宽限期内取消该昵称的待释放座位标记，座位自动保留
	delete(gameHub.pendingRelease, claims.Sub)
	gameHub.lock.Unlock()

	log.Printf("[ws] %s 加入游戏（已登录=%v 头像=%q 头像框=%q）", claims.Sub, claims.Registered, claims.Avatar, claims.Frame)

	// 快照对全房间一致，无法区分“我是谁”；连接建立后单独告知本连接自己的身份与展示资料
	welcome, _ := json.Marshal(map[string]interface{}{
		"type":    "welcome",
		"nick":    claims.Sub,
		"roleId":  claims.RoleID,
		"avatar":  claims.Avatar,
		"frameId": claims.FrameID,
		"frame":   claims.Frame,
	})
	select {
	case c.send <- welcome:
	default:
	}

	gameHub.report(claims.Sub)
	gameHub.Broadcast()

	go c.writePump()
	s.readPump(c)
}

func (c *client) writePump() {
	for data := range c.send {
		_ = c.conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
		if err := c.conn.WriteMessage(websocket.TextMessage, data); err != nil {
			return
		}
	}
}

func (s *Handler) readPump(c *client) {
	var readErr error
	defer func() {
		stillCurrent := false
		gameHub.lock.Lock()
		if cur, ok := gameHub.clients[c.nick]; ok && cur == c {
			stillCurrent = true
			delete(gameHub.clients, c.nick)
		}
		gameHub.lock.Unlock()
		_ = c.conn.Close()
		if stillCurrent {
			log.Printf("[ws] %s 断开（来自 %s）：%v", c.nick, c.remote, readErr)
			// 不立即删座位：进入宽限期，同昵称重连可恢复；超时未归才释放
			gameHub.markSeatPendingRelease(c.nick)
			gameHub.syncRoomLP()
			gameHub.report(c.nick)
			gameHub.Broadcast()
		} else {
			log.Printf("[ws] %s 旧连接退出（来自 %s）：%v", c.nick, c.remote, readErr)
		}
	}()

	c.conn.SetReadLimit(64 * 1024)
	for {
		_, data, err := c.conn.ReadMessage()
		if err != nil {
			readErr = err
			return
		}
		s.handleClientMessage(c, data)
	}
}

func (s *Handler) handleClientMessage(c *client, data []byte) {
	var msg struct {
		Type  string `json:"type"`
		Color int    `json:"color"`
		X     int    `json:"x"`
		Y     int    `json:"y"`
		Mode  string `json:"mode"`
		// Value 用于布尔型指令，如全屏开关（true=进入全屏/最大化，false=退出全屏）
		Value bool `json:"value"`
	}
	if err := json.Unmarshal(data, &msg); err != nil {
		return
	}

	switch msg.Type {
	case "fullscreen":
		// 由游戏指挥插件房外壳进入/退出全屏；全屏是各客户端本地视图，只回发给发起方
		s.sendShellControl(c, "fullscreen", msg.Value)

	case "sit":
		if msg.Color != ColorBlack && msg.Color != ColorWhite {
			return
		}
		gameHub.lock.Lock()
		// 若该座位有人则先让出
		if cur, ok := gameHub.seats[msg.Color]; ok && cur != c.nick {
			gameHub.lock.Unlock()
			s.sendErr(c, "该座位已有人")
			return
		}
		// 对局中换座会让本局失效
		aborted, result := gameHub.abortGameOnSeatChangeLocked(c.nick)
		// 离开原座位
		for color, seatNick := range gameHub.seats {
			if seatNick == c.nick {
				delete(gameHub.seats, color)
			}
		}
		gameHub.seats[msg.Color] = c.nick
		gameHub.lock.Unlock()
		if aborted {
			s.notifyGameAborted(result)
		}
		gameHub.syncRoomLP()
		gameHub.report(c.nick)
		gameHub.Broadcast()

	case "leave_seat":
		gameHub.lock.Lock()
		// 对局中离座会让本局失去一方，直接中止对局
		aborted, result := gameHub.abortGameOnSeatChangeLocked(c.nick)
		for color, seatNick := range gameHub.seats {
			if seatNick == c.nick {
				delete(gameHub.seats, color)
			}
		}
		gameHub.lock.Unlock()
		if aborted {
			s.notifyGameAborted(result)
		}
		gameHub.syncRoomLP()
		gameHub.report(c.nick)
		gameHub.Broadcast()

	case "start":
		if err := gameHub.StartGame(); err != nil {
			s.sendErr(c, err.Error())
		}

	case "reset":
		if err := gameHub.ResetGame(c.nick); err != nil {
			s.sendErr(c, err.Error())
		}

	case "set_mode":
		if err := gameHub.SetMode(msg.Mode); err != nil {
			s.sendErr(c, err.Error())
		}

	case "place":
		gameHub.lock.Lock()
		game := gameHub.game
		var err error
		if game == nil {
			err = fmt.Errorf("对局尚未开始")
		} else {
			err = game.Place(c.nick, msg.X, msg.Y)
		}
		var finished bool
		var result string
		if game != nil {
			finished = game.Phase == PhaseEnded
			result = game.Result
		}
		if err == nil {
			// 轮到对手，重新开始计时
			gameHub.armTurnTimerLocked()
		}
		gameHub.lock.Unlock()
		if err != nil {
			s.sendErr(c, err.Error())
			return
		}
		if finished {
			s.finishGame(result)
		}
		gameHub.Broadcast()

	case "pass":
		gameHub.lock.Lock()
		game := gameHub.game
		var err error
		if game == nil {
			err = fmt.Errorf("对局尚未开始")
		} else {
			err = game.Pass(c.nick)
		}
		var finished bool
		var result string
		if game != nil {
			finished = game.Phase == PhaseEnded
			result = game.Result
		}
		if err == nil {
			gameHub.armTurnTimerLocked()
		}
		gameHub.lock.Unlock()
		if err != nil {
			s.sendErr(c, err.Error())
			return
		}
		if finished {
			s.finishGame(result)
		}
		gameHub.Broadcast()

	case "resign":
		gameHub.lock.Lock()
		game := gameHub.game
		var err error
		if game == nil {
			err = fmt.Errorf("对局尚未开始")
		} else {
			err = game.Resign(c.nick)
		}
		result := ""
		if game != nil {
			result = game.Result
		}
		if err == nil {
			// 对局已结束，停表
			gameHub.armTurnTimerLocked()
		}
		gameHub.lock.Unlock()
		if err != nil {
			s.sendErr(c, err.Error())
			return
		}
		s.finishGame(result)
		gameHub.Broadcast()
	}
}

// finishGame 终局处理：写公屏事件、同步卡片文案并全量重推状态
func (s *Handler) finishGame(result string) {
	if s.bridge != nil && result != "" {
		s.bridge.EventLog("对局结束：" + result)
	}
	gameHub.syncRoomLP()
	gameHub.reportAll()
}

// notifyGameAborted 对局中止（棋手离座/离房）后的通知：公屏事件 + 卡片文案 + 全量状态重推
func (s *Handler) notifyGameAborted(result string) {
	if s.bridge != nil && result != "" {
		s.bridge.EventLog("对局结束：" + result)
	}
	gameHub.syncRoomLP()
	gameHub.reportAll()
}

// sendShellControl 通过游戏连接向插件房外壳下发外壳控制指令。
// 目前支持 action="fullscreen"：value=true 让外壳最大化（进入全屏），false 退出全屏。
// 外壳收到后用浏览器 Fullscreen API 作用于游戏区域，并通过 FULLSCREEN_CHANGE 回传最终状态。
func (s *Handler) sendShellControl(c *client, action string, value bool) {
	data, _ := json.Marshal(map[string]interface{}{
		"type":   "shell_control",
		"action": action,
		"value":  value,
	})
	select {
	case c.send <- data:
	default:
	}
}

func (s *Handler) sendErr(c *client, msg string) {
	data, _ := json.Marshal(map[string]interface{}{"type": "error", "message": msg})
	select {
	case c.send <- data:
	default:
	}
}

func main() {
	cfg := &Config{}
	// 运行配置（本服务部署参数）
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

	// 接入配置（聊天室下发的凭证与参数）
	integrationPath := defaultIntegrationPath
	if v := os.Getenv("DEMO_INTEGRATION"); v != "" {
		integrationPath = v
	}
	intCfg, err := nichijouSDK.LoadIntegrationConfig(integrationPath)
	if err != nil {
		log.Fatalf("读取接入配置失败 %s: %v", integrationPath, err)
	}
	// integration.json 优先，旧版写在 plugin_config.json 里的兜底
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

	// 优先级：命令行参数 > 环境变量 > 配置文件 > 内置默认值
	flag.StringVar(&cfg.Addr, "addr", pickString(fileCfg.Addr, "DEMO_ADDR", ":8090"), "本服务监听地址")
	flag.StringVar(&cfg.ChatroomBase, "chatroom", pickString(intCfg.Chatroom, "CHATROOM", "http://127.0.0.1:4430"), "聊天室 HTTP 基地址")
	flag.IntVar(&cfg.RoomID, "room-id", pickInt(intCfg.RoomID, "ROOM_ID", 0), "插件房房间号（留空则仅提供页面服务）")
	flag.StringVar(&cfg.IntegrationID, "integration-id", pickString(intCfg.IntegrationID, "INTEGRATION_ID", ""), "接入方 ID（JWT 的 aud）")
	flag.StringVar(&cfg.BridgeSecret, "bridge-secret", pickString(intCfg.BridgeSecret, "BRIDGE_SECRET", ""), "桥接密钥")
	flag.StringVar(&cfg.ExchangeSecret, "exchange-secret", pickString(intCfg.ExchangeSecret, "EXCHANGE_SECRET", ""), "票据兑换凭证")
	flag.Int64Var(&cfg.SecretVersion, "secret-version", int64(pickInt(int(intCfg.SecretVersion), "SECRET_VERSION", 1)), "桥接密钥版本")
	flag.StringVar(&cfg.InstanceID, "instance-id", pickString(fileCfg.InstanceID, "INSTANCE_ID", "demo-1"), "实例标识")
	flag.StringVar(&cfg.PublicKeyPath, "pubkey", pickString(fileCfg.PublicKeyPath, "PUBKEY", "config/plugin_jwt.pub.pem"), "聊天室公钥 PEM 路径")
	flag.IntVar(&cfg.TurnSeconds, "turn-seconds", pickInt(fileCfg.TurnSeconds, "TURN_SECONDS", 60), "每回合思考时限（秒，0 为不计时）")
	flag.Parse()

	bridgeReady := cfg.RoomID != 0 && cfg.IntegrationID != "" && cfg.BridgeSecret != ""

	cfg.ChatroomWSBase = strings.Replace(
		strings.Replace(cfg.ChatroomBase, "https://", "wss://", 1),
		"http://", "ws://", 1,
	)

	// 公钥用于校验聊天室下发的令牌；没有它就无法接受游戏连接
	var pub *rsa.PublicKey
	if loaded, err := nichijouSDK.LoadPublicKey(cfg.PublicKeyPath); err == nil {
		pub = loaded
	} else if bridgeReady {
		log.Fatalf("加载公钥失败: %v", err)
	} else {
		log.Printf("[start] 未加载公钥（%v），游戏连接将不可用", err)
	}
	cfg.PublicKey = pub

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
				states := gameHub.AllStates()
				out := make(map[string]nichijouSDK.State, len(states))
				for nick, state := range states {
					out[nick] = nichijouSDK.State{State: state.State, Text: state.Text}
				}
				return out
			},
			RoomLP:      func() string { return gameHub.roomLP() },
			OnUserLeave: func(nick string) { gameHub.OnUserLeaveRoom(nick) },
			OnChat:      func(nick, text string, timestamp int64) { gameHub.BroadcastChat(nick, text, timestamp) },
		})
	} else {
		missing := make([]string, 0, 3)
		if cfg.RoomID == 0 {
			missing = append(missing, "-room-id（必须填数字）")
		}
		if cfg.IntegrationID == "" {
			missing = append(missing, "-integration-id")
		}
		if cfg.BridgeSecret == "" {
			missing = append(missing, "-bridge-secret")
		}
		log.Printf("[start] 仅提供页面服务（不连桥接通道），缺少：%s", strings.Join(missing, "、"))
		log.Printf("[start] 已收到的参数：room-id=%d integration-id=%q bridge-secret长度=%d",
			cfg.RoomID, cfg.IntegrationID, len(cfg.BridgeSecret))
	}

	gameHub = NewHub(bridge)
	gameHub.turnSeconds = cfg.TurnSeconds
	if bridge != nil {
		go bridge.Run()
	}

	handler := &Handler{cfg: cfg, pub: pub, bridge: bridge}

	mux := http.NewServeMux()
	// 页面资源自动选择：
	//   磁盘存在 ./public      -> 从磁盘读取（本地开发改页面即时生效，无需重启）
	//   磁盘不存在 ./public    -> 用编译时内嵌的资源（发布单文件二进制）
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
	mux.Handle("/", staticHandler)
	mux.HandleFunc("/ws", handler.ServeWS)
	mux.Handle("/exchange", nichijouSDK.NewExchangeHandler(nichijouSDK.ExchangeConfig{
		ChatroomBase:   cfg.ChatroomBase,
		ExchangeSecret: cfg.ExchangeSecret,
	}))

	if bridgeReady {
		log.Printf("围棋插件 Demo 启动于 %s", cfg.Addr)
		log.Printf("  房间号   room-id          = %d", cfg.RoomID)
		log.Printf("  接入方   integration-id   = %s", cfg.IntegrationID)
		log.Printf("  聊天室   chatroom         = %s", cfg.ChatroomBase)
		log.Printf("  玩法     默认%s，可切换%s · 棋盘 %d/%d 路固定 · 每回合 %d 秒",
			ModeLabel(ModeGo), ModeLabel(ModeGomoku), BoardSizeStandard, BoardSizeGomoku, cfg.TurnSeconds)
	} else {
		log.Printf("围棋插件 Demo 启动于 %s（页面模式，等待配置房间参数后再启用联机）", cfg.Addr)
	}
	if err := http.ListenAndServe(cfg.Addr, mux); err != nil {
		log.Fatal(err)
	}
}
