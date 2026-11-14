// 前端游戏状态。所有金额单位：分。
export class GameState {
  constructor() {
    this.connected = false
    this.connecting = false
    this.nick = ''
    this.avatar = ''
    this.frame = ''
    this.account = null
    this.round = { phase: 'idle', betCents: 0, poolCents: 0, revealAt: 0, result: '', resultAt: 0 }
    this.window = { phase: 'open', closeAt: 0, reopenAt: 0 }
    this.serverTime = 0
    this.clockOffset = 0
    this.leaderboard = null
    this.lastStateAt = 0
  }

  applyState(msg) {
    if (msg.account) this.account = msg.account
    if (msg.round) this.round = msg.round
    if (msg.window) this.window = msg.window
    if (msg.serverTime) {
      this.serverTime = msg.serverTime
      this.clockOffset = msg.serverTime - Date.now()
      this.lastStateAt = Date.now()
    }
    if (this.account && this.account.nick) this.nick = this.account.nick
  }

  applyWindow(msg) {
    if (!msg) return
    this.window = {
      phase: msg.phase || 'open',
      closeAt: msg.closeAt || 0,
      reopenAt: msg.reopenAt || 0
    }
    if (msg.serverTime) {
      this.serverTime = msg.serverTime
      this.clockOffset = msg.serverTime - Date.now()
    }
  }

  serverNow() {
    return Date.now() + this.clockOffset
  }

  get phase() { return this.round?.phase || 'idle' }
  get poolCents() { return this.round?.poolCents || 0 }
  get betCents() { return this.round?.betCents || 0 }
  get walletCents() { return this.account?.walletCents || 0 }
  get debtCents() { return this.account?.debtCents || 0 }
  get netWorthCents() { return this.account?.netWorthCents ?? (this.walletCents + this.poolCents - this.debtCents) }
  get totalAssetsCents() { return this.account?.totalAssetsCents ?? (this.walletCents + this.poolCents) }
  get bankrupt() { return !!this.account?.bankrupt }
  get freePlaysLeft() { return this.account?.freePlaysLeft ?? 0 }
  get freePlaysMax() { return this.account?.freePlaysMax ?? 10 }
  get dailyInterestCents() { return this.account?.dailyInterestCents || 0 }
  get maxLoanCents() { return this.account?.maxLoanCents || 0 }
  get minBetCents() { return this.account?.minBetCents || 100 }
  get nextFeeCents() { return this.account?.nextFeeCents || 0 }
  get giftPendingCents() { return this.account?.giftPendingCents || 0 }
  get isClosed() { return this.window?.phase === 'closed' }
  get isClosing() { return this.window?.phase === 'closing' }
  get canBet() {
    return this.connected && !this.bankrupt && !this.isClosed && this.phase === 'idle'
  }
  get canReveal() {
    return this.connected && !this.bankrupt && !this.isClosed && this.phase === 'bet' && this.poolCents > 0
  }
  get canCashout() {
    return this.connected && !this.bankrupt && this.phase === 'bet' && this.poolCents > 0
  }
}
