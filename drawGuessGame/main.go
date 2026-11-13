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

	"github.com/BlackCarDriver/nichijouSDK"
	"github.com/gorilla/websocket"
)

//go:embed public/*
var publicFS embed.FS

// Config 运行配置
type Config struct {
	// Addr 本服务监听地址
	Addr string
	// ChatroomBase 聊天室 HTTP 基地址
	ChatroomBase string
	// ChatroomWSBase 聊天室 WebSocket 基地址
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
	// InstanceID 实例标识
	InstanceID string
	// PublicKeyPath 聊天室公钥 PEM
	PublicKeyPath string
	// PublicKey 已加载的公钥
	PublicKey *rsa.PublicKey

	// RoundSeconds 绘画回合时长
	RoundSeconds int
	// SelectSeconds 选题时长
	SelectSeconds int
	// ResultSeconds 回合结算展示时长
	ResultSeconds int
	// TotalRounds 单局总回合数（0=每人一次）
	TotalRounds int
	// MaxPlayers 单局最大参与人数
	MaxPlayers int
}

// state 用户状态（上报聊天室）
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
	// 展示资料（JWT 声明）
	roleID  int
	avatar  string
	frameID int
	frame   string
	// 进房序号：决定画手轮换起点与参与资格
	enterSeq int
	// 已准备参与下一局（用户主动点准备；游戏中进房者先观战）
	ready bool
}

// Hub 房间内所有连接与游戏状态
type Hub struct {
	lock     sync.Mutex
	clients  map[string]*client
	game     *Game
	bridge   *nichijouSDK.Bridge
	enterSeq int
	started  bool // 是否有过游戏（用于首次进房即开始的判断）

	// 按昵称持久化的参与状态（断线重连继承，离开房间时清除）：
	// enterSeqByNick 保证房主身份（进房最早者）在重连后不变；
	// readyByNick 保证准备状态在重连后不丢失。
	enterSeqByNick map[string]int
	readyByNick    map[string]bool

	// 定时器
	selectTimer *time.Timer
	roundTimer  *time.Timer
	resultTimer *time.Timer
	// 计时器代号：每次重置自增，旧计时器回调据此自废
	timerSeq int64

	// 回合时长配置
	roundSeconds  int
	selectSeconds int
	resultSeconds int
	totalRounds   int
	maxPlayers    int

	// 公屏事件文案队列（最近 N 条，供新进房者展示）
	events []string
}

var gameHub = NewHub(nil)

// NewHub 创建 hub
func NewHub(bridge *nichijouSDK.Bridge) *Hub {
	return &Hub{
		clients:        map[string]*client{},
		enterSeqByNick: map[string]int{},
		readyByNick:    map[string]bool{},
		roundSeconds:   60,
		selectSeconds:  10,
		resultSeconds:  8,
		totalRounds:    0,
		maxPlayers:     12,
		bridge:         bridge,
	}
}

// SetBridge 注入桥接客户端
func (h *Hub) SetBridge(b *nichijouSDK.Bridge) {
	h.lock.Lock()
	h.bridge = b
	h.lock.Unlock()
}

// SetTiming 设置回合时长配置（启动时调用）
func (h *Hub) SetTiming(round, selectS, resultS, rounds, maxPlayers int) {
	h.lock.Lock()
	if round > 0 {
		h.roundSeconds = round
	}
	if selectS > 0 {
		h.selectSeconds = selectS
	}
	if resultS > 0 {
		h.resultSeconds = resultS
	}
	h.totalRounds = rounds
	if maxPlayers > 0 {
		h.maxPlayers = maxPlayers
	}
	h.lock.Unlock()
}

// ---------- 状态上报 ----------

// AllStates 全量状态快照
func (h *Hub) AllStates() map[string]*state {
	h.lock.Lock()
	defer h.lock.Unlock()
	out := make(map[string]*state, len(h.clients))
	for nick := range h.clients {
		out[nick] = h.stateOfLocked(nick)
	}
	return out
}

// stateOfLocked 计算某用户当前状态（调用方需持锁）
func (h *Hub) stateOfLocked(nick string) *state {
	if h.game == nil {
		return &state{State: "waiting", Text: "等待开局"}
	}
	if h.game.Phase == PhasePlaying || h.game.Phase == PhaseSelect {
		if nick == h.game.Drawer {
			return &state{State: "playing", Text: "作画中"}
		}
		if p, ok := h.game.Players[nick]; ok && p != nil {
			if p.Answered {
				return &state{State: "playing", Text: "已答对"}
			}
			return &state{State: "playing", Text: "猜词中"}
		}
		return &state{State: "spectating", Text: "观战中"}
	}
	if h.game.Phase == PhaseRoundEnd || h.game.Phase == PhaseGameOver {
		if p, ok := h.game.Players[nick]; ok && p != nil {
			return &state{State: "waiting", Text: "回合结算"}
		}
		return &state{State: "spectating", Text: "观战中"}
	}
	return &state{State: "waiting", Text: "等待开局"}
}

// report 某用户状态变化后重推全量状态（合并式全量重推）
func (h *Hub) report(nick string) {
	h.reportAll()
}

func (h *Hub) reportAll() {
	if h.bridge != nil {
		h.bridge.ScheduleFullReport()
	}
}

// ---------- 快照 ----------

// hostNickLocked 当前房主昵称：在线用户中进房最早者（调用方需持锁）
func (h *Hub) hostNickLocked() string {
	var host string
	minSeq := 1 << 62
	for nick, c := range h.clients {
		if c.enterSeq < minSeq {
			minSeq = c.enterSeq
			host = nick
		}
	}
	return host
}

// hostNick 当前房主昵称（调用方无需持锁）
func (h *Hub) hostNick() string {
	h.lock.Lock()
	defer h.lock.Unlock()
	return h.hostNickLocked()
}

// snapshot 生成下发给前端的完整快照
func (h *Hub) snapshot() map[string]interface{} {
	h.lock.Lock()
	defer h.lock.Unlock()

	users := make([]map[string]interface{}, 0, len(h.clients))
	for nick, c := range h.clients {
		st := h.stateOfLocked(nick)
		users = append(users, map[string]interface{}{
			"nick":    nick,
			"state":   st.State,
			"text":    st.Text,
			"roleId":  c.roleID,
			"avatar":  c.avatar,
			"frameId": c.frameID,
			"frame":   c.frame,
			"ready":   c.ready,
		})
	}
	sort.Slice(users, func(i, j int) bool {
		return users[i]["nick"].(string) < users[j]["nick"].(string)
	})

	var game map[string]interface{}
	if h.game != nil {
		g := h.game
		game = map[string]interface{}{
			"phase":          g.Phase,
			"drawer":         g.Drawer,
			"word":           g.Word,
			"wordHint":       g.WordHint,
			"wordCategory":   g.WordCategory,
			"options":        g.Options,
			"round":          g.Round,
			"totalRounds":    g.TotalRounds,
			"selectDeadline": g.SelectDeadline,
			"roundDeadline":  g.RoundDeadline,
			"resultDeadline": g.ResultDeadline,
			"result":         g.Result,
			"ranking":        g.Ranking,
			"winner":         g.Winner,
			"scoreboard":     g.playerScoreSummary(),
		}
	}

	return map[string]interface{}{
		"type":      "state",
		"users":     users,
		"game":      game,
		"host":      h.hostNickLocked(),
		"serverNow": time.Now().UnixMilli(),
		"canvas":    h.canvasSnapshotLocked(),
		"events":    h.events,
		"timing": map[string]interface{}{
			"round":       h.roundSeconds,
			"select":      h.selectSeconds,
			"totalRounds": h.totalRounds, // 房间级回合设置（0=每人一次画手），仅房主可改
		},
	}
}

// canvasSnapshotLocked 当前画布动作列表（新进房者重放用，调用方需持锁）
func (h *Hub) canvasSnapshotLocked() []DrawAction {
	if h.game == nil {
		return nil
	}
	out := make([]DrawAction, len(h.game.CanvasActions))
	copy(out, h.game.CanvasActions)
	return out
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

// broadcastRaw 广播原始 JSON 数据（绘画动作等高频消息，不走全量快照）
func (h *Hub) broadcastRaw(payload []byte) {
	h.lock.Lock()
	clients := make([]*client, 0, len(h.clients))
	for _, c := range h.clients {
		clients = append(clients, c)
	}
	h.lock.Unlock()
	for _, c := range clients {
		select {
		case c.send <- payload:
		default:
		}
	}
}

// addEvent 记录公屏事件文案（服务端事件历史）
func (h *Hub) addEvent(text string) {
	h.lock.Lock()
	h.events = append(h.events, text)
	if len(h.events) > 30 {
		h.events = h.events[len(h.events)-30:]
	}
	h.lock.Unlock()
	if h.bridge != nil {
		h.bridge.EventLog(text)
	}
}

// ---------- 聊天（猜词判定） ----------

// OnChatFromBridge 聊天室消息到达：转发给游戏页 + 答题判定
func (h *Hub) OnChatFromBridge(nick, text string, ts int64) {
	h.lock.Lock()
	avatar, frame := "", ""
	if c, ok := h.clients[nick]; ok {
		avatar, frame = c.avatar, c.frame
	}
	// 答题判定
	guessed := false
	score := 0
	if h.game != nil && h.game.Phase == PhasePlaying {
		if ok, s, _ := h.game.TryGuess(nick, text); ok {
			guessed = true
			score = s
		}
	}
	// 防剧透：绘画阶段，聊天内容中的答案文字打码（对所有人，含画手——防止画手打字带出答案）。
	// 答对判定在打码前完成，正确答案消息发送者仍正常判定。
	shownText := text
	if h.game != nil && h.game.Phase == PhasePlaying && h.game.Word != "" {
		shownText = maskAnswer(text, h.game.Word)
	}
	clients := make([]*client, 0, len(h.clients))
	for _, c := range h.clients {
		clients = append(clients, c)
	}
	h.lock.Unlock()

	// 转发聊天到游戏页（带打码后的文本与是否答对标记）
	data, _ := json.Marshal(map[string]interface{}{
		"type":      "chat",
		"nick":      nick,
		"text":      shownText,
		"avatar":    avatar,
		"frame":     frame,
		"timestamp": ts,
		"guessed":   guessed,
		"score":     score,
	})
	for _, c := range clients {
		select {
		case c.send <- data:
		default:
		}
	}

	if guessed {
		// 答对：全量重推（得分/状态变化）
		h.lock.Lock()
		g := h.game
		allDone := g.AllAnswered()
		h.lock.Unlock()
		h.addEvent(fmt.Sprintf("🎉 %s 答对了（+%d分）", nick, score))
		if allDone {
			h.finishRound("全部答对")
		} else {
			h.reportAll()
			h.Broadcast()
		}
	}
}

// ---------- 离房 ----------

// OnUserLeaveRoom 聊天室通知该用户已离开房间
func (h *Hub) OnUserLeaveRoom(nick string) {
	h.lock.Lock()
	if c, ok := h.clients[nick]; ok {
		close(c.send)
		_ = c.conn.Close()
		delete(h.clients, nick)
	}
	// 真正离开房间：清除按昵称持久化的参与状态（断线重连则保留，由 readPump 处理）
	delete(h.enterSeqByNick, nick)
	delete(h.readyByNick, nick)
	h.lock.Unlock()

	h.addEvent(fmt.Sprintf("【%s】离开了房间", nick))
	h.handlePlayerDrop(nick)
	h.reportAll()
	h.Broadcast()
}

// handlePlayerDrop 玩家离开/断开：若其正在参与游戏则处理（画手离开则结束回合；玩家离开不整局作废，剩余玩家继续）
// handlePlayerDrop 玩家离开/断开：若其正在参与游戏则处理。
// 画手离开则结束当前回合；玩家离开不整局作废，剩余玩家继续；不足 2 人则中止回等待。
func (h *Hub) handlePlayerDrop(nick string) {
	h.lock.Lock()
	// 房间已无人在线：彻底重置游戏状态（清空残留的旧局/玩家列表）
	if len(h.clients) == 0 && h.game != nil {
		h.armTimerLocked()
		h.game = nil
		h.lock.Unlock()
		h.syncRoomLP()
		h.reportAll()
		h.Broadcast()
		return
	}
	if h.game == nil || h.game.Phase == PhaseGameOver || h.game.Phase == PhaseWaiting {
		h.lock.Unlock()
		return
	}
	if _, ok := h.game.Players[nick]; !ok {
		h.lock.Unlock()
		return
	}
	// 从玩家列表移除
	delete(h.game.Players, nick)
	// 从轮换顺序移除
	newOrder := make([]string, 0, len(h.game.PlayerOrder))
	for _, pn := range h.game.PlayerOrder {
		if pn != nick {
			newOrder = append(newOrder, pn)
		}
	}
	h.game.PlayerOrder = newOrder

	drawerLeft := h.game.Drawer == nick
	tooFew := len(h.game.Players) < 2

	// 玩家不足 2 人：整局直接中止回等待
	if tooFew {
		h.armTimerLocked()
		g := h.game
		g.Reset()
		h.lock.Unlock()

		h.addEvent(fmt.Sprintf("%s 离开，剩余玩家不足，游戏结束", nick))
		h.syncRoomLP()
		h.reportAll()
		h.Broadcast()
		return
	}

	// 画手离开但仍有足够玩家：进入结算展示，正常轮换
	if drawerLeft {
		h.armTimerLocked()
		h.lock.Unlock()

		h.finishRound(fmt.Sprintf("%s 离开房间，本回合结束", nick))
		return
	}
	h.lock.Unlock()

	// 非画手离开：游戏继续，重推状态即可
	h.syncRoomLP()
	h.reportAll()
	h.Broadcast()
}

// ---------- 游戏流程 ----------

// StartGame 开始游戏：仅房主可操作，参与玩家 = 所有已准备玩家
func (h *Hub) StartGame(nick string) error {
	h.lock.Lock()
	if h.game != nil && h.game.Phase != PhaseWaiting && h.game.Phase != PhaseGameOver {
		h.lock.Unlock()
		return fmt.Errorf("游戏正在进行中")
	}
	if nick != h.hostNickLocked() {
		h.lock.Unlock()
		return fmt.Errorf("只有房主 %s 可以开始游戏", h.hostNick())
	}
	nicks := h.readyPlayersLocked()
	if len(nicks) < 2 {
		h.lock.Unlock()
		return fmt.Errorf("至少需要 2 名玩家准备后才能开始")
	}
	if len(nicks) > h.maxPlayers {
		nicks = nicks[:h.maxPlayers]
	}
	h.game = NewGame(nicks, h.totalRounds)
	h.armTimerLocked()
	drawer := h.game.Drawer
	h.lock.Unlock()

	h.addEvent(fmt.Sprintf("🎨 游戏开始！本轮画手：%s", drawer))
	h.syncRoomLP()
	h.reportAll()
	h.Broadcast()
	return nil
}

// readyPlayersLocked 收集所有已准备用户昵称（按进房序，调用方需持锁）
func (h *Hub) readyPlayersLocked() []string {
	nicks := make([]string, 0, len(h.clients))
	for nick, c := range h.clients {
		if c.ready {
			nicks = append(nicks, nick)
		}
	}
	sort.Slice(nicks, func(i, j int) bool {
		return h.clients[nicks[i]].enterSeq < h.clients[nicks[j]].enterSeq
	})
	return nicks
}

// SetReady 用户准备 / 取消准备（任意阶段可点：等待阶段准备参与下一局，
// 游戏进行中观战者也可准备，下局开始自动加入）。
func (h *Hub) SetReady(nick string, ready bool) error {
	h.lock.Lock()
	c, ok := h.clients[nick]
	if !ok {
		h.lock.Unlock()
		return fmt.Errorf("你不在房间中")
	}
	// 游戏中已是本局玩家时不允许改准备状态（本局已锁定）
	if h.game != nil && h.game.Phase != PhaseWaiting && h.game.Phase != PhaseGameOver {
		if p, inGame := h.game.Players[nick]; inGame && p != nil {
			h.lock.Unlock()
			return fmt.Errorf("本局进行中，不能修改准备状态")
		}
	}
	if c.ready == ready {
		h.lock.Unlock()
		return nil
	}
	c.ready = ready
	h.readyByNick[nick] = ready
	h.lock.Unlock()

	if ready {
		h.addEvent(fmt.Sprintf("【%s】已准备", nick))
	} else {
		h.addEvent(fmt.Sprintf("【%s】取消准备", nick))
	}
	h.syncRoomLP()
	h.reportAll()
	h.Broadcast()
	return nil
}

// SetRounds 设置一局的总回合数（仅房主，非游戏进行中可改）。
// rounds<=0 表示每人当一次画手；上限 maxRoundsPerGame。
func (h *Hub) SetRounds(nick string, rounds int) error {
	const maxRoundsPerGame = 30
	h.lock.Lock()
	if nick != h.hostNickLocked() {
		h.lock.Unlock()
		return fmt.Errorf("只有房主 %s 可以设置回合数", h.hostNick())
	}
	if h.game != nil && h.game.Phase != PhaseWaiting && h.game.Phase != PhaseGameOver {
		h.lock.Unlock()
		return fmt.Errorf("游戏进行中，不能修改回合数")
	}
	if rounds < 0 || rounds > maxRoundsPerGame {
		h.lock.Unlock()
		return fmt.Errorf("回合数需在 0~%d 之间（0 表示每人一次）", maxRoundsPerGame)
	}
	h.totalRounds = rounds
	h.lock.Unlock()

	if rounds <= 0 {
		h.addEvent(fmt.Sprintf("⚙️ 房主 %s 将回合数设为「每人一次画手」", nick))
	} else {
		h.addEvent(fmt.Sprintf("⚙️ 房主 %s 将回合数设为 %d 回合", nick, rounds))
	}
	h.syncRoomLP()
	h.reportAll()
	h.Broadcast()
	return nil
}

// SelectWord 画手选题
func (h *Hub) SelectWord(nick, word string) error {
	h.lock.Lock()
	if h.game == nil {
		h.lock.Unlock()
		return fmt.Errorf("游戏尚未开始")
	}
	err := h.game.SelectWord(nick, word)
	if err != nil {
		h.lock.Unlock()
		return err
	}
	// 开画：设定绘画截止
	picked := h.game.Word
	h.game.StartRound(h.roundSeconds)
	h.armTimerLocked()
	h.lock.Unlock()

	h.addEvent(fmt.Sprintf("✏️ %s 开始作画，提示：%s", nick, wordLengthHint(picked)))
	h.syncRoomLP()
	h.reportAll()
	h.Broadcast()
	return nil
}

// finishRound 结束当前回合，进入结算展示阶段（超时/全部答对/画手离开/画手放弃）
func (h *Hub) finishRound(cause string) {
	h.lock.Lock()
	if h.game == nil || h.game.Phase != PhasePlaying {
		h.lock.Unlock()
		return
	}
	g := h.game
	word := g.Word
	// 日志：回合因何结束、剩余时间（排查偶现的“回合提前结束”）
	remain := (g.RoundDeadline - time.Now().UnixMilli()) / 1000
	log.Printf("[round] 回合结束 cause=%q round=%d 剩余=%ds deadline=%d now=%d", cause, g.Round, remain, g.RoundDeadline, time.Now().UnixMilli())
	g.EndRound(cause)
	h.armTimerLocked()
	h.lock.Unlock()

	h.addEvent(fmt.Sprintf("🔁 回合结束（%s），正确答案：%s", cause, word))
	h.syncRoomLP()
	h.reportAll()
	h.Broadcast()
}

// SkipRound 画手放弃回合（直接结束回合并展示答案）
func (h *Hub) SkipRound(nick string) error {
	h.lock.Lock()
	if h.game == nil || h.game.Phase != PhasePlaying {
		h.lock.Unlock()
		return fmt.Errorf("当前不在绘画阶段")
	}
	if nick != h.game.Drawer {
		h.lock.Unlock()
		return fmt.Errorf("只有画手可以放弃回合")
	}
	h.lock.Unlock()
	h.finishRound(fmt.Sprintf("%s 放弃了本回合", nick))
	return nil
}

// RestartGame 整局结束后再来一局（仅房主可操作，参与玩家 = 已准备用户）
func (h *Hub) RestartGame(nick string) error {
	h.lock.Lock()
	if h.game == nil || h.game.Phase != PhaseGameOver {
		h.lock.Unlock()
		return fmt.Errorf("当前没有已结束的游戏")
	}
	if nick != h.hostNickLocked() {
		h.lock.Unlock()
		return fmt.Errorf("只有房主 %s 可以开始游戏", h.hostNick())
	}
	nicks := h.readyPlayersLocked()
	if len(nicks) < 2 {
		h.lock.Unlock()
		return fmt.Errorf("至少需要 2 名玩家准备后才能开始")
	}
	h.game = NewGame(nicks, h.totalRounds)
	h.armTimerLocked()
	drawer := h.game.Drawer
	h.lock.Unlock()

	h.addEvent(fmt.Sprintf("🎨 新一轮游戏开始！本轮画手：%s", drawer))
	h.syncRoomLP()
	h.reportAll()
	h.Broadcast()
	return nil
}

// ---------- 画布操作 ----------

// AppendDraw 画手绘画动作：记录 + 广播
func (h *Hub) AppendDraw(nick string, act DrawAction) {
	h.lock.Lock()
	if h.game == nil || h.game.Phase != PhasePlaying {
		h.lock.Unlock()
		return
	}
	if nick != h.game.Drawer {
		h.lock.Unlock()
		return
	}
	if act.Tool == "" {
		h.lock.Unlock()
		return
	}
	// 校验点数量，防止单帧过大（先截断再记录）
	if len(act.Points) > 2000 {
		act.Points = act.Points[:2000]
	}
	h.game.AppendAction(act)
	h.lock.Unlock()

	data, _ := json.Marshal(map[string]interface{}{
		"type":   "draw",
		"nick":   nick,
		"action": act,
	})
	h.broadcastRaw(data)
}

// ClearCanvas 画手清空画布
func (h *Hub) ClearCanvas(nick string) error {
	h.lock.Lock()
	if h.game == nil || h.game.Phase != PhasePlaying {
		h.lock.Unlock()
		return fmt.Errorf("当前不在绘画阶段")
	}
	if nick != h.game.Drawer {
		h.lock.Unlock()
		return fmt.Errorf("只有画手可以操作画布")
	}
	h.game.ClearCanvas()
	h.lock.Unlock()

	data, _ := json.Marshal(map[string]interface{}{"type": "clear_canvas", "nick": nick})
	h.broadcastRaw(data)
	return nil
}

// UndoCanvas 画手撤销最后一笔
func (h *Hub) UndoCanvas(nick string) error {
	h.lock.Lock()
	if h.game == nil || h.game.Phase != PhasePlaying {
		h.lock.Unlock()
		return fmt.Errorf("当前不在绘画阶段")
	}
	if nick != h.game.Drawer {
		h.lock.Unlock()
		return fmt.Errorf("只有画手可以操作画布")
	}
	if _, ok := h.game.UndoAction(); !ok {
		h.lock.Unlock()
		return fmt.Errorf("没有可撤销的动作")
	}
	h.lock.Unlock()

	data, _ := json.Marshal(map[string]interface{}{"type": "undo_canvas", "nick": nick})
	h.broadcastRaw(data)
	return nil
}

// ---------- 计时器 ----------

// armTimerLocked 重置当前阶段计时器（调用方需持锁）
func (h *Hub) armTimerLocked() {
	h.timerSeq++
	seq := h.timerSeq
	if h.selectTimer != nil {
		h.selectTimer.Stop()
		h.selectTimer = nil
	}
	if h.roundTimer != nil {
		h.roundTimer.Stop()
		h.roundTimer = nil
	}
	if h.resultTimer != nil {
		h.resultTimer.Stop()
		h.resultTimer = nil
	}
	g := h.game
	if g == nil {
		return
	}
	switch g.Phase {
	case PhaseSelect:
		g.SelectDeadline = time.Now().Add(time.Duration(h.selectSeconds) * time.Second).UnixMilli()
		h.selectTimer = time.AfterFunc(time.Duration(h.selectSeconds)*time.Second, func() {
			h.onSelectTimeout(seq)
		})
	case PhasePlaying:
		g.SelectDeadline = 0
		// 绘画截止已在 StartRound 设定；这里只挂超时回调
		remain := g.RoundDeadline - time.Now().UnixMilli()
		if remain > 0 {
			h.roundTimer = time.AfterFunc(time.Duration(remain)*time.Millisecond, func() {
				h.onRoundTimeout(seq)
			})
		}
	case PhaseRoundEnd:
		g.RoundDeadline = 0
		g.ResultDeadline = time.Now().Add(time.Duration(h.resultSeconds) * time.Second).UnixMilli()
		h.resultTimer = time.AfterFunc(time.Duration(h.resultSeconds)*time.Second, func() {
			h.onResultTimeout(seq)
		})
	default:
		g.SelectDeadline = 0
		g.RoundDeadline = 0
		g.ResultDeadline = 0
	}
}

// onSelectTimeout 选题超时：随机选一个词自动开始
func (h *Hub) onSelectTimeout(seq int64) {
	h.lock.Lock()
	if seq != h.timerSeq || h.game == nil || h.game.Phase != PhaseSelect {
		h.lock.Unlock()
		return
	}
	word := ""
	if len(h.game.Options) > 0 {
		word = h.game.Options[0]
	}
	h.lock.Unlock()
	if word == "" {
		return
	}
	// 复用选题逻辑
	h.lock.Lock()
	_ = h.game.SelectWord(h.game.Drawer, word)
	h.game.StartRound(h.roundSeconds)
	h.armTimerLocked()
	drawer := h.game.Drawer
	h.lock.Unlock()
	h.addEvent(fmt.Sprintf("⏰ 画手 %s 选题超时，已自动开始（%s）", drawer, wordLengthHint(word)))
	h.syncRoomLP()
	h.reportAll()
	h.Broadcast()
}

// onRoundTimeout 绘画超时：结束回合进入结算
func (h *Hub) onRoundTimeout(seq int64) {
	h.lock.Lock()
	if seq != h.timerSeq || h.game == nil || h.game.Phase != PhasePlaying {
		h.lock.Unlock()
		return
	}
	// 日志：回合为何在此刻结束（排查偶现的“回合提前结束”）
	log.Printf("[timer] 回合超时触发 seq=%d now=%d roundSeconds=%d", seq, time.Now().UnixMilli(), h.roundSeconds)
	h.lock.Unlock()
	h.finishRound("时间到")
}

// onResultTimeout 结算展示结束：进入下一轮选题或整局结束
func (h *Hub) onResultTimeout(seq int64) {
	h.lock.Lock()
	if seq != h.timerSeq || h.game == nil || h.game.Phase != PhaseRoundEnd {
		h.lock.Unlock()
		return
	}
	g := h.game
	over := g.AdvanceRound()
	h.armTimerLocked()
	h.lock.Unlock()

	if over {
		h.addEvent(fmt.Sprintf("🏁 游戏结束！冠军：%s", g.Winner))
	}
	h.syncRoomLP()
	h.reportAll()
	h.Broadcast()
}

// ---------- 房间卡片文案 ----------

// roomLP 计算卡片副文案
func (h *Hub) roomLP() string {
	h.lock.Lock()
	defer h.lock.Unlock()
	return h.roomLPLocked()
}

func (h *Hub) roomLPLocked() string {
	if h.game == nil || h.game.Phase == PhaseWaiting {
		return "画猜房待开始"
	}
	switch h.game.Phase {
	case PhaseSelect:
		return "画手选题中"
	case PhasePlaying:
		return fmt.Sprintf("作画中-%s", h.game.Drawer)
	case PhaseRoundEnd:
		return "回合结算中"
	case PhaseGameOver:
		return "游戏已结束"
	}
	return "画猜房待开始"
}

// syncRoomLP 向聊天室同步一次卡片副文案
func (h *Hub) syncRoomLP() {
	if h.bridge != nil {
		h.bridge.SetRoomLP(h.roomLP())
	}
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
		send:    make(chan []byte, 128),
		remote:  r.RemoteAddr,
		roleID:  claims.RoleID,
		avatar:  claims.Avatar,
		frameID: claims.FrameID,
		frame:   claims.Frame,
	}
	gameHub.lock.Lock()
	if old, ok := gameHub.clients[claims.Sub]; ok {
		log.Printf("[ws] %s 已有连接（来自 %s），被新连接 %s 顶替", claims.Sub, old.remote, r.RemoteAddr)
		close(old.send)
		_ = old.conn.Close()
	}
	// 进房序号与准备状态按昵称持久化：断线/刷新重连后房主身份与准备状态保持不变
	if seq, ok := gameHub.enterSeqByNick[claims.Sub]; ok {
		c.enterSeq = seq
	} else {
		gameHub.enterSeq++
		c.enterSeq = gameHub.enterSeq
		gameHub.enterSeqByNick[claims.Sub] = c.enterSeq
	}
	c.ready = gameHub.readyByNick[claims.Sub]
	gameHub.clients[claims.Sub] = c
	gameHub.lock.Unlock()

	log.Printf("[ws] %s 加入游戏（已登录=%v 头像=%q 头像框=%q）", claims.Sub, claims.Registered, claims.Avatar, claims.Frame)

	// 告知本连接身份
	welcome, _ := json.Marshal(map[string]interface{}{
		"type":    "welcome",
		"nick":    claims.Sub,
		"roleId":  claims.RoleID,
		"avatar":  claims.Avatar,
		"frameId": claims.FrameID,
		"frame":   claims.Frame,
		"ready":   c.ready,
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
			gameHub.handlePlayerDrop(c.nick)
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
		Type string `json:"type"`
		Word string `json:"word"`
		Tool string `json:"tool"`
		// DrawAction 绘画动作
		Action DrawAction `json:"action"`
		// 全屏
		Value bool `json:"value"`
		// 回合数设置
		Rounds int `json:"rounds"`
	}
	if err := json.Unmarshal(data, &msg); err != nil {
		return
	}

	switch msg.Type {
	case "fullscreen":
		s.sendShellControl(c, "fullscreen", msg.Value)

	case "start":
		if err := gameHub.StartGame(c.nick); err != nil {
			s.sendErr(c, err.Error())
		}

	case "restart":
		if err := gameHub.RestartGame(c.nick); err != nil {
			s.sendErr(c, err.Error())
		}

	case "ready":
		if err := gameHub.SetReady(c.nick, true); err != nil {
			s.sendErr(c, err.Error())
		}

	case "cancel_ready":
		if err := gameHub.SetReady(c.nick, false); err != nil {
			s.sendErr(c, err.Error())
		}

	case "set_rounds":
		if err := gameHub.SetRounds(c.nick, msg.Rounds); err != nil {
			s.sendErr(c, err.Error())
		}

	case "select":
		if err := gameHub.SelectWord(c.nick, msg.Word); err != nil {
			s.sendErr(c, err.Error())
		}

	case "skip":
		if err := gameHub.SkipRound(c.nick); err != nil {
			s.sendErr(c, err.Error())
		}

	case "draw":
		gameHub.AppendDraw(c.nick, msg.Action)

	case "clear_canvas":
		if err := gameHub.ClearCanvas(c.nick); err != nil {
			s.sendErr(c, err.Error())
		}

	case "undo_canvas":
		if err := gameHub.UndoCanvas(c.nick); err != nil {
			s.sendErr(c, err.Error())
		}
	}
}

// sendShellControl 通过游戏连接向插件房外壳下发外壳控制指令（全屏）
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
	// 运行配置
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

	// 接入配置
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

	// 词库加载
	wordsPath := defaultWordsPath
	if v := os.Getenv("DEMO_WORDS"); v != "" {
		wordsPath = v
	}
	if fileCfg.WordsPath != "" {
		wordsPath = fileCfg.WordsPath
	}
	if err := LoadWords(wordsPath); err != nil {
		log.Fatalf("加载词库失败 %s: %v", wordsPath, err)
	}
	log.Printf("[start] 词库已加载 %s（%d 个分类，%d 个词）", wordsPath, len(library.Categories()), libraryCount())

	// 优先级：命令行参数 > 环境变量 > 配置文件 > 内置默认值
	flag.StringVar(&cfg.Addr, "addr", pickString(fileCfg.Addr, "DEMO_ADDR", ":8091"), "本服务监听地址")
	flag.StringVar(&cfg.ChatroomBase, "chatroom", pickString(intCfg.Chatroom, "CHATROOM", "http://127.0.0.1:4430"), "聊天室 HTTP 基地址")
	flag.IntVar(&cfg.RoomID, "room-id", pickInt(intCfg.RoomID, "ROOM_ID", 0), "插件房房间号（留空则仅提供页面服务）")
	flag.StringVar(&cfg.IntegrationID, "integration-id", pickString(intCfg.IntegrationID, "INTEGRATION_ID", ""), "接入方 ID（JWT 的 aud）")
	flag.StringVar(&cfg.BridgeSecret, "bridge-secret", pickString(intCfg.BridgeSecret, "BRIDGE_SECRET", ""), "桥接密钥")
	flag.StringVar(&cfg.ExchangeSecret, "exchange-secret", pickString(intCfg.ExchangeSecret, "EXCHANGE_SECRET", ""), "票据兑换凭证")
	flag.Int64Var(&cfg.SecretVersion, "secret-version", int64(pickInt(int(intCfg.SecretVersion), "SECRET_VERSION", 1)), "桥接密钥版本")
	flag.StringVar(&cfg.InstanceID, "instance-id", pickString(fileCfg.InstanceID, "INSTANCE_ID", "draw-1"), "实例标识")
	flag.StringVar(&cfg.PublicKeyPath, "pubkey", pickString(fileCfg.PublicKeyPath, "PUBKEY", "config/plugin_jwt.pub.pem"), "聊天室公钥 PEM 路径")
	flag.IntVar(&cfg.RoundSeconds, "round-seconds", pickInt(fileCfg.RoundSeconds, "ROUND_SECONDS", 60), "绘画回合时长（秒）")
	flag.IntVar(&cfg.SelectSeconds, "select-seconds", pickInt(fileCfg.SelectSeconds, "SELECT_SECONDS", 10), "选题时长（秒）")
	flag.IntVar(&cfg.ResultSeconds, "result-seconds", pickInt(fileCfg.ResultSeconds, "RESULT_SECONDS", 8), "回合结算展示时长（秒）")
	flag.IntVar(&cfg.TotalRounds, "total-rounds", pickInt(fileCfg.TotalRounds, "TOTAL_ROUNDS", 0), "单局总回合数（0=每人当一次画手）")
	flag.IntVar(&cfg.MaxPlayers, "max-players", pickInt(fileCfg.MaxPlayers, "MAX_PLAYERS", 12), "单局最大参与人数")
	// 日志自动裁剪：run.sh 会把输出追加到日志文件，这里定期把超限的旧内容裁掉
	logFilePath := pickString("", "LOG_FILE", "")
	logMaxKB := pickInt(0, "LOG_MAX_KB", 200)
	flag.StringVar(&logFilePath, "log-file", logFilePath, "日志文件路径（用于自动裁剪，留空则不裁剪）")
	flag.IntVar(&logMaxKB, "log-max-kb", logMaxKB, "日志文件超过该大小（KB）时裁剪旧内容")
	flag.Parse()

	bridgeReady := cfg.RoomID != 0 && cfg.IntegrationID != "" && cfg.BridgeSecret != ""

	cfg.ChatroomWSBase = strings.Replace(
		strings.Replace(cfg.ChatroomBase, "https://", "wss://", 1),
		"http://", "ws://", 1,
	)

	// 公钥
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
			OnChat:      func(nick, text string, timestamp int64) { gameHub.OnChatFromBridge(nick, text, timestamp) },
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
	gameHub.SetTiming(cfg.RoundSeconds, cfg.SelectSeconds, cfg.ResultSeconds, cfg.TotalRounds, cfg.MaxPlayers)
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
		log.Printf("你画我猜插件 Demo 启动于 %s", cfg.Addr)
		log.Printf("  房间号   room-id          = %d", cfg.RoomID)
		log.Printf("  接入方   integration-id   = %s", cfg.IntegrationID)
		log.Printf("  聊天室   chatroom         = %s", cfg.ChatroomBase)
		log.Printf("  回合     绘画 %d 秒 / 选题 %d 秒 / 结算 %d 秒 · 单局 %d 回合（0=每人一次）· 最多 %d 人",
			cfg.RoundSeconds, cfg.SelectSeconds, cfg.ResultSeconds, cfg.TotalRounds, cfg.MaxPlayers)
	} else {
		log.Printf("你画我猜插件 Demo 启动于 %s（页面模式，等待配置房间参数后再启用联机）", cfg.Addr)
	}
	startLogTrim(logFilePath, logMaxKB)

	if err := http.ListenAndServe(cfg.Addr, mux); err != nil {
		log.Fatal(err)
	}
}

// libraryCount 词库词条总数（供启动日志）
func libraryCount() int {
	return len(library.flat)
}
