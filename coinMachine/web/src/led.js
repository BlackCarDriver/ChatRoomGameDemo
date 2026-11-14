import * as THREE from 'three'

// 7 段数码管。段编号：a 上，b 右上，c 右下，d 下，e 左下，f 左上，g 中。
const SEGMENTS = {
  0: 'abcdef',
  1: 'bc',
  2: 'abged',
  3: 'abgcd',
  4: 'fgbc',
  5: 'afgcd',
  6: 'afgecd',
  7: 'abc',
  8: 'abcdefg',
  9: 'abcdfg'
}

export function createLED() {
  const W = 512
  const H = 128
  const canvas = document.createElement('canvas')
  canvas.width = W
  canvas.height = H
  const ctx = canvas.getContext('2d')
  const texture = new THREE.CanvasTexture(canvas)
  texture.colorSpace = THREE.SRGBColorSpace
  texture.minFilter = THREE.LinearFilter
  texture.magFilter = THREE.LinearFilter
  texture.generateMipmaps = false

  let value = 0
  let textMode = ''
  let dirty = true
  let rollingUntil = 0
  let lastRoll = 0

  function segOn(on, x, y, w, h) {
    ctx.fillStyle = on ? '#ff3347' : '#2a0709'
    if (on) {
      ctx.shadowColor = '#ff2a3c'
      ctx.shadowBlur = 14
    } else {
      ctx.shadowBlur = 0
    }
    roundRect(x, y, w, h, Math.min(w, h) * 0.35)
    ctx.fill()
  }

  function roundRect(x, y, w, h, r) {
    const rr = Math.min(r, w / 2, h / 2)
    ctx.beginPath()
    ctx.moveTo(x + rr, y)
    ctx.arcTo(x + w, y, x + w, y + h, rr)
    ctx.arcTo(x + w, y + h, x, y + h, rr)
    ctx.arcTo(x, y + h, x, y, rr)
    ctx.arcTo(x, y, x + w, y, rr)
    ctx.closePath()
  }

  function drawDigit(d, x, y, dw, dh, t) {
    const on = SEGMENTS[d] || ''
    const has = s => on.includes(s)
    // 横向段长 dw*0.72，纵向段高 dh*0.38
    const hw = dw * 0.72
    const vh = dh * 0.38
    segOn(has('a'), x + dw * 0.14, y, hw, t)
    segOn(has('b'), x + dw - t * 0.6, y + dh * 0.06, t, vh)
    segOn(has('c'), x + dw - t * 0.6, y + dh * 0.54, t, vh)
    segOn(has('d'), x + dw * 0.14, y + dh - t, hw, t)
    segOn(has('e'), x + t * 0.6 - t, y + dh * 0.54, t, vh)
    segOn(has('f'), x + t * 0.6 - t, y + dh * 0.06, t, vh)
    segOn(has('g'), x + dw * 0.14, y + dh * 0.47, hw, t)
  }

  function drawBackground() {
    ctx.clearRect(0, 0, W, H)
    // 黑玻璃底
    const g = ctx.createLinearGradient(0, 0, 0, H)
    g.addColorStop(0, '#0d0407')
    g.addColorStop(0.5, '#070205')
    g.addColorStop(1, '#0b0306')
    ctx.fillStyle = g
    ctx.fillRect(0, 0, W, H)

    // 暗红网格
    ctx.strokeStyle = 'rgba(255,42,60,0.06)'
    ctx.lineWidth = 1
    for (let x = 0; x < W; x += 16) {
      ctx.beginPath(); ctx.moveTo(x, 0); ctx.lineTo(x, H); ctx.stroke()
    }
    for (let y = 0; y < H; y += 16) {
      ctx.beginPath(); ctx.moveTo(0, y); ctx.lineTo(W, y); ctx.stroke()
    }

    // 内边框
    ctx.strokeStyle = 'rgba(255,42,60,0.22)'
    ctx.lineWidth = 2
    roundRect(3, 3, W - 6, H - 6, 8)
    ctx.stroke()
  }

  function draw() {
    drawBackground()
    if (textMode) {
      ctx.save()
      ctx.font = `bold ${Math.floor(H * 0.42)}px "Arial Black", "PingFang SC", "Microsoft YaHei", sans-serif`
      ctx.textAlign = 'center'
      ctx.textBaseline = 'middle'
      ctx.shadowColor = '#ff2a3c'
      ctx.shadowBlur = 24
      ctx.fillStyle = '#ff3347'
      ctx.fillText(textMode, W / 2, H / 2 + 2)
      ctx.restore()
      texture.needsUpdate = true
      dirty = false
      return
    }

    const digits = 6
    const gap = 28
    const margin = 18
    const dw = (W - margin * 2 - gap * (digits - 1)) / digits
    const dh = H - 24
    const y = 12
    const t = Math.max(4, Math.floor(dh * 0.11))
    const s = String(Math.max(0, Math.min(999999, Math.floor(value)))).padStart(digits, '0')
    for (let i = 0; i < digits; i++) {
      drawDigit(s[i], margin + i * (dw + gap), y, dw, dh, t)
    }
    ctx.shadowBlur = 0

    // 扫描线
    ctx.fillStyle = 'rgba(0,0,0,0.18)'
    for (let yy = 0; yy < H; yy += 4) ctx.fillRect(0, yy, W, 1)

    texture.needsUpdate = true
    dirty = false
  }

  function setValue(v) {
    rollingUntil = 0
    const n = Math.max(0, Math.min(999999, Math.floor(Number(v) || 0)))
    if (n !== value || textMode) {
      value = n
      textMode = ''
      dirty = true
    }
  }

  function setText(t) {
    rollingUntil = 0
    if (textMode !== t) {
      textMode = t || ''
      dirty = true
    }
  }

  function startRoll(ms) {
    rollingUntil = performance.now() + ms
    lastRoll = 0
  }

  function update(now) {
    if (rollingUntil) {
      if (now < rollingUntil) {
        if (now - lastRoll > 55) {
          lastRoll = now
          const n = Math.floor(Math.random() * 999999)
          const old = value
          value = n
          textMode = ''
          dirty = true
          if (old === n) dirty = true
        }
      } else {
        rollingUntil = 0
        dirty = true
      }
    }
    if (dirty) draw()
  }

  // 首帧
  draw()
  return { texture, setValue, setText, startRoll, update, canvas }
}
