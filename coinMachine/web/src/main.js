import { GameState } from './state.js'
import { Net } from './net.js'
import { UI } from './ui.js'
import { AudioEngine } from './audio.js'
import { SceneView } from './scene.js'
import { uuid } from './format.js'

const params = new URLSearchParams(location.search)
const MOCK = params.get('mock') === '1'
const DEBUG = params.get('debug') === '1'

const state = new GameState()
const ui = new UI()
const audio = new AudioEngine()
const canvas = document.getElementById('scene')
const scene = new SceneView(canvas, {
  onButton: action => onMachineButton(action),
  onCoinClink: intensity => audio.play('clink', intensity)
})

window.__coin = { state, ui, scene }

let busy = false
let revealTickTimer = null
let mockRevealTimer = null

function setBusy(ms = 1500) {
  busy = true
  setTimeout(() => { busy = false }, ms)
}

function sendAction(type, extra = {}) {
  if (busy) return
  if (state.isClosed) {
    ui.showToast('结算中，暂停参与', 'error')
    return
  }
  if (state.bankrupt) {
    ui.showToast('已破产，无法继续操作', 'error')
    return
  }
  if (!state.connected) {
    ui.showToast('连接未就绪，请稍候', 'error')
    return
  }
  setBusy()
  net.send({ type, requestId: uuid(), ...extra })
}

function onMachineButton(action) {
  if (busy) return
  audio.unlock()
  audio.play('click')
  if (!state.connected) {
    ui.showToast('连接未就绪，请稍候', 'error')
    return
  }
  if (state.isClosed) {
    ui.showToast('结算中，暂停参与', 'error')
    return
  }
  if (state.bankrupt) {
    ui.showToast('已破产，无法继续押注', 'error')
    return
  }
  if (scene.machine.isTrapdoorBusy()) {
    ui.showToast('奖池正在落币，请稍候')
    return
  }
  if (scene.coins.flyingIn) {
    ui.showToast('硬币正在进入奖池，请稍候')
    return
  }
  if (action === 'left') {
    if (state.phase === 'bet') {
      doCashout()
    } else if (state.phase === 'idle') {
      ui.openBet()
    }
  } else if (action === 'right') {
    if (state.phase === 'bet') {
      doReveal()
    }
  }
}

function doCashout() {
  if (MOCK) return mockCashout()
  sendAction('cashout')
}

function doReveal() {
  if (MOCK) return mockReveal()
  sendAction('reveal')
  startRevealTicks(2600)
}

function startRevealTicks(duration) {
  stopRevealTicks()
  const start = performance.now()
  const tick = () => {
    const elapsed = performance.now() - start
    if (elapsed > duration || !state.connected) return
    audio.play('tick')
    const p = Math.min(1, elapsed / duration)
    const delay = Math.max(120, 300 - 180 * p)
    revealTickTimer = setTimeout(tick, delay)
  }
  tick()
}

function stopRevealTicks() {
  if (revealTickTimer) {
    clearTimeout(revealTickTimer)
    revealTickTimer = null
  }
}

// ===================== 网络 =====================

const net = new Net({
  onState: msg => {
    busy = false
    state.connected = true
    state.connecting = false
    state.applyState(msg)
    ui.render(state)
    scene.setState(state)
  },
  onEvent: ev => {
    busy = false
    stopRevealTicks()
    ui.onEvent(ev)
    scene.onEvent(ev, state)
    playEventSound(ev)
  },
  onWindow: msg => {
    state.applyWindow(msg)
    ui.onWindow(msg)
    scene.setState(state)
  },
  onLeaderboard: msg => {
    state.leaderboard = msg
    ui.renderLeaderboard(msg)
  },
  onWelcome: msg => {
    state.nick = msg.nick || state.nick
    if (msg.window) state.applyWindow(msg.window)
    ui.render(state)
  },
  onStatus: (text, ok) => {
    if (!ok) busy = false
    state.connecting = false
    state.connected = !!ok
    ui.setConnected(!!ok)
    ui.renderStatus(state)
    if (!ok) scene.setState(state)
  },
  onError: msg => { busy = false; ui.showToast(msg, 'error') },
  onFatal: msg => { busy = false; ui.showFatal(msg) },
  onServerTime: ms => {
    if (ms) {
      state.serverTime = ms
      state.clockOffset = ms - Date.now()
      ui.tick()
    }
  },
  onSettlement: delay => ui.showSettlementDelay(delay),
  getClockOffset: () => state.clockOffset
})

function playEventSound(ev) {
  switch (ev.action) {
    case 'bet':
      audio.play('coin')
      break
    case 'reveal_result':
      audio.play(ev.result === 'win' ? 'win' : 'lose')
      break
    case 'cashout':
      audio.play('cashout')
      break
    case 'loan':
      audio.play('loan')
      break
    case 'repay':
      audio.play('repay')
      break
    case 'gift':
      audio.play('coin')
      break
    case 'settlement':
      audio.play('warn')
      break
    case 'bankrupt':
      audio.play('bankrupt')
      break
  }
}

// ===================== UI 回调 =====================

ui.on({
  onBet: cents => {
    if (MOCK) return mockBet(cents)
    sendAction('bet', { amountCents: cents })
    audio.play('coin')
  },
  onCashout: doCashout,
  onBorrow: cents => {
    if (MOCK) return mockBorrow(cents)
    if (busy) return
    setBusy()
    net.send({ type: 'borrow', amountCents: cents, requestId: uuid() })
  },
  onRepay: cents => {
    if (MOCK) return mockRepay(cents)
    if (busy) return
    setBusy()
    net.send({ type: 'repay', amountCents: cents, requestId: uuid() })
  },
  onLeaderboard: () => {
    if (MOCK) return mockLeaderboard()
    if (!state.connected) return
    net.send({ type: 'leaderboard' })
  },
  onToggleSound: () => {
    audio.unlock()
    return audio.toggleMuted()
  },
  onModalChange: open => scene.setInputEnabled(!open)
})

ui.setSoundMuted(audio.muted)

// 首次交互解锁音频
for (const ev of ['pointerdown', 'keydown']) {
  document.addEventListener(ev, () => audio.unlock(), { once: true })
}

// 普通 UI 按钮的点击音
document.addEventListener('click', e => {
  const btn = e.target?.closest?.('button')
  if (btn) audio.play('click')
})

// 键盘快捷键
document.addEventListener('keydown', e => {
  if (ui.modalOpen) return
  const tag = (e.target?.tagName || '').toLowerCase()
  if (tag === 'input' || tag === 'textarea') return
  if (e.code === 'Space') {
    e.preventDefault()
    onMachineButton('left')
  } else if (e.code === 'Enter') {
    e.preventDefault()
    onMachineButton('right')
  }
})

// ===================== 启动 =====================

if (MOCK) {
  startMock()
} else {
  state.connecting = true
  ui.render(state)
  scene.setState(state)
  net.start()
}

// ===================== 调试 =====================

if (DEBUG) {
  const panel = document.getElementById('debugPanel')
  panel.classList.remove('hidden')
  document.getElementById('debugClose').addEventListener('click', () => panel.classList.add('hidden'))
  const fpsEl = document.getElementById('debugFps')
  setInterval(() => {
    fpsEl.textContent = `${Math.round(scene.fps)} fps · ${scene.quality}`
  }, 500)
  for (const btn of document.querySelectorAll('[data-debug]')) {
    btn.addEventListener('click', () => {
      const what = btn.dataset.debug
      if (what === 'win') debugResult(true)
      else if (what === 'lose') debugResult(false)
      else if (what === 'cashout') scene.onEvent({ action: 'cashout', amountCents: state.poolCents }, state)
      else if (what === 'bankrupt') {
        if (state.account) {
          state.account.bankrupt = true
          scene.setState(state)
          ui.render(state)
        }
      } else if (what === 'settle') ui.showSettlementDelay(60000)
      else if (what === 'big') {
        if (state.account) {
          state.account.walletCents = 12345678900
          state.account.netWorthCents = 98765432100
          ui.render(state)
        }
      } else if (what === 'quality') {
        scene.setQuality(scene.quality === 'high' ? 'medium' : scene.quality === 'medium' ? 'low' : 'high')
      }
    })
  }
}

function debugResult(win) {
  const pool = win ? Math.max(200, state.poolCents * 2) : 0
  scene.onEvent({ action: 'reveal_result', result: win ? 'win' : 'lose', poolCents: pool }, state)
  audio.play(win ? 'win' : 'lose')
}

// ===================== 本地 mock =====================

function mockApply(extra = {}) {
  const acc = state.account
  acc.totalAssetsCents = acc.walletCents + state.poolCents
  acc.netWorthCents = acc.totalAssetsCents - acc.debtCents
  acc.dailyInterestCents = Math.ceil(acc.debtCents / 100)
  acc.maxLoanCents = Math.max(0, 100 * (acc.walletCents - acc.debtCents) - acc.debtCents)
  acc.freePlaysLeft = Math.max(0, acc.freePlaysMax - acc.freePlaysUsed)
  const nextIndex = acc.freePlaysUsed + 1
  acc.nextFeeCents = nextIndex <= acc.freePlaysMax ? 0 : 100 * Math.max(1, Math.floor(nextIndex / 10))
  acc.minBetCents = acc.nextFeeCents + 100
  state.connected = true
  state.serverTime = Date.now()
  state.clockOffset = 0
  ui.render(state)
  scene.setState(state)
}

function startMock() {
  const now = Date.now()
  state.connected = true
  state.nick = '测试玩家'
  state.account = {
    nick: '测试玩家',
    avatar: '',
    frame: '',
    walletCents: 10000,
    debtCents: 0,
    totalAssetsCents: 10000,
    netWorthCents: 10000,
    freePlaysUsed: 0,
    freePlaysLeft: 10,
    freePlaysMax: 10,
    dailyInterestCents: 0,
    maxLoanCents: 1000000,
    minBetCents: 100,
    nextFeeCents: 0,
    giftPendingCents: 0,
    lastGiftDate: '',
    dailyGiftCents: 1000,
    giftClaimedToday: true,
    bankrupt: false,
    stats: {}
  }
  state.round = { phase: 'idle', betCents: 0, poolCents: 0, revealAt: 0, result: '', resultAt: 0 }
  state.window = { phase: 'open', closeAt: now + 3600_000, reopenAt: now + 7200_000 }
  state.serverTime = now
  mockApply()
  ui.showToast('MOCK 模式：本地模拟，不连接服务器')
}

function mockBet(cents) {
  if (state.phase !== 'idle' || state.bankrupt) return
  const acc = state.account
  const fee = acc.nextFeeCents
  const pool = cents - fee
  if (cents > acc.walletCents || pool <= 0) return
  acc.walletCents -= cents
  acc.freePlaysUsed++
  state.round = { phase: 'bet', betCents: cents, poolCents: pool, revealAt: 0, result: '', resultAt: 0 }
  const ev = { action: 'bet', amountCents: cents, feeCents: fee, poolCents: pool, walletCents: acc.walletCents, freePlaysLeft: Math.max(0, acc.freePlaysMax - acc.freePlaysUsed) }
  ui.onEvent(ev)
  scene.onEvent(ev, state)
  audio.play('coin')
  mockApply()
}

function mockCashout() {
  if (state.phase !== 'bet' || state.poolCents <= 0) return
  const amount = state.poolCents
  state.account.walletCents += amount
  state.round = { phase: 'idle', betCents: 0, poolCents: 0, revealAt: 0, result: '', resultAt: 0 }
  const ev = { action: 'cashout', amountCents: amount, walletCents: state.account.walletCents }
  ui.onEvent(ev)
  scene.onEvent(ev, state)
  audio.play('cashout')
  mockApply()
}

function mockReveal() {
  if (state.phase !== 'bet' || state.poolCents <= 0) return
  const revealAt = Date.now() + 2500
  state.round.phase = 'revealing'
  state.round.revealAt = revealAt
  const ev = { action: 'reveal_start', revealAt, poolCents: state.poolCents }
  ui.onEvent(ev)
  scene.onEvent(ev, state)
  audio.play('click')
  startRevealTicks(2600)
  mockApply()
  if (mockRevealTimer) clearTimeout(mockRevealTimer)
  mockRevealTimer = setTimeout(() => {
    const win = Math.random() < 0.5
    if (win) {
      state.round.poolCents *= 2
      state.round.phase = 'bet'
      state.round.result = 'win'
    } else {
      state.round.poolCents = 0
      state.round.phase = 'idle'
      state.round.result = 'lose'
    }
    state.round.resultAt = Date.now()
    state.round.revealAt = 0
    const res = { action: 'reveal_result', result: win ? 'win' : 'lose', poolCents: state.round.poolCents, walletCents: state.account.walletCents }
    stopRevealTicks()
    ui.onEvent(res)
    scene.onEvent(res, state)
    audio.play(win ? 'win' : 'lose')
    mockApply()
  }, 2500)
}

function mockBorrow(cents) {
  if (state.bankrupt) return
  if (cents > state.account.maxLoanCents) return
  state.account.walletCents += cents
  state.account.debtCents += cents
  const ev = { action: 'loan', amountCents: cents, walletCents: state.account.walletCents, debtCents: state.account.debtCents }
  ui.onEvent(ev)
  audio.play('loan')
  mockApply()
}

function mockRepay(cents) {
  if (state.bankrupt) return
  if (cents > state.account.walletCents || cents > state.account.debtCents) return
  state.account.walletCents -= cents
  state.account.debtCents -= cents
  const ev = { action: 'repay', amountCents: cents, walletCents: state.account.walletCents, debtCents: state.account.debtCents }
  ui.onEvent(ev)
  audio.play('repay')
  mockApply()
}

function mockLeaderboard() {
  const me = {
    rank: 3,
    nick: state.nick,
    avatar: '',
    frame: '',
    totalAssetsCents: state.account.walletCents + state.poolCents,
    debtCents: state.account.debtCents,
    netWorthCents: state.account.netWorthCents,
    bankrupt: state.account.bankrupt
  }
  const entries = [
    { rank: 1, nick: '霓虹赌徒', avatar: '', totalAssetsCents: 5000000, debtCents: 0, netWorthCents: 5000000, bankrupt: false },
    { rank: 2, nick: '负债之王', avatar: '', totalAssetsCents: 1200000, debtCents: 300000, netWorthCents: 900000, bankrupt: false },
    me,
    { rank: 4, nick: '梭哈新手', avatar: '', totalAssetsCents: 50000, debtCents: 100000, netWorthCents: -50000, bankrupt: false },
    { rank: 5, nick: '已破产', avatar: '', totalAssetsCents: 0, debtCents: 800000, netWorthCents: -800000, bankrupt: true }
  ]
  ui.renderLeaderboard({ entries, self: me, total: entries.length })
}
