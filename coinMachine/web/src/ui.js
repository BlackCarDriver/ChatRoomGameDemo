import { fmtCents, fmtCentsSigned } from './format.js'

const $ = id => document.getElementById(id)

function interestCents(debtCents) {
  return Math.ceil(Math.max(0, debtCents) / 100)
}

function pad2(n) { return String(n).padStart(2, '0') }

function mmss(ms) {
  const total = Math.max(0, Math.floor(ms / 1000))
  return `${pad2(Math.floor(total / 60))}:${pad2(total % 60)}`
}

export class UI {
  constructor() {
    this.state = null
    this.callbacks = {}
    this.modalOpen = false
    this.lastWallet = null
    this.lastNet = null
    this.avatarSrc = ''
    this.toastTimer = null
    this.disconnectTimer = null
    this.leaderboardTimer = null
    this.settleLocalAt = 0
    this.settleVisible = false

    this.el = {
      avatar: $('avatar'),
      avatarWrap: document.querySelector('.avatar-wrap'),
      avatarFallback: $('avatarFallback'),
      nick: $('nick'),
      connDot: $('connDot'),
      wallet: $('wallet'),
      netWorth: $('netWorth'),
      debt: $('debt'),
      freePlays: $('freePlays'),
      giftStatus: $('giftStatus'),
      interestWarn: $('interestWarn'),
      statusText: $('statusText'),
      clock: $('clock'),
      windowWarn: $('windowWarn'),
      bankruptBanner: $('bankruptBanner'),
      betModal: $('betModal'),
      bankModal: $('bankModal'),
      leaderboardModal: $('leaderboardModal'),
      rulesModal: $('rulesModal'),
      settleOverlay: $('settleOverlay'),
      settleCountdown: $('settleCountdown'),
      disconnectOverlay: $('disconnectOverlay'),
      disconnectText: $('disconnectText'),
      toast: $('toast'),
      btnSound: $('btnSound')
    }
    this.bind()
    this.timer = setInterval(() => this.tick(), 500)
  }

  on(callbacks) {
    this.callbacks = callbacks || {}
  }

  bind() {
    for (const btn of document.querySelectorAll('[data-close]')) {
      btn.addEventListener('click', () => this.closeModal(btn.dataset.close))
    }
    for (const backdrop of document.querySelectorAll('.modal-backdrop')) {
      backdrop.addEventListener('pointerdown', e => {
        if (e.target === backdrop) this.closeModal(backdrop.id)
      })
    }
    document.addEventListener('keydown', e => {
      if (e.key === 'Escape' && this.modalOpen) {
        const open = document.querySelector('.modal-backdrop.open')
        if (open) this.closeModal(open.id)
      }
    })

    $('btnRules').addEventListener('click', () => this.openModal('rulesModal'))
    $('btnLeaderboard').addEventListener('click', () => this.openLeaderboard())
    $('btnBank').addEventListener('click', () => this.openBank())
    $('btnSound').addEventListener('click', () => {
      const muted = this.callbacks.onToggleSound?.()
      if (typeof muted === 'boolean') this.setSoundMuted(muted)
    })

    $('betMinus').addEventListener('click', () => this.stepBet(-1))
    $('betPlus').addEventListener('click', () => this.stepBet(1))
    $('betAmount').addEventListener('input', () => this.updateBetBreakdown())
    for (const btn of document.querySelectorAll('[data-bet-quick]')) {
      btn.addEventListener('click', () => {
        const v = btn.dataset.betQuick
        const max = Math.floor((this.state?.walletCents || 0) / 100)
        const cur = Math.max(0, Math.floor(Number($('betAmount').value) || 0))
        // 数字按钮是累加：点两次 10 = 20；梭哈是直接拉满
        let n = v === 'all' ? max : cur + Number(v)
        n = Math.max(0, Math.min(max, n))
        $('betAmount').value = String(n)
        this.updateBetBreakdown()
      })
    }
    $('betConfirm').addEventListener('click', () => this.confirmBet())
    $('betGoBank').addEventListener('click', () => {
      this.closeModal('betModal')
      this.openBank()
    })

    $('bankTabLoan').addEventListener('click', () => this.switchBankTab('loan'))
    $('bankTabRepay').addEventListener('click', () => this.switchBankTab('repay'))
    $('loanAmount').addEventListener('input', () => this.updateLoanProjection())
    $('repayAmount').addEventListener('input', () => this.updateRepayProjection())
    $('loanMinus').addEventListener('click', () => this.stepInput('loanAmount', -1))
    $('loanPlus').addEventListener('click', () => this.stepInput('loanAmount', 1))
    $('repayMinus').addEventListener('click', () => this.stepInput('repayAmount', -1, 0.01))
    $('repayPlus').addEventListener('click', () => this.stepInput('repayAmount', 1, 0.01))
    for (const btn of document.querySelectorAll('[data-loan-quick]')) {
      btn.addEventListener('click', () => {
        const v = btn.dataset.loanQuick
        const max = Math.floor((this.state?.maxLoanCents || 0) / 100)
        $('loanAmount').value = String(v === 'max' ? Math.max(1, max) : Number(v))
        this.updateLoanProjection()
      })
    }
    for (const btn of document.querySelectorAll('[data-repay-quick]')) {
      btn.addEventListener('click', () => {
        const v = btn.dataset.repayQuick
        const max = Math.min(this.state?.walletCents || 0, this.state?.debtCents || 0) / 100
        $('repayAmount').value = String(v === 'all' ? max.toFixed(2) : Number(v))
        this.updateRepayProjection()
      })
    }
    $('loanConfirm').addEventListener('click', () => this.confirmLoan())
    $('repayConfirm').addEventListener('click', () => this.confirmRepay())

    $('leaderboardRefresh').addEventListener('click', () => this.callbacks.onLeaderboard?.())
    $('rulesConfirm').addEventListener('click', () => this.closeModal('rulesModal'))
  }

  // ===================== 状态渲染 =====================

  render(state) {
    this.state = state
    if (!state) return
    this.renderPlayer(state)
    this.renderStats(state)
    this.renderStatus(state)
    if (!state.isClosed && this.settleVisible) this.hideSettlement()
    this.el.bankruptBanner.classList.toggle('hidden', !state.bankrupt)
    if (this.el.betModal.classList.contains('open')) this.updateBetBreakdown()
    if (this.el.bankModal.classList.contains('open')) this.updateBank()
    this.tick()
  }

  renderPlayer(state) {
    const acc = state.account || {}
    this.el.nick.textContent = acc.nick || '未连接'
    this.el.avatarFallback.textContent = (acc.nick || '梭').slice(0, 1)

    const avatar = acc.avatar || ''
    if (avatar) {
      if (this.avatarSrc !== avatar) {
        this.avatarSrc = avatar
        this.el.avatarWrap.classList.remove('no-img')
        this.el.avatar.onload = () => this.el.avatarWrap.classList.remove('no-img')
        this.el.avatar.onerror = () => this.el.avatarWrap.classList.add('no-img')
        this.el.avatar.src = avatar
      }
    } else {
      this.el.avatarWrap.classList.add('no-img')
    }

    this.setMoney(this.el.wallet, state.walletCents, this.lastWallet)
    this.lastWallet = state.walletCents
    this.setMoney(this.el.netWorth, state.netWorthCents, this.lastNet)
    this.lastNet = state.netWorthCents
    this.el.netWorth.classList.toggle('red', state.netWorthCents < 0)
    this.el.netWorth.classList.toggle('green', state.netWorthCents > 0)

    this.setMoney(this.el.debt, state.debtCents, null)
    this.el.debt.classList.toggle('red', state.debtCents > 0)
  }

  setMoney(el, cents, fromCents) {
    if (fromCents == null || fromCents === cents) {
      el.textContent = fmtCents(cents)
      return
    }
    const token = (el._moneyToken || 0) + 1
    el._moneyToken = token
    const start = performance.now()
    const dur = 420
    const step = t => {
      if (el._moneyToken !== token) return
      const p = Math.min(1, (t - start) / dur)
      const v = fromCents + (cents - fromCents) * (1 - Math.pow(1 - p, 3))
      el.textContent = fmtCents(v)
      if (p < 1) requestAnimationFrame(step)
    }
    requestAnimationFrame(step)
  }

  renderStats(state) {
    this.el.freePlays.textContent = `${state.freePlaysLeft} / ${state.freePlaysMax}`
    const acc = state.account || {}
    let gift = '未领取'
    if (state.isClosed) gift = '结算中'
    else if (acc.giftClaimedToday) gift = state.giftPendingCents > 0 ? '待抵利息' : '已领取'
    this.el.giftStatus.textContent = gift

    const interest = state.dailyInterestCents
    const dailyGift = acc.dailyGiftCents || 1000
    if (state.debtCents > 0) {
      this.el.interestWarn.classList.remove('hidden')
      if (interest > dailyGift) {
        const inc = interest - dailyGift
        this.el.interestWarn.textContent = `日息 ${fmtCents(interest)} · 债务每日 +${fmtCents(inc)}${state.bankrupt ? '（已破产）' : ''}`
      } else {
        this.el.interestWarn.textContent = `日息 ${fmtCents(interest)} · 每日赠送可抵扣`
      }
    } else {
      this.el.interestWarn.classList.add('hidden')
    }
  }

  renderStatus(state) {
    let text = ''
    if (!state.connected) {
      text = state.connecting ? '正在连接…' : '连接断开，正在重连…'
    } else if (state.isClosed) {
      text = '结算中，00:01 后重新开放'
    } else if (state.bankrupt) {
      text = '已破产，无法继续押注'
    } else if (state.phase === 'revealing') {
      text = '开奖中…'
    } else if (state.phase === 'bet') {
      text = `奖池 ${fmtCents(state.poolCents)} · 红色搏一把，绿色收手`
    } else if (state.walletCents < state.minBetCents) {
      text = '金币不足，等待每日赠送或去银行贷款'
    } else {
      text = '点击绿色按钮投币'
      if (state.nextFeeCents > 0) text += ` · 本次手续费 ${fmtCents(state.nextFeeCents)}`
    }
    this.el.statusText.textContent = text
  }

  tick() {
    const state = this.state
    if (!state) return
    const now = state.serverNow()
    const d = new Date(now)
    this.el.clock.textContent = `${pad2(d.getHours())}:${pad2(d.getMinutes())}:${pad2(d.getSeconds())}`

    if (state.window?.phase === 'closing') {
      this.el.windowWarn.classList.remove('hidden')
      this.el.windowWarn.textContent = `距离关门 ${mmss((state.window.closeAt || now) - now)}`
    } else if (state.window?.phase === 'closed') {
      this.el.windowWarn.classList.remove('hidden')
      this.el.windowWarn.textContent = '结算中'
    } else {
      this.el.windowWarn.classList.add('hidden')
    }

    if (this.el.settleOverlay.classList.contains('hidden')) {
      if (state.isClosed) this.showSettlementDelay(0)
    } else {
      const remain = this.settleLocalAt - Date.now()
      this.el.settleCountdown.textContent = remain > 0 ? mmss(remain) : '00:00'
    }
  }

  // ===================== 弹窗 =====================

  openModal(id) {
    for (const m of document.querySelectorAll('.modal-backdrop.open')) {
      if (m.id !== id) this.closeModal(m.id)
    }
    const el = $(id)
    if (!el) return
    el.classList.add('open')
    el.setAttribute('aria-hidden', 'false')
    this.modalOpen = true
    this.callbacks.onModalChange?.(true)
  }

  closeModal(id) {
    const el = $(id)
    if (!el) return
    el.classList.remove('open')
    el.setAttribute('aria-hidden', 'true')
    if (id === 'leaderboardModal') this.stopLeaderboardTimer()
    if (!document.querySelector('.modal-backdrop.open')) {
      this.modalOpen = false
      this.callbacks.onModalChange?.(false)
    }
  }

  // ===================== 投币 =====================

  openBet() {
    if (!this.state) return
    if (this.state.bankrupt) {
      this.showToast('已破产，无法继续押注', 'error')
      return
    }
    if (this.state.isClosed) {
      this.showToast('结算中，暂停参与', 'error')
      return
    }
    if (this.state.phase === 'revealing') {
      this.showToast('开奖中，请稍候', 'error')
      return
    }
    if (this.state.phase === 'bet') {
      this.callbacks.onCashout?.()
      return
    }
    this.openModal('betModal')
    this.resetBetModal()
  }

  resetBetModal() {
    const s = this.state
    if (!s) return
    $('betWallet').textContent = fmtCents(s.walletCents)
    $('betFree').textContent = `${s.freePlaysLeft} / ${s.freePlaysMax}`
    const min = Math.max(1, Math.ceil(s.minBetCents / 100))
    const max = Math.floor(s.walletCents / 100)
    const amount = $('betAmount')
    amount.min = String(min)
    amount.max = String(max)
    amount.value = '0'
    this.updateBetBreakdown()
  }

  stepBet(delta) {
    const el = $('betAmount')
    const s = this.state
    const min = Math.max(1, Math.ceil((s?.minBetCents || 100) / 100))
    const max = Math.floor((s?.walletCents || 0) / 100)
    let n = Math.floor(Number(el.value) || 0) + delta
    n = Math.max(0, Math.min(max, n))
    if (delta > 0 && n < min) n = min
    el.value = String(n)
    this.updateBetBreakdown()
  }

  updateBetBreakdown() {
    const s = this.state
    if (!s) return
    const amount = Math.max(0, Math.floor(Number($('betAmount').value) || 0))
    const stake = amount * 100
    const fee = s.nextFeeCents
    const pool = Math.max(0, stake - fee)
    $('betStake').textContent = fmtCents(stake)
    $('betFee').textContent = fmtCents(fee)
    $('betPool').textContent = fmtCents(pool)

    let err = ''
    const noMoney = s.walletCents < s.minBetCents
    if (noMoney) err = '金币不足，请先去银行贷款或等待每日赠送'
    else if (amount <= 0) err = '请输入押注数量'
    else if (stake < s.minBetCents) err = `本次最少押注 ${fmtCents(s.minBetCents)}`
    else if (stake > s.walletCents) err = '金币不足，请先去银行贷款'
    else if (pool <= 0) err = '押注金额不足'
    $('betError').textContent = err
    $('betGoBank').classList.toggle('hidden', !noMoney)
    $('betConfirm').disabled = !!err
  }

  confirmBet() {
    const s = this.state
    if (!s) return
    const amount = Math.max(0, Math.floor(Number($('betAmount').value) || 0))
    const stake = amount * 100
    if (stake < s.minBetCents || stake > s.walletCents) return
    this.callbacks.onBet?.(stake)
    this.closeModal('betModal')
  }

  // ===================== 银行 =====================

  openBank() {
    this.openModal('bankModal')
    this.switchBankTab('loan')
    this.updateBank()
  }

  switchBankTab(which) {
    $('bankTabLoan').classList.toggle('active', which === 'loan')
    $('bankTabRepay').classList.toggle('active', which === 'repay')
    $('bankLoanPanel').classList.toggle('hidden', which !== 'loan')
    $('bankRepayPanel').classList.toggle('hidden', which !== 'repay')
    if (which === 'loan') this.updateLoanProjection()
    else this.updateRepayProjection()
  }

  updateBank() {
    const s = this.state
    if (!s) return
    const bankrupt = s.bankrupt
    $('bankBankrupt').classList.toggle('hidden', !bankrupt)
    $('bankWallet').textContent = fmtCents(s.walletCents)
    $('bankDebt').textContent = fmtCents(s.debtCents)
    const available = s.walletCents - s.debtCents
    $('bankAvailable').textContent = fmtCentsSigned(available)
    $('bankAvailable').classList.toggle('red', available < 0)
    $('bankMaxLoan').textContent = fmtCents(s.maxLoanCents)
    $('bankInterest').textContent = fmtCents(s.dailyInterestCents)
    $('bankGiftNote').textContent = fmtCents(s.account?.dailyGiftCents || 1000) +
      (s.giftPendingCents > 0 ? '（已挂起抵息）' : '')

    for (const id of ['bankTabLoan', 'bankTabRepay', 'loanAmount', 'loanMinus', 'loanPlus', 'loanConfirm', 'repayAmount', 'repayMinus', 'repayPlus', 'repayConfirm']) {
      $(id).disabled = bankrupt
    }
    for (const btn of document.querySelectorAll('[data-loan-quick], [data-repay-quick]')) {
      btn.disabled = bankrupt
    }

    const maxLoanCoins = Math.floor(s.maxLoanCents / 100)
    const loanInput = $('loanAmount')
    loanInput.max = String(Math.max(1, maxLoanCoins))
    if (Number(loanInput.value) > maxLoanCoins) loanInput.value = String(Math.max(1, maxLoanCoins))
    this.updateLoanProjection()
    this.updateRepayProjection()
  }

  updateLoanProjection() {
    const s = this.state
    if (!s) return
    const amount = Math.max(0, Math.floor(Number($('loanAmount').value) || 0))
    const cents = amount * 100
    const projectedDebt = s.debtCents + cents
    const projectedInterest = interestCents(projectedDebt)
    $('loanProjectedDebt').textContent = fmtCents(projectedDebt)
    $('loanProjectedInterest').textContent = fmtCents(projectedInterest)

    const dailyGift = s.account?.dailyGiftCents || 1000
    const warn = $('loanWarning')
    if (s.maxLoanCents < 100) {
      warn.classList.remove('hidden')
      warn.classList.add('danger')
      warn.textContent = '当前没有可借额度；可等待每日赠送或还款后恢复额度'
    } else if (projectedInterest > dailyGift) {
      warn.classList.remove('hidden')
      warn.classList.add('danger')
      warn.textContent = `日息超过每日赠送，债务每天增加 ${fmtCents(projectedInterest - dailyGift)}`
    } else {
      warn.classList.add('hidden')
    }

    const invalid = s.bankrupt || amount <= 0 || cents > s.maxLoanCents
    $('loanConfirm').disabled = invalid
  }

  stepInput(id, delta, step = 1) {
    const el = $(id)
    let n = Number(el.value) || 0
    n = Math.max(0, n + delta * step)
    el.value = step < 1 ? n.toFixed(2) : String(Math.round(n))
    if (id === 'loanAmount') this.updateLoanProjection()
    else this.updateRepayProjection()
  }

  updateRepayProjection() {
    const s = this.state
    if (!s) return
    const amount = Math.max(0, Number($('repayAmount').value) || 0)
    const cents = Math.round(amount * 100)
    const projected = Math.max(0, s.debtCents - cents)
    $('repayProjected').textContent = fmtCents(projected)
    const invalid = s.bankrupt || cents <= 0 || cents > s.walletCents || cents > s.debtCents
    $('repayConfirm').disabled = invalid
  }

  confirmLoan() {
    const s = this.state
    if (!s) return
    const amount = Math.max(0, Math.floor(Number($('loanAmount').value) || 0))
    const cents = amount * 100
    if (cents <= 0 || cents > s.maxLoanCents) return
    this.callbacks.onBorrow?.(cents)
  }

  confirmRepay() {
    const s = this.state
    if (!s) return
    const amount = Math.max(0, Number($('repayAmount').value) || 0)
    const cents = Math.round(amount * 100)
    if (cents <= 0 || cents > s.walletCents || cents > s.debtCents) return
    this.callbacks.onRepay?.(cents)
  }

  // ===================== 榜单 =====================

  openLeaderboard() {
    this.openModal('leaderboardModal')
    this.callbacks.onLeaderboard?.()
    this.stopLeaderboardTimer()
    this.leaderboardTimer = setInterval(() => {
      this.callbacks.onLeaderboard?.()
    }, 10000)
  }

  stopLeaderboardTimer() {
    if (this.leaderboardTimer) {
      clearInterval(this.leaderboardTimer)
      this.leaderboardTimer = null
    }
  }

  renderLeaderboard(data) {
    if (!data) return
    const list = $('leaderboardList')
    list.textContent = ''
    const selfNick = this.state?.nick || ''
    for (const e of data.entries || []) {
      list.appendChild(this.makeLeaderboardRow(e, e.nick === selfNick))
    }
    const selfBox = $('leaderboardSelf')
    selfBox.textContent = ''
    const inTop = (data.entries || []).some(e => e.nick === data.self?.nick)
    if (data.self && !inTop) {
      const label = document.createElement('div')
      label.className = 'leaderboard-total'
      label.style.textAlign = 'left'
      label.style.padding = '6px 10px 0'
      label.textContent = '你的排名'
      selfBox.appendChild(label)
      selfBox.appendChild(this.makeLeaderboardRow(data.self, true))
      selfBox.classList.remove('hidden')
    } else {
      selfBox.classList.add('hidden')
    }
    $('leaderboardTotal').textContent = `共 ${data.total ?? (data.entries || []).length} 位玩家`
  }

  makeLeaderboardRow(e, isSelf) {
    const row = document.createElement('div')
    row.className = 'leaderboard-row' + (isSelf ? ' me' : '') + (e.bankrupt ? ' is-bankrupt' : '')

    const who = document.createElement('div')
    who.className = 'who'
    const rank = document.createElement('span')
    rank.className = 'rank' + (e.rank <= 3 ? ` top${e.rank}` : '')
    rank.textContent = String(e.rank ?? '-')
    who.appendChild(rank)
    who.appendChild(this.makeAvatar(e))
    const name = document.createElement('span')
    name.className = 'name'
    name.textContent = e.nick || '-'
    who.appendChild(name)
    if (isSelf) {
      const you = document.createElement('span')
      you.className = 'self-mark'
      you.textContent = '（你）'
      who.appendChild(you)
    }
    if (e.bankrupt) {
      const tag = document.createElement('span')
      tag.className = 'tag-bankrupt'
      tag.textContent = '已破产'
      who.appendChild(tag)
    }

    const total = document.createElement('span')
    total.className = 'num'
    total.textContent = fmtCents(e.totalAssetsCents)

    const debt = document.createElement('span')
    debt.className = 'num' + (e.debtCents > 0 ? ' debt' : '')
    debt.textContent = fmtCents(e.debtCents)

    const net = document.createElement('span')
    net.className = 'num net ' + (e.netWorthCents < 0 ? 'neg' : 'pos')
    net.textContent = fmtCentsSigned(e.netWorthCents)

    row.append(who, total, debt, net)
    return row
  }

  makeAvatar(e) {
    if (e.avatar) {
      const img = document.createElement('img')
      img.className = 'mini-avatar'
      img.src = e.avatar
      img.alt = ''
      img.referrerPolicy = 'no-referrer'
      img.onerror = () => {
        const fb = document.createElement('div')
        fb.className = 'mini-fallback'
        fb.textContent = (e.nick || '?').slice(0, 1)
        img.replaceWith(fb)
      }
      return img
    }
    const fb = document.createElement('div')
    fb.className = 'mini-fallback'
    fb.textContent = (e.nick || '?').slice(0, 1)
    return fb
  }

  // ===================== 事件 / 状态 =====================

  onEvent(ev) {
    const action = ev.action
    if (action === 'gift') {
      if (ev.mode === 'interest') this.showToast(`每日赠送 ${fmtCents(ev.amountCents)} 金币将在结算时抵扣利息`)
      else this.showToast(`每日赠送 ${fmtCents(ev.amountCents)} 金币已到账`)
    } else if (action === 'bet') {
      if (ev.feeCents > 0) this.showToast(`已投币 ${fmtCents(ev.amountCents)}（手续费 ${fmtCents(ev.feeCents)}），奖池 ${fmtCents(ev.poolCents)}`)
    } else if (action === 'cashout') {
      this.showToast(`已收手，收回 ${fmtCents(ev.amountCents)} 金币`)
    } else if (action === 'reveal_result') {
      if (ev.result === 'win') this.showToast(`翻倍！奖池 ${fmtCents(ev.poolCents)}`)
      else this.showToast('奖池清零', 'error')
    } else if (action === 'loan') {
      this.showToast(`贷款 ${fmtCents(ev.amountCents)} 金币，当前欠款 ${fmtCents(ev.debtCents)}`)
    } else if (action === 'repay') {
      this.showToast(`还款 ${fmtCents(ev.amountCents)} 金币，剩余欠款 ${fmtCents(ev.debtCents)}`)
    } else if (action === 'settlement') {
      this.showToast(`今日利息 ${fmtCents(ev.interestCents)} 已加入欠款`)
    } else if (action === 'bankrupt') {
      this.showToast('你已破产，无法继续押注', 'error')
    }
  }

  onWindow(msg) {
    if (!msg) return
    if (msg.phase === 'closing') {
      this.showToast('23:59 关门，请尽快结束本局')
    } else if (msg.phase === 'closed') {
      const delay = Math.max(0, (msg.reopenAt || 0) - (msg.serverTime || Date.now()))
      this.showSettlementDelay(delay)
    } else if (msg.phase === 'open') {
      this.hideSettlement()
      this.hideDisconnect()
      this.showToast('已重新开放')
    }
  }

  showSettlementDelay(delayMs) {
    if (this.settleVisible) {
      if (delayMs > 0) this.settleLocalAt = Date.now() + delayMs
      return
    }
    this.settleVisible = true
    this.settleLocalAt = Date.now() + Math.max(0, delayMs || 0)
    this.el.settleOverlay.classList.remove('hidden')
    this.hideDisconnect()
    this.el.settleCountdown.textContent = mmss(Math.max(0, delayMs || 0))
  }

  hideSettlement() {
    this.settleVisible = false
    this.el.settleOverlay.classList.add('hidden')
  }

  setConnected(ok) {
    this.el.connDot.classList.toggle('ok', !!ok)
    this.el.connDot.classList.toggle('bad', !ok)
    if (ok) {
      this.hideDisconnect()
      if (this.disconnectTimer) {
        clearTimeout(this.disconnectTimer)
        this.disconnectTimer = null
      }
      return
    }
    if (this.disconnectTimer) clearTimeout(this.disconnectTimer)
    this.disconnectTimer = setTimeout(() => {
      this.disconnectTimer = null
      if (this.settleVisible) return
      this.el.disconnectOverlay.classList.remove('hidden')
    }, 1200)
  }

  hideDisconnect() {
    this.el.disconnectOverlay.classList.add('hidden')
  }

  showFatal(msg) {
    this.el.disconnectText.textContent = msg || '连接已关闭'
    this.el.statusText.textContent = msg || '连接已关闭'
    this.el.disconnectOverlay.classList.remove('hidden')
    if (this.disconnectTimer) {
      clearTimeout(this.disconnectTimer)
      this.disconnectTimer = null
    }
  }

  setSoundMuted(muted) {
    this.el.btnSound.classList.toggle('muted', !!muted)
    this.el.btnSound.title = muted ? '音效已静音' : '音效开启'
    this.el.btnSound.setAttribute('aria-label', muted ? '开启音效' : '关闭音效')
  }

  showToast(msg, type = 'info') {
    if (!msg) return
    this.el.toast.textContent = msg
    this.el.toast.classList.toggle('error', type === 'error')
    this.el.toast.classList.add('show')
    if (this.toastTimer) clearTimeout(this.toastTimer)
    this.toastTimer = setTimeout(() => {
      this.el.toast.classList.remove('show')
    }, 2800)
  }

  destroy() {
    clearInterval(this.timer)
    this.stopLeaderboardTimer()
    if (this.toastTimer) clearTimeout(this.toastTimer)
    if (this.disconnectTimer) clearTimeout(this.disconnectTimer)
  }
}
