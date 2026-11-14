// 子路径部署时（如 /coinMachine/），HTTP 与 WS 都必须带上前缀。
function appBasePath() {
  const path = location.pathname
  return path.endsWith('/') ? path : path.slice(0, path.lastIndexOf('/') + 1)
}

// 聊天室鉴权 + 游戏 WebSocket。
export class Net {
  constructor(handlers = {}) {
    this.h = handlers
    this.inIframe = window.parent !== window
    this.token = ''
    this.tokenExpireAt = 0
    this.nick = ''
    this.ws = null
    this.connected = false
    this.reconnectTimer = null
    this.tokenRetry = null
    this.pingTimer = null
    this.lastPong = 0
    this.backoff = 1000
    this.lastWindow = null
    this.initConfig = null
    this.stopped = false

    window.addEventListener('message', e => this.onShellMessage(e))
  }

  start() {
    if (this.inIframe) {
      this.postToParent({ type: 'READY' })
      this.requestTokenLoop()
    } else {
      this.exchangeTicket()
    }
  }

  requestTokenLoop() {
    if (this.stopped || this.token) return
    this.requestToken()
    this.tokenRetry = setTimeout(() => this.requestTokenLoop(), 1500)
  }

  postToParent(msg) {
    try {
      // 与现有示例页面一致，使用 '*'；聊天室外壳会按消息类型与 JWT 做后续校验。
      window.parent.postMessage(msg, '*')
    } catch (e) {}
  }

  onShellMessage(e) {
    if (!this.inIframe) return
    // 聊天室外壳在不同版本里可能是父窗口或嵌套壳层；身份最终以服务端 JWT 验签为准。
    const data = e.data || {}
    if (data.type === 'INIT') {
      this.initConfig = data.config || null
      this.h.onInit?.(data.config)
    } else if (data.type === 'TOKEN' && data.token) {
      if (this.tokenRetry) {
        clearTimeout(this.tokenRetry)
        this.tokenRetry = null
      }
      if (data.token === this.token && this.ws && (this.ws.readyState === WebSocket.OPEN || this.ws.readyState === WebSocket.CONNECTING)) return
      this.token = data.token
      this.tokenExpireAt = Number(data.expireAt || 0)
      this.h.onToken?.()
      this.connect()
    } else if (data.type === 'ROOM_STATE') {
      this.h.onRoomState?.(data)
    }
  }

  async exchangeTicket() {
    const hash = (location.hash || '').replace(/^#/, '')
    const params = new URLSearchParams(hash)
    const ticket = params.get('ticket')
    if (!ticket) {
      this.h.onError?.('缺少票据，请从聊天室房间进入游戏')
      return
    }
    try { history.replaceState(null, '', location.pathname + location.search) } catch (e) {}
    try {
      const resp = await fetch(`${appBasePath()}exchange`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ ticket })
      })
      if (!resp.ok) throw new Error('exchange failed')
      const data = await resp.json()
      this.token = data.token
      this.nick = data.nick || ''
      this.tokenExpireAt = 0
      this.connect()
    } catch (e) {
      this.h.onError?.('票据兑换失败，请从房间重新进入')
    }
  }

  tokenRemain() {
    if (!this.tokenExpireAt) return Infinity
    return this.tokenExpireAt - Math.floor(Date.now() / 1000)
  }

  requestToken() {
    if (this.inIframe) {
      this.postToParent({ type: 'REQUEST_TOKEN' })
    } else {
      this.h.onError?.('凭证已过期，请从房间重新进入')
    }
  }

  connect() {
    if (this.stopped) return
    if (!this.token) {
      this.requestToken()
      return
    }
    if (this.tokenRemain() < 30) {
      this.requestToken()
      return
    }
    this.closeSocket()
    const proto = location.protocol === 'https:' ? 'wss:' : 'ws:'
    const url = `${proto}//${location.host}${appBasePath()}ws?token=${encodeURIComponent(this.token)}`
    let sock
    try {
      sock = new WebSocket(url)
    } catch (e) {
      this.scheduleReconnect(2000)
      return
    }
    this.ws = sock

    sock.onopen = () => {
      if (this.ws !== sock) return
      this.connected = true
      this.backoff = 1000
      this.lastPong = Date.now()
      this.h.onStatus?.('已连接', true)
      this.startPing()
      this.send({ type: 'sync' })
    }

    sock.onmessage = ev => {
      if (this.ws !== sock) return
      let msg
      try { msg = JSON.parse(ev.data) } catch (e) { return }
      this.dispatch(msg)
    }

    sock.onclose = ev => {
      if (this.ws !== sock) return
      this.ws = null
      this.connected = false
      this.stopPing()
      this.h.onStatus?.('已断开', false)
      if (ev.code === 4000) {
        this.scheduleAfterWindow()
        return
      }
      if ([4001, 4002, 4003, 4005, 4006].includes(ev.code)) {
        const msg = {
          4001: '房间已关闭，请退出游戏',
          4002: '已在其他窗口打开，本窗口停止',
          4003: '密钥版本过旧，请联系管理员',
          4005: '密钥错误，请联系管理员',
          4006: '协议错误，请刷新页面'
        }[ev.code] || `连接已关闭（${ev.code}）`
        this.h.onFatal?.(msg)
        return
      }
      this.scheduleReconnect(this.backoff)
      this.backoff = Math.min(10000, Math.round(this.backoff * 1.6))
    }

    sock.onerror = () => {
      if (this.ws !== sock) return
      this.h.onStatus?.('连接异常', false)
    }
  }

  scheduleAfterWindow() {
    if (this.stopped) return
    let delay = 60000
    if (this.lastWindow && this.lastWindow.reopenAt) {
      const nowServer = Date.now() + (this.h.getClockOffset?.() || 0)
      delay = Math.max(1000, this.lastWindow.reopenAt - nowServer + 500)
    }
    // 结算窗口只有 2 分钟；异常情况下也不要等待过久
    delay = Math.min(120000, delay)
    this.h.onSettlement?.(delay)
    this.scheduleReconnect(delay)
  }

  scheduleReconnect(delay) {
    if (this.stopped) return
    if (this.reconnectTimer) clearTimeout(this.reconnectTimer)
    this.reconnectTimer = setTimeout(() => {
      this.reconnectTimer = null
      if (this.tokenRemain() < 30) {
        this.requestToken()
      } else {
        this.connect()
      }
    }, delay)
  }

  startPing() {
    this.stopPing()
    this.pingTimer = setInterval(() => {
      if (!this.connected) return
      if (Date.now() - this.lastPong > 65000) {
        this.closeSocket()
        this.scheduleReconnect(1000)
        return
      }
      this.send({ type: 'ping' })
    }, 20000)
  }

  stopPing() {
    if (this.pingTimer) {
      clearInterval(this.pingTimer)
      this.pingTimer = null
    }
  }

  closeSocket() {
    if (!this.ws) return
    const old = this.ws
    this.ws = null
    old.onopen = null
    old.onmessage = null
    old.onclose = null
    old.onerror = null
    try { old.close() } catch (e) {}
  }

  dispatch(msg) {
    switch (msg.type) {
      case 'welcome':
        this.nick = msg.nick || this.nick
        this.lastWindow = msg.window || this.lastWindow
        this.h.onWelcome?.(msg)
        break
      case 'state':
        if (msg.window) this.lastWindow = msg.window
        this.h.onState?.(msg)
        break
      case 'event':
        this.h.onEvent?.(msg)
        break
      case 'leaderboard':
        this.h.onLeaderboard?.(msg)
        break
      case 'window':
        this.lastWindow = msg
        this.h.onWindow?.(msg)
        break
      case 'error':
        this.h.onError?.(msg.message || '操作失败')
        break
      case 'pong':
        this.lastPong = Date.now()
        this.h.onServerTime?.(msg.serverTime)
        break
    }
  }

  send(obj) {
    if (!this.ws || this.ws.readyState !== WebSocket.OPEN) {
      this.h.onError?.('连接未就绪，请稍候')
      return false
    }
    try {
      this.ws.send(JSON.stringify(obj))
      return true
    } catch (e) {
      return false
    }
  }

  stop() {
    this.stopped = true
    this.stopPing()
    if (this.reconnectTimer) clearTimeout(this.reconnectTimer)
    if (this.tokenRetry) clearTimeout(this.tokenRetry)
    this.closeSocket()
  }
}
