package main

import (
	"crypto/rand"
	"encoding/json"
	"fmt"
	"log"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/BlackCarDriver/nichijouSDK"
	"github.com/gorilla/websocket"
)

// client 一条游戏 WebSocket 连接。
type client struct {
	conn   *websocket.Conn
	nick   string
	send   chan []byte
	remote string

	roleID int
	avatar string
	frame  string

	done      chan struct{}
	closeOnce sync.Once
}

func (c *client) close() {
	c.closeOnce.Do(func() {
		close(c.done)
		_ = c.conn.Close()
	})
}

func (c *client) closeWithCode(code int, reason string) {
	c.closeOnce.Do(func() {
		_ = c.conn.WriteControl(
			websocket.CloseMessage,
			websocket.FormatCloseMessage(code, reason),
			time.Now().Add(time.Second),
		)
		close(c.done)
		_ = c.conn.Close()
	})
}

func (c *client) sendJSON(v interface{}) bool {
	data, err := json.Marshal(v)
	if err != nil {
		return false
	}
	select {
	case <-c.done:
		return false
	default:
	}
	select {
	case c.send <- data:
		return true
	case <-c.done:
		return false
	default:
		return false
	}
}

func (c *client) writePump() {
	for {
		select {
		case <-c.done:
			return
		case data := <-c.send:
			_ = c.conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
			if err := c.conn.WriteMessage(websocket.TextMessage, data); err != nil {
				c.close()
				return
			}
		}
	}
}

// hub 连接、窗口、游戏操作和桥接状态。
type hub struct {
	cfg    *Config
	store  *Store
	bridge *nichijouSDK.Bridge
	loc    *time.Location

	mu          sync.Mutex
	clients     map[string]*client
	graceTimers map[string]*time.Timer
	window      WindowView
	settledDate string
	roomClosed  bool
}

func newHub(cfg *Config, store *Store, loc *time.Location) *hub {
	return &hub{
		cfg:         cfg,
		store:       store,
		loc:         loc,
		clients:     map[string]*client{},
		graceTimers: map[string]*time.Timer{},
		window:      WindowView{Phase: windowOpen},
	}
}

func (h *hub) setBridge(b *nichijouSDK.Bridge) {
	h.mu.Lock()
	h.bridge = b
	h.mu.Unlock()
}

func (h *hub) isRoomClosed() bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.roomClosed
}

func (h *hub) now() time.Time {
	return time.Now().In(h.loc)
}

// ============================ 定时任务 ============================

func (h *hub) Run(stop <-chan struct{}) {
	ticker := time.NewTicker(200 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-stop:
			return
		case <-ticker.C:
			now := h.now()
			h.updateWindow(now)
			h.resolveReveals(now)
			h.maybeSettle(now)
		}
	}
}

func (h *hub) windowPhaseAt(now time.Time) string {
	hm := now.Hour()*60 + now.Minute()
	switch {
	case hm == 23*60+59 || hm == 0:
		return windowClosed
	case hm >= 23*60+50:
		return windowClosing
	default:
		return windowOpen
	}
}

func (h *hub) windowTimes(now time.Time, phase string) (closeAt, reopenAt time.Time) {
	day := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, h.loc)
	if phase == windowClosed && now.Hour() == 0 {
		day = day.AddDate(0, 0, -1)
	}
	closeAt = day.Add(23*time.Hour + 59*time.Minute)
	reopenAt = closeAt.Add(2 * time.Minute)
	return
}

func (h *hub) updateWindow(now time.Time) {
	phase := h.windowPhaseAt(now)
	closeAt, reopenAt := h.windowTimes(now, phase)

	h.mu.Lock()
	old := h.window.Phase
	h.window = WindowView{
		Phase:    phase,
		CloseAt:  closeAt.UnixMilli(),
		ReopenAt: reopenAt.UnixMilli(),
	}
	h.mu.Unlock()

	if old == phase {
		return
	}
	switch phase {
	case windowClosing:
		log.Printf("[window] 23:50 关门提醒")
		h.broadcastWindow()
	case windowClosed:
		log.Printf("[window] 23:59 强制结束并关闭连接")
		h.forceCashoutAll("daily_close")
		h.broadcastWindow()
		h.closeAllClients(4000, "settlement")
	case windowOpen:
		log.Printf("[window] 00:01 重新开放")
		h.broadcastWindow()
	}
}

func (h *hub) maybeSettle(now time.Time) {
	// 23:59:59 结算当天
	if now.Hour() == 23 && now.Minute() == 59 && now.Second() >= 59 {
		date := localDay(now)
		h.settleOnce(date, now)
		return
	}
	// 服务器在 00:00~00:01 启动时，补做前一天结算（同一天重复调用会按 LastSettleDate 跳过）
	if now.Hour() == 0 && now.Minute() < 1 {
		date := localDay(now.Add(-time.Minute))
		h.settleOnce(date, now)
	}
}

func (h *hub) settleOnce(date string, now time.Time) {
	h.mu.Lock()
	if h.settledDate == date {
		h.mu.Unlock()
		return
	}
	h.settledDate = date
	h.mu.Unlock()

	wasBankrupt := make(map[string]bool)
	changed, err := h.store.UpdateAll(func(acc *Account) bool {
		wasBankrupt[acc.Nick] = acc.Bankrupt
		return settleAccount(acc, date, h.cfg, now.UnixMilli())
	})
	if err != nil {
		log.Printf("[settle] 写盘失败: %v", err)
	}
	log.Printf("[settle] %s 结算完成，变更 %d 个账号", date, len(changed))
	for _, acc := range changed {
		h.pushAccount(acc)
		if acc.Bankrupt {
			h.sendEvent(acc.Nick, h.event("bankrupt", map[string]interface{}{
				"debtCents":   acc.DebtCents,
				"walletCents": acc.WalletCents,
			}))
			if !wasBankrupt[acc.Nick] {
				h.bridgeEvent("💀 " + acc.Nick + " 破产了")
			}
		}
	}
}

func (h *hub) resolveReveals(now time.Time) {
	nowMs := now.UnixMilli()
	nicks := h.store.AccountNicks()
	for _, nick := range nicks {
		var resolved *Account
		var result string
		updated, err := h.store.Update(nick, func(acc *Account) error {
			if acc.Round.Phase != phaseRevealing || nowMs < acc.Round.RevealAt {
				return errNoChange
			}
			resolveReveal(acc, nowMs)
			result = acc.Round.Result
			return nil
		})
		if err != nil {
			log.Printf("[reveal] %s 结算失败: %v", nick, err)
			continue
		}
		if result == "" {
			continue
		}
		resolved = updated
		h.pushAccount(resolved)
		h.sendEvent(nick, h.event("reveal_result", map[string]interface{}{
			"result":      result,
			"poolCents":   resolved.Round.PoolCents,
			"walletCents": resolved.WalletCents,
		}))
		h.ReportBridge()
	}
}

// ============================ 窗口与广播 ============================

func (h *hub) windowView() WindowView {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.window
}

func (h *hub) windowMessage() map[string]interface{} {
	w := h.windowView()
	return map[string]interface{}{
		"type":       "window",
		"phase":      w.Phase,
		"closeAt":    w.CloseAt,
		"reopenAt":   w.ReopenAt,
		"serverTime": h.now().UnixMilli(),
	}
}

func (h *hub) broadcastWindow() {
	h.broadcastAll(h.windowMessage())
}

func (h *hub) broadcastAll(msg interface{}) {
	h.mu.Lock()
	list := make([]*client, 0, len(h.clients))
	for _, c := range h.clients {
		list = append(list, c)
	}
	h.mu.Unlock()
	for _, c := range list {
		c.sendJSON(msg)
	}
}

func (h *hub) sendTo(nick string, msg interface{}) {
	h.mu.Lock()
	c := h.clients[nick]
	h.mu.Unlock()
	if c != nil {
		c.sendJSON(msg)
	}
}

func (h *hub) sendEvent(nick string, msg map[string]interface{}) {
	h.sendTo(nick, msg)
}

func (h *hub) event(action string, fields map[string]interface{}) map[string]interface{} {
	out := map[string]interface{}{
		"type":       "event",
		"action":     action,
		"serverTime": h.now().UnixMilli(),
	}
	for k, v := range fields {
		out[k] = v
	}
	return out
}

func (h *hub) stateMessage(acc *Account) map[string]interface{} {
	now := h.now()
	return map[string]interface{}{
		"type":       "state",
		"account":    accountView(acc, h.cfg, localDay(now)),
		"round":      roundViewOf(acc),
		"window":     h.windowView(),
		"serverTime": now.UnixMilli(),
	}
}

func (h *hub) pushAccount(acc *Account) {
	h.sendTo(acc.Nick, h.stateMessage(acc))
}

func (h *hub) sendError(nick, requestID, message string) {
	h.sendTo(nick, map[string]interface{}{
		"type":      "error",
		"code":      "action_failed",
		"message":   message,
		"requestId": requestID,
	})
}

// ============================ 连接生命周期 ============================

func (h *hub) activateClient(c *client, claims *nichijouSDK.Claims) {
	now := h.now()

	h.mu.Lock()
	if t, ok := h.graceTimers[c.nick]; ok {
		t.Stop()
		delete(h.graceTimers, c.nick)
	}
	old := h.clients[c.nick]
	h.clients[c.nick] = c
	h.mu.Unlock()
	if old != nil {
		old.close()
	}

	acc, created, err := h.store.Ensure(c.nick, claims.Avatar, claims.Frame, claims.RoleID, now)
	if err != nil {
		h.sendError(c.nick, "", "读取账号失败："+err.Error())
		c.close()
		return
	}
	if created {
		log.Printf("[account] 新账号 %s", c.nick)
	}

	giftMode := ""
	acc, err = h.store.Update(c.nick, func(a *Account) error {
		mode := grantDailyGift(a, h.cfg, now)
		if mode == "" {
			return errNoChange
		}
		giftMode = mode
		return nil
	})
	if err != nil {
		log.Printf("[gift] %s 发放失败: %v", c.nick, err)
	}

	welcome := map[string]interface{}{
		"type":       "welcome",
		"nick":       c.nick,
		"roleId":     claims.RoleID,
		"avatar":     claims.Avatar,
		"frameId":    claims.FrameID,
		"frame":      claims.Frame,
		"serverTime": now.UnixMilli(),
		"window":     h.windowView(),
	}
	c.sendJSON(welcome)
	c.sendJSON(h.stateMessage(acc))
	if giftMode != "" {
		c.sendJSON(h.event("gift", map[string]interface{}{
			"amountCents": h.cfg.GiftCents,
			"mode":        giftMode,
			"walletCents": acc.WalletCents,
		}))
	}
	log.Printf("[ws] %s 进入游戏（余额 %.2f，欠款 %.2f，免费 %d/%d）",
		c.nick, float64(acc.WalletCents)/100, float64(acc.DebtCents)/100,
		freePlaysLeft(acc, h.cfg.FreePlays), h.cfg.FreePlays)
	h.ReportBridge()
	h.SyncRoomLP()
}

func (h *hub) handleDisconnect(c *client) {
	h.mu.Lock()
	current := h.clients[c.nick]
	if current != c {
		h.mu.Unlock()
		log.Printf("[ws] %s 旧连接退出", c.nick)
		return
	}
	delete(h.clients, c.nick)
	if t, ok := h.graceTimers[c.nick]; ok {
		t.Stop()
	}
	grace := time.Duration(h.cfg.GraceSeconds) * time.Second
	if grace < 0 {
		grace = 0
	}
	h.graceTimers[c.nick] = time.AfterFunc(grace, func() {
		h.autoCashout(c.nick, "disconnect")
	})
	h.mu.Unlock()

	c.close()
	log.Printf("[ws] %s 断开，%s 后自动提款", c.nick, grace)
	h.ReportBridge()
	h.SyncRoomLP()
}

func (h *hub) autoCashout(nick, reason string) {
	h.mu.Lock()
	if h.clients[nick] != nil {
		h.mu.Unlock()
		return
	}
	delete(h.graceTimers, nick)
	h.mu.Unlock()

	changed := false
	acc, err := h.store.Update(nick, func(a *Account) error {
		changed = cashoutRound(a, h.now().UnixMilli())
		if !changed {
			return errNoChange
		}
		return nil
	})
	if err != nil {
		log.Printf("[cashout] %s 自动提款失败: %v", nick, err)
		return
	}
	if changed {
		log.Printf("[cashout] %s %s 自动提款", nick, reason)
		h.pushAccount(acc)
	}
	h.ReportBridge()
	h.SyncRoomLP()
}

// OnUserLeave 聊天室离房：立即关闭连接并提款，不等待宽限期。
func (h *hub) OnUserLeave(nick string) {
	h.mu.Lock()
	c := h.clients[nick]
	delete(h.clients, nick)
	if t, ok := h.graceTimers[nick]; ok {
		t.Stop()
		delete(h.graceTimers, nick)
	}
	h.mu.Unlock()
	if c != nil {
		c.close()
	}

	if _, ok := h.store.Get(nick); !ok {
		h.ReportBridge()
		h.SyncRoomLP()
		return
	}

	_, err := h.store.Update(nick, func(acc *Account) error {
		if !cashoutRound(acc, h.now().UnixMilli()) {
			return errNoChange
		}
		return nil
	})
	if err != nil {
		log.Printf("[leave] %s 结算失败: %v", nick, err)
	}
	log.Printf("[leave] %s 离开房间", nick)
	h.ReportBridge()
	h.SyncRoomLP()
}

func (h *hub) OnRoomClosed() {
	h.mu.Lock()
	h.roomClosed = true
	h.mu.Unlock()
	log.Printf("[room] 房间已关闭，停止接纳")
	h.forceCashoutAll("room_closed")
	h.broadcastWindow()
	h.closeAllClients(4001, "room closed")
}

func (h *hub) closeAllClients(code int, reason string) {
	h.mu.Lock()
	list := make([]*client, 0, len(h.clients))
	for nick, c := range h.clients {
		list = append(list, c)
		delete(h.clients, nick)
	}
	for nick, t := range h.graceTimers {
		t.Stop()
		delete(h.graceTimers, nick)
	}
	h.mu.Unlock()
	for _, c := range list {
		c.closeWithCode(code, reason)
	}
}

// forceCashoutAll 强制结束所有奖池并提款。启动、关机、23:59 关门时使用。
func (h *hub) forceCashoutAll(reason string) {
	nowMs := h.now().UnixMilli()
	changed, err := h.store.UpdateAll(func(acc *Account) bool {
		return cashoutRound(acc, nowMs)
	})
	if err != nil {
		log.Printf("[cashout] %s 强制提款写盘失败: %v", reason, err)
	}
	log.Printf("[cashout] %s 强制结束 %d 个账号", reason, len(changed))
	for _, acc := range changed {
		h.pushAccount(acc)
	}
}

// ForceSettleAllOnStartup 服务器重启：全部强制结束并自动提款。
func (h *hub) ForceSettleAllOnStartup() {
	h.forceCashoutAll("server_start")
}

// Shutdown 优雅退出：强制结算、写盘、备份。
func (h *hub) Shutdown() {
	log.Printf("[shutdown] 开始优雅退出")
	h.forceCashoutAll("shutdown")
	h.closeAllClients(1001, "server shutdown")
	if err := h.store.Save(); err != nil {
		log.Printf("[shutdown] 写盘失败: %v", err)
	}
	h.store.Backup()
	log.Printf("[shutdown] 完成")
}

// cashoutRound 把奖池提现到钱包；如果正在开奖，先应用已定结果。
func cashoutRound(acc *Account, nowMs int64) bool {
	changed := false
	if acc.Round.Phase == phaseRevealing {
		resolveReveal(acc, nowMs)
		changed = true
	}
	if acc.Round.PoolCents > 0 {
		acc.WalletCents += acc.Round.PoolCents
		acc.Stats.Cashouts++
		acc.Round.PoolCents = 0
		changed = true
	}
	if acc.Round.Phase != phaseIdle {
		acc.Round.Phase = phaseIdle
		changed = true
	}
	if acc.Round.BetCents != 0 {
		acc.Round.BetCents = 0
		changed = true
	}
	if acc.Round.PendingResult != "" {
		acc.Round.PendingResult = ""
		changed = true
	}
	if acc.Round.RevealAt != 0 {
		acc.Round.RevealAt = 0
		changed = true
	}
	if acc.Round.Result != "" {
		acc.Round.Result = ""
		acc.Round.ResultAt = 0
		changed = true
	}
	return changed
}

// grantDailyGift 每天首次进入发 10 金币。无负债直接进钱包；有负债先挂起抵扣利息。
func grantDailyGift(acc *Account, cfg *Config, now time.Time) string {
	today := localDay(now)
	if acc.LastGiftDate == today {
		return ""
	}
	acc.LastGiftDate = today
	acc.Stats.TotalGiftCents += cfg.GiftCents
	if acc.DebtCents <= 0 {
		acc.WalletCents += cfg.GiftCents
		acc.GiftPendingCents = 0
		return "wallet"
	}
	acc.GiftPendingCents += cfg.GiftCents
	return "interest"
}

// ============================ 游戏操作 ============================

func (h *hub) Bet(nick, requestID string, amountCents int64) {
	if h.windowView().Phase == windowClosed {
		h.sendError(nick, requestID, "正在结算，暂停参与")
		return
	}
	now := h.now()
	duplicate := false
	feeCents := int64(0)
	acc, err := h.store.Update(nick, func(a *Account) error {
		if requestID != "" && a.LastRequestID == requestID {
			duplicate = true
			return errNoChange
		}
		if a.Bankrupt {
			return fmt.Errorf("已破产，无法继续押注")
		}
		if a.Round.Phase == phaseRevealing {
			return fmt.Errorf("开奖中，请稍候")
		}
		if a.Round.Phase == phaseBet {
			return fmt.Errorf("请先取出或开奖")
		}
		if amountCents <= 0 {
			return fmt.Errorf("押注金额必须大于 0")
		}
		if amountCents%100 != 0 {
			return fmt.Errorf("押注必须是整数金币")
		}
		minBet := minBetCents(a, h.cfg)
		if amountCents < minBet {
			return fmt.Errorf("本次最少押注 %.2f 金币", float64(minBet)/100)
		}
		if amountCents > a.WalletCents {
			return fmt.Errorf("金币不足")
		}
		feeCents = nextFeeCents(a, h.cfg)
		poolCents := amountCents - feeCents
		if poolCents <= 0 {
			return fmt.Errorf("押注金额不足")
		}
		a.WalletCents -= amountCents
		a.FreePlaysUsed++
		a.Round = Round{Phase: phaseBet, BetCents: amountCents, PoolCents: poolCents}
		a.Stats.Bets++
		if poolCents > a.Stats.MaxPoolCents {
			a.Stats.MaxPoolCents = poolCents
		}
		a.LastRequestID = requestID
		a.LastRequestAt = now.UnixMilli()
		return nil
	})
	if err != nil {
		h.sendError(nick, requestID, err.Error())
		return
	}
	if duplicate {
		h.pushAccount(acc)
		return
	}
	h.pushAccount(acc)
	h.sendEvent(nick, h.event("bet", map[string]interface{}{
		"amountCents":   amountCents,
		"feeCents":      feeCents,
		"poolCents":     acc.Round.PoolCents,
		"walletCents":   acc.WalletCents,
		"freePlaysLeft": freePlaysLeft(acc, h.cfg.FreePlays),
	}))
	h.ReportBridge()
	h.SyncRoomLP()
}

func (h *hub) Cashout(nick, requestID string) {
	now := h.now()
	duplicate := false
	var amountCents, betCents int64
	acc, err := h.store.Update(nick, func(a *Account) error {
		if requestID != "" && a.LastRequestID == requestID {
			duplicate = true
			return errNoChange
		}
		if a.Round.Phase != phaseBet || a.Round.PoolCents <= 0 {
			return fmt.Errorf("当前没有可取出的金币")
		}
		amountCents = a.Round.PoolCents
		betCents = a.Round.BetCents
		a.LastRequestID = requestID
		a.LastRequestAt = now.UnixMilli()
		if !cashoutRound(a, now.UnixMilli()) {
			return fmt.Errorf("当前没有可取出的金币")
		}
		return nil
	})
	if err != nil {
		h.sendError(nick, requestID, err.Error())
		return
	}
	if duplicate {
		h.pushAccount(acc)
		return
	}
	h.pushAccount(acc)
	h.sendEvent(nick, h.event("cashout", map[string]interface{}{
		"amountCents": amountCents,
		"walletCents": acc.WalletCents,
	}))
	log.Printf("[cashout] %s pool=%.2f bet=%.2f profit=%.2f",
		nick, float64(amountCents)/100, float64(betCents)/100, float64(amountCents-betCents)/100)
	if amountCents > betCents {
		profitCoins := (amountCents - betCents) / 100
		h.bridgeEvent(fmt.Sprintf("%s成功带走了%d金币", nick, profitCoins))
	}
	h.ReportBridge()
	h.SyncRoomLP()
}

func (h *hub) Reveal(nick, requestID string) {
	if h.windowView().Phase == windowClosed {
		h.sendError(nick, requestID, "正在结算，暂停参与")
		return
	}
	now := h.now()
	duplicate := false
	var revealAt int64
	var poolCents int64
	acc, err := h.store.Update(nick, func(a *Account) error {
		if requestID != "" && a.LastRequestID == requestID {
			duplicate = true
			return errNoChange
		}
		if a.Bankrupt {
			return fmt.Errorf("已破产，无法继续开奖")
		}
		if a.Round.Phase != phaseBet || a.Round.PoolCents <= 0 {
			return fmt.Errorf("请先押注")
		}
		result := resultLose
		if randomBool() {
			result = resultWin
		}
		a.Round.Phase = phaseRevealing
		a.Round.PendingResult = result
		a.Round.RevealAt = now.UnixMilli() + h.cfg.RevealMs
		a.Round.Result = ""
		a.Round.ResultAt = 0
		a.LastRequestID = requestID
		a.LastRequestAt = now.UnixMilli()
		revealAt = a.Round.RevealAt
		poolCents = a.Round.PoolCents
		return nil
	})
	if err != nil {
		h.sendError(nick, requestID, err.Error())
		return
	}
	if duplicate {
		h.pushAccount(acc)
		return
	}
	h.pushAccount(acc)
	h.sendEvent(nick, h.event("reveal_start", map[string]interface{}{
		"revealAt":  revealAt,
		"poolCents": poolCents,
	}))
	h.ReportBridge()
}

func (h *hub) Borrow(nick, requestID string, amountCents int64) {
	now := h.now()
	duplicate := false
	acc, err := h.store.Update(nick, func(a *Account) error {
		if requestID != "" && a.LastRequestID == requestID {
			duplicate = true
			return errNoChange
		}
		if a.Bankrupt {
			return fmt.Errorf("已破产，无法贷款")
		}
		if amountCents <= 0 {
			return fmt.Errorf("贷款金额必须大于 0")
		}
		if amountCents%100 != 0 {
			return fmt.Errorf("贷款必须是整数金币")
		}
		maxLoan := maxLoanCents(a.WalletCents, a.DebtCents, h.cfg.LoanMultiplier)
		if amountCents > maxLoan {
			return fmt.Errorf("超过可贷额度 %.2f 金币", float64(maxLoan)/100)
		}
		a.WalletCents += amountCents
		a.DebtCents += amountCents
		a.Stats.TotalBorrowedCents += amountCents
		a.LastRequestID = requestID
		a.LastRequestAt = now.UnixMilli()
		return nil
	})
	if err != nil {
		h.sendError(nick, requestID, err.Error())
		return
	}
	if duplicate {
		h.pushAccount(acc)
		return
	}
	h.pushAccount(acc)
	h.sendEvent(nick, h.event("loan", map[string]interface{}{
		"amountCents": amountCents,
		"walletCents": acc.WalletCents,
		"debtCents":   acc.DebtCents,
	}))
	h.ReportBridge()
	h.SyncRoomLP()
}

func (h *hub) Repay(nick, requestID string, amountCents int64) {
	now := h.now()
	duplicate := false
	acc, err := h.store.Update(nick, func(a *Account) error {
		if requestID != "" && a.LastRequestID == requestID {
			duplicate = true
			return errNoChange
		}
		if a.Bankrupt {
			return fmt.Errorf("已破产，无法还款")
		}
		if amountCents <= 0 {
			return fmt.Errorf("还款金额必须大于 0")
		}
		if amountCents > a.WalletCents {
			return fmt.Errorf("钱包余额不足")
		}
		if amountCents > a.DebtCents {
			return fmt.Errorf("还款金额超过欠款")
		}
		a.WalletCents -= amountCents
		a.DebtCents -= amountCents
		a.Stats.TotalRepaidCents += amountCents
		a.LastRequestID = requestID
		a.LastRequestAt = now.UnixMilli()
		return nil
	})
	if err != nil {
		h.sendError(nick, requestID, err.Error())
		return
	}
	if duplicate {
		h.pushAccount(acc)
		return
	}
	h.pushAccount(acc)
	h.sendEvent(nick, h.event("repay", map[string]interface{}{
		"amountCents": amountCents,
		"walletCents": acc.WalletCents,
		"debtCents":   acc.DebtCents,
	}))
	h.ReportBridge()
	h.SyncRoomLP()
}

func (h *hub) SendState(nick string) {
	acc, ok := h.store.Get(nick)
	if !ok {
		return
	}
	h.pushAccount(acc)
}

func (h *hub) SendLeaderboard(nick string) {
	top, self, total := h.Leaderboard(nick)
	h.sendTo(nick, map[string]interface{}{
		"type":       "leaderboard",
		"entries":    top,
		"self":       self,
		"total":      total,
		"serverTime": h.now().UnixMilli(),
	})
}

// ============================ 桥接 ============================

func (h *hub) BridgeStates() map[string]nichijouSDK.State {
	h.mu.Lock()
	nicks := make([]string, 0, len(h.clients))
	for nick := range h.clients {
		nicks = append(nicks, nick)
	}
	h.mu.Unlock()

	out := make(map[string]nichijouSDK.State, len(nicks))
	for _, nick := range nicks {
		acc, ok := h.store.Get(nick)
		if !ok {
			continue
		}
		if acc.Bankrupt {
			out[nick] = nichijouSDK.State{State: "custom", Text: "已破产"}
		} else {
			out[nick] = nichijouSDK.State{State: "playing"}
		}
	}
	return out
}

func (h *hub) RoomLP() string {
	accounts := h.store.All()
	if len(accounts) == 0 {
		return "梭哈模拟器"
	}
	var best *Account
	for _, acc := range accounts {
		if best == nil || netWorthCents(acc) > netWorthCents(best) {
			best = acc
		}
	}
	if best == nil {
		return "梭哈模拟器"
	}
	label := "梭哈模拟器 · 首富 " + best.Nick
	runes := make([]rune, 0, 24)
	bytes := 0
	for _, r := range label {
		n := utf8.RuneLen(r)
		if len(runes) >= 24 || bytes+n > 64 {
			break
		}
		runes = append(runes, r)
		bytes += n
	}
	return string(runes)
}

func (h *hub) bridgeEvent(text string) {
	h.mu.Lock()
	b := h.bridge
	h.mu.Unlock()
	if b != nil {
		log.Printf("[event] %s", text)
		b.EventLog(text)
	}
}

func (h *hub) ReportBridge() {
	h.mu.Lock()
	b := h.bridge
	h.mu.Unlock()
	if b != nil {
		b.ScheduleFullReport()
	}
}

func (h *hub) SyncRoomLP() {
	h.mu.Lock()
	b := h.bridge
	h.mu.Unlock()
	if b != nil {
		b.SetRoomLP(h.RoomLP())
	}
}

// ============================ 工具 ============================

func randomBool() bool {
	var b [1]byte
	if _, err := rand.Read(b[:]); err == nil {
		return b[0]&1 == 0
	}
	return time.Now().UnixNano()&1 == 0
}
