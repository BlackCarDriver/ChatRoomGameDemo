import * as THREE from 'three'
import { RoundedBoxGeometry } from 'three/addons/geometries/RoundedBoxGeometry.js'
import { createLED } from './led.js'

const FONT_CN = '"PingFang SC", "Microsoft YaHei", "Noto Sans SC", sans-serif'
const FONT_EN = '"Arial Black", "Impact", "Helvetica Neue", sans-serif'

export function makeTextTexture(opts) {
  const {
    text = '',
    sub = '',
    color = '#00f0ff',
    subColor = '#ff2bd6',
    width = 1024,
    height = 256,
    font = `800 96px ${FONT_CN}`,
    subFont = `600 34px ${FONT_EN}`,
    bg = null,
    border = false,
    glow = 28,
    align = 'center',
    x = null
  } = opts || {}
  const canvas = document.createElement('canvas')
  canvas.width = width
  canvas.height = height
  const ctx = canvas.getContext('2d')
  ctx.clearRect(0, 0, width, height)

  if (bg) {
    ctx.fillStyle = bg
    roundRect(ctx, 8, 8, width - 16, height - 16, 18)
    ctx.fill()
  }
  if (border) {
    ctx.strokeStyle = color
    ctx.lineWidth = 4
    ctx.shadowColor = color
    ctx.shadowBlur = 18
    roundRect(ctx, 8, 8, width - 16, height - 16, 18)
    ctx.stroke()
    ctx.shadowBlur = 0
  }

  ctx.textAlign = align
  ctx.textBaseline = 'middle'
  const cx = x == null ? width / 2 : x
  ctx.font = font
  ctx.fillStyle = color
  ctx.shadowColor = color
  ctx.shadowBlur = glow
  ctx.fillText(text, cx, sub ? height * 0.41 : height * 0.52)

  if (sub) {
    ctx.font = subFont
    ctx.fillStyle = subColor
    ctx.shadowColor = subColor
    ctx.shadowBlur = Math.max(8, glow * 0.6)
    ctx.fillText(sub, cx, height * 0.74)
  }
  ctx.shadowBlur = 0

  const texture = new THREE.CanvasTexture(canvas)
  texture.colorSpace = THREE.SRGBColorSpace
  texture.minFilter = THREE.LinearFilter
  texture.magFilter = THREE.LinearFilter
  texture.generateMipmaps = false
  return { canvas, texture }
}

function roundRect(ctx, x, y, w, h, r) {
  const rr = Math.min(r, w / 2, h / 2)
  ctx.beginPath()
  ctx.moveTo(x + rr, y)
  ctx.arcTo(x + w, y, x + w, y + h, rr)
  ctx.arcTo(x + w, y + h, x, y + h, rr)
  ctx.arcTo(x, y + h, x, y, rr)
  ctx.arcTo(x, y, x + w, y, rr)
  ctx.closePath()
}

export function createMachine(scene) {
  const group = new THREE.Group()
  group.name = 'machine'
  scene.add(group)

  const led = createLED()

  // ---------- 材质 ----------
  const bodyMat = new THREE.MeshStandardMaterial({ color: 0x0a0e18, metalness: 0.88, roughness: 0.3, envMapIntensity: 1.2 })
  const panelMat = new THREE.MeshStandardMaterial({ color: 0x05070c, metalness: 0.65, roughness: 0.45 })
  const trimMat = new THREE.MeshStandardMaterial({ color: 0x1a2233, metalness: 0.95, roughness: 0.18, envMapIntensity: 1.4 })
  const cyanMat = new THREE.MeshBasicMaterial({ color: 0x00f0ff, toneMapped: false })
  const magentaMat = new THREE.MeshBasicMaterial({ color: 0xff2bd6, toneMapped: false })
  const goldMat = new THREE.MeshStandardMaterial({ color: 0xffcc66, metalness: 0.95, roughness: 0.22, emissive: 0x3a1f00, emissiveIntensity: 0.35 })
  const darkGlow = new THREE.MeshBasicMaterial({ color: 0x001018 })
  const atmMat = new THREE.MeshStandardMaterial({ color: 0x0b0e14, metalness: 0.55, roughness: 0.5 })

  // ---------- 底座与机身 ----------
  const base = new THREE.Mesh(new RoundedBoxGeometry(2.95, 0.28, 1.6, 3, 0.06), bodyMat)
  base.position.y = 0.14
  base.castShadow = true
  base.receiveShadow = true
  group.add(base)

  const body = new THREE.Mesh(new RoundedBoxGeometry(2.45, 2.95, 1.12, 5, 0.1), bodyMat)
  body.position.y = 1.76
  body.castShadow = true
  body.receiveShadow = true
  group.add(body)

  const front = new THREE.Mesh(new RoundedBoxGeometry(2.12, 2.45, 0.09, 4, 0.06), panelMat)
  front.position.set(0, 1.68, 0.57)
  front.receiveShadow = true
  group.add(front)

  // 前面板内侧的暗色玻璃
  const glassPanel = new THREE.Mesh(
    new THREE.PlaneGeometry(1.95, 2.28),
    new THREE.MeshPhysicalMaterial({
      color: 0x06101c,
      metalness: 0.2,
      roughness: 0.08,
      transparent: true,
      opacity: 0.28,
      side: THREE.DoubleSide
    })
  )
  glassPanel.position.set(0, 1.68, 0.625)
  group.add(glassPanel)

  // 侧边霓虹灯带
  for (const [x, mat, color] of [[-1.22, cyanMat, 0x00f0ff], [1.22, magentaMat, 0xff2bd6]]) {
    const rail = new THREE.Mesh(new THREE.BoxGeometry(0.035, 2.45, 0.035), mat)
    rail.position.set(x, 1.78, 0.63)
    group.add(rail)
    const glow = new THREE.Mesh(
      new THREE.PlaneGeometry(0.16, 2.5),
      new THREE.MeshBasicMaterial({ color, transparent: true, opacity: 0.16, blending: THREE.AdditiveBlending, depthWrite: false })
    )
    glow.position.set(x, 1.78, 0.62)
    group.add(glow)
  }

  // ---------- 顶部招牌 ----------
  const marquee = new THREE.Mesh(new RoundedBoxGeometry(2.32, 0.62, 0.95, 4, 0.08), bodyMat)
  marquee.position.set(0, 3.22, 0)
  marquee.castShadow = true
  group.add(marquee)

  const sign = makeTextTexture({
    text: '梭哈模拟器',
    sub: 'ALL-IN SIMULATOR',
    color: '#00f0ff',
    subColor: '#ff2bd6',
    width: 1024,
    height: 256,
    font: `800 92px ${FONT_CN}`,
    subFont: `700 32px ${FONT_EN}`,
    bg: 'rgba(3,6,14,0.94)',
    border: true,
    glow: 26
  })
  const signMesh = new THREE.Mesh(
    new THREE.PlaneGeometry(2.12, 0.53),
    new THREE.MeshBasicMaterial({ map: sign.texture, transparent: true, toneMapped: false })
  )
  signMesh.position.set(0, 3.26, 0.486)
  group.add(signMesh)

  const signGlow = new THREE.Mesh(
    new THREE.PlaneGeometry(2.4, 0.8),
    new THREE.MeshBasicMaterial({ color: 0x00f0ff, transparent: true, opacity: 0.07, blending: THREE.AdditiveBlending, depthWrite: false })
  )
  signGlow.position.set(0, 3.26, 0.481)
  group.add(signGlow)

  // ---------- LED 屏 ----------
  const bezel = new THREE.Mesh(new RoundedBoxGeometry(1.92, 0.78, 0.08, 4, 0.05), trimMat)
  bezel.position.set(0, 2.54, 0.63)
  group.add(bezel)

  const ledMesh = new THREE.Mesh(
    new THREE.PlaneGeometry(1.7, 0.56),
    new THREE.MeshBasicMaterial({ map: led.texture, toneMapped: false })
  )
  ledMesh.position.set(0, 2.54, 0.677)
  group.add(ledMesh)

  const ledGlass = new THREE.Mesh(
    new THREE.PlaneGeometry(1.8, 0.64),
    new THREE.MeshPhysicalMaterial({
      color: 0x08131e,
      metalness: 0.1,
      roughness: 0.04,
      transparent: true,
      opacity: 0.22,
      side: THREE.DoubleSide
    })
  )
  ledGlass.position.set(0, 2.54, 0.69)
  group.add(ledGlass)

  // 屏幕下方的品红细线
  const ledLine = new THREE.Mesh(new THREE.BoxGeometry(1.55, 0.012, 0.012), magentaMat)
  ledLine.position.set(0, 2.12, 0.64)
  group.add(ledLine)

  // ---------- 按钮 ----------
  const buttons = {}
  const interactive = []

  function createButton(action, x, colorHex, initialLabel) {
    const g = new THREE.Group()
    g.position.set(x, 1.48, 0.64)
    g.userData.action = action
    g.userData.enabled = true
    g.userData.baseZ = 0.64
    g.userData.press = 1

    const ring = new THREE.Mesh(new THREE.TorusGeometry(0.36, 0.024, 16, 48), trimMat)
    ring.position.z = 0.02
    g.add(ring)

    const glowRing = new THREE.Mesh(
      new THREE.TorusGeometry(0.36, 0.05, 16, 48),
      new THREE.MeshBasicMaterial({ color: colorHex, transparent: true, opacity: 0.55, blending: THREE.AdditiveBlending, depthWrite: false })
    )
    glowRing.position.z = 0.03
    g.add(glowRing)

    const baseCyl = new THREE.Mesh(new THREE.CylinderGeometry(0.31, 0.34, 0.12, 36), panelMat)
    baseCyl.rotation.x = Math.PI / 2
    baseCyl.position.z = 0.08
    g.add(baseCyl)

    const capMat = new THREE.MeshStandardMaterial({
      color: 0x081018,
      metalness: 0.6,
      roughness: 0.24,
      emissive: colorHex,
      emissiveIntensity: 0.38
    })
    const cap = new THREE.Mesh(new THREE.CylinderGeometry(0.27, 0.31, 0.06, 36), capMat)
    cap.rotation.x = Math.PI / 2
    cap.position.z = 0.16
    g.add(cap)

    const labelCanvas = makeTextTexture({
      text: initialLabel,
      color: '#eaffff',
      width: 512,
      height: 160,
      font: `800 120px ${FONT_CN}`,
      glow: 0
    })
    const labelMesh = new THREE.Mesh(
      new THREE.PlaneGeometry(0.5, 0.175),
      new THREE.MeshBasicMaterial({ map: labelCanvas.texture, transparent: true, toneMapped: false, depthWrite: false })
    )
    labelMesh.position.z = 0.205
    g.add(labelMesh)

    g.userData.capMat = capMat
    g.userData.glowRing = glowRing
    g.userData.labelMesh = labelMesh
    g.userData.labelTex = labelCanvas.texture
    g.userData.color = colorHex

    group.add(g)
    interactive.push(g)
    buttons[action] = g
    return g
  }

  createButton('left', -0.62, 0x39ff88, '投币')
  createButton('right', 0.62, 0xff2a3c, '搏一把')

  function setButtonLabel(action, text) {
    const g = buttons[action]
    if (!g || g.userData.label === text) return
    g.userData.label = text
    const canvas = makeTextTexture({
      text,
      color: g.userData.enabled ? '#eaffff' : '#5b7484',
      width: 512,
      height: 160,
      font: `800 120px ${FONT_CN}`,
      glow: 0
    })
    const old = g.userData.labelTex
    g.userData.labelMesh.material.map = canvas.texture
    g.userData.labelMesh.material.needsUpdate = true
    g.userData.labelTex = canvas.texture
    if (old) old.dispose()
  }

  function setButtonEnabled(action, enabled) {
    const g = buttons[action]
    if (!g) return
    if (g.userData.enabled === enabled) return
    g.userData.enabled = enabled
    g.userData.capMat.emissiveIntensity = enabled ? 0.38 : 0.06
    g.userData.glowRing.material.opacity = enabled ? 0.55 : 0.08
    const label = g.userData.label || ''
    const canvas = makeTextTexture({
      text: label,
      color: enabled ? '#eaffff' : '#4b6474',
      width: 512,
      height: 160,
      font: `800 120px ${FONT_CN}`,
      glow: 0
    })
    const old = g.userData.labelTex
    g.userData.labelMesh.material.map = canvas.texture
    g.userData.labelMesh.material.needsUpdate = true
    g.userData.labelTex = canvas.texture
    if (old) old.dispose()
  }

  function pressButton(action) {
    const g = buttons[action]
    if (!g) return
    g.userData.press = 0
  }

  // ---------- 投币口 ----------
  const slotBezel = new THREE.Mesh(new RoundedBoxGeometry(0.66, 0.11, 0.07, 3, 0.02), atmMat)
  slotBezel.position.set(0, 1.02, 0.63)
  group.add(slotBezel)
  const slotMouth = new THREE.Mesh(
    new THREE.PlaneGeometry(0.5, 0.028),
    new THREE.MeshBasicMaterial({ color: 0x01030a })
  )
  slotMouth.position.set(0, 1.02, 0.668)
  group.add(slotMouth)

  // ---------- 出币托盘 ----------
  const tray = new THREE.Group()
  tray.position.set(0, 0.55, 0.86)
  group.add(tray)

  // 黑色底槽：让金币轮廓更明显；底部由两扇向下打开的暗门组成。
  const blackMat = new THREE.MeshStandardMaterial({ color: 0x000000, metalness: 0.05, roughness: 0.9 })

  const hole = new THREE.Mesh(
    new THREE.BoxGeometry(1.2, 0.3, 0.5),
    new THREE.MeshBasicMaterial({ color: 0x000000 })
  )
  hole.position.set(0, -0.27, 0)
  tray.add(hole)

  const leftPivot = new THREE.Group()
  leftPivot.position.set(-0.68, -0.055, 0)
  tray.add(leftPivot)
  const leftPanel = new THREE.Mesh(new RoundedBoxGeometry(0.67, 0.04, 0.62, 2, 0.01), blackMat)
  leftPanel.position.set(0.335, -0.02, 0)
  leftPanel.receiveShadow = true
  leftPivot.add(leftPanel)

  const rightPivot = new THREE.Group()
  rightPivot.position.set(0.68, -0.055, 0)
  tray.add(rightPivot)
  const rightPanel = new THREE.Mesh(new RoundedBoxGeometry(0.67, 0.04, 0.62, 2, 0.01), blackMat)
  rightPanel.position.set(-0.335, -0.02, 0)
  rightPanel.receiveShadow = true
  rightPivot.add(rightPanel)

  const trap = { progress: 0, phase: 'closed', timer: 0 }

  function openTrapdoor() {
    trap.phase = 'opening'
    trap.progress = 0
    trap.timer = 0
  }

  function isTrapdoorBusy() {
    return trap.phase !== 'closed'
  }

  function updateTrapdoor(dt) {
    if (trap.phase === 'opening') {
      trap.progress = Math.min(1, trap.progress + dt / 0.18)
      if (trap.progress >= 1) {
        trap.phase = 'holding'
        trap.timer = 0.55
      }
    } else if (trap.phase === 'holding') {
      trap.timer -= dt
      if (trap.timer <= 0) trap.phase = 'closing'
    } else if (trap.phase === 'closing') {
      trap.progress = Math.max(0, trap.progress - dt / 0.28)
      if (trap.progress <= 0) {
        trap.progress = 0
        trap.phase = 'closed'
      }
    }
    const angle = (Math.PI * 0.4) * trap.progress
    leftPivot.rotation.z = -angle
    rightPivot.rotation.z = angle
  }

  for (const [x, z, w, d] of [
    [-0.68, 0, 0.04, 0.62],
    [0.68, 0, 0.04, 0.62],
    [0, -0.3, 1.36, 0.04]
  ]) {
    const wall = new THREE.Mesh(new THREE.BoxGeometry(w, 0.28, d), panelMat)
    wall.position.set(x, 0.04, z)
    tray.add(wall)
  }

  const trayGlass = new THREE.Mesh(
    new THREE.PlaneGeometry(1.32, 0.3),
    new THREE.MeshPhysicalMaterial({
      color: 0x0a1626,
      metalness: 0.1,
      roughness: 0.05,
      transparent: true,
      opacity: 0.2,
      side: THREE.DoubleSide
    })
  )
  trayGlass.position.set(0, 0.04, 0.31)
  tray.add(trayGlass)

  const trayLight = new THREE.PointLight(0xbfeaff, 0.8, 3.2, 2)
  trayLight.position.set(0, 0.66, 1.0)
  group.add(trayLight)

  // 出币口：简单的 ATM 式横向出款槽，位置抬高，和奖池拉开距离
  const chute = new THREE.Group()
  chute.position.set(0, 1.15, 0.62)

  const chutePanel = new THREE.Mesh(new RoundedBoxGeometry(0.82, 0.13, 0.04, 3, 0.02), atmMat)
  chutePanel.castShadow = true
  chute.add(chutePanel)

  const chuteMouth = new THREE.Mesh(
    new THREE.PlaneGeometry(0.68, 0.032),
    new THREE.MeshBasicMaterial({ color: 0x01030a })
  )
  chuteMouth.position.z = 0.025
  chute.add(chuteMouth)

  group.add(chute)

  // ---------- 两侧灯管 ----------
  const tubes = {}
  for (const [name, x, colorHex] of [['green', -1.28, 0x39ff88], ['red', 1.28, 0xff2a3c]]) {
    const mat = new THREE.MeshStandardMaterial({
      color: 0x05100a,
      metalness: 0.5,
      roughness: 0.25,
      emissive: colorHex,
      emissiveIntensity: 0.35
    })
    const tube = new THREE.Mesh(new THREE.CylinderGeometry(0.04, 0.04, 1.8, 16), mat)
    tube.position.set(x, 1.95, 0.42)
    group.add(tube)

    const glow = new THREE.Mesh(
      new THREE.PlaneGeometry(0.18, 1.9),
      new THREE.MeshBasicMaterial({ color: colorHex, transparent: true, opacity: 0.16, blending: THREE.AdditiveBlending, depthWrite: false })
    )
    glow.position.set(x, 1.95, 0.44)
    group.add(glow)

    const light = new THREE.PointLight(colorHex, 0.4, 4.5, 2)
    light.position.set(x, 1.95, 0.58)
    group.add(light)

    tubes[name] = { mat, glow, light, color: colorHex }
  }

  // ---------- 小标签 ----------
  function addLabel(text, opts) {
    const { x = 0, y = 0, z = 0.64, w = 0.7, h = 0.12, color = '#ffcc66', size = 48, glow = 12 } = opts || {}
    const t = makeTextTexture({ text, color, width: 512, height: 128, font: `700 ${size}px ${FONT_EN}`, glow })
    const m = new THREE.Mesh(
      new THREE.PlaneGeometry(w, h),
      new THREE.MeshBasicMaterial({ map: t.texture, transparent: true, toneMapped: false, depthWrite: false })
    )
    m.position.set(x, y, z)
    group.add(m)
    return m
  }

  addLabel('RISK', { x: -0.86, y: 0.7, w: 0.32, h: 0.08, color: '#ff2bd6', size: 40 })
  addLabel('NO REFUND', { x: 0.86, y: 0.7, w: 0.42, h: 0.08, color: '#ff2a3c', size: 36 })
  addLabel('VIRTUAL COINS · NO CASH OUT', { x: 0, y: 0.2, z: 0.82, w: 1.1, h: 0.08, color: '#ffcc66', size: 30, glow: 8 })

  // 螺丝 / 散热格栅
  for (const [x, y] of [[-1.08, 2.92], [1.08, 2.92], [-1.08, 0.42], [1.08, 0.42]]) {
    const screw = new THREE.Mesh(new THREE.CylinderGeometry(0.028, 0.028, 0.02, 12), trimMat)
    screw.rotation.x = Math.PI / 2
    screw.position.set(x, y, 0.63)
    group.add(screw)
  }
  for (let i = 0; i < 5; i++) {
    const vent = new THREE.Mesh(new THREE.BoxGeometry(0.42, 0.012, 0.02), panelMat)
    vent.position.set(-0.86, 2.72 - i * 0.035, 0.63)
    group.add(vent)
  }

  // 背部线缆
  const curve = new THREE.CatmullRomCurve3([
    new THREE.Vector3(-0.7, 1.0, -0.6),
    new THREE.Vector3(-1.2, 0.5, -1.0),
    new THREE.Vector3(-1.6, 0.08, -1.1)
  ])
  const cable = new THREE.Mesh(new THREE.TubeGeometry(curve, 24, 0.035, 8, false), new THREE.MeshStandardMaterial({ color: 0x05070a, roughness: 0.8 }))
  group.add(cable)

  // ---------- 状态 ----------
  const state = {
    lightMode: 'idle',
    blinkPhase: 0,
    blinkTimer: 0,
    bankrupt: false,
    shake: 0,
    marqueeFlicker: 0
  }

  function setLightMode(mode) {
    if (state.lightMode === mode) return
    state.lightMode = mode
    state.blinkPhase = 0
    state.blinkTimer = 0
    applyLights()
  }

  function applyLights() {
    const g = tubes.green
    const r = tubes.red
    switch (state.lightMode) {
      case 'off':
        g.mat.emissiveIntensity = 0.04; g.light.intensity = 0; g.glow.material.opacity = 0.03
        r.mat.emissiveIntensity = 0.04; r.light.intensity = 0; r.glow.material.opacity = 0.03
        break
      case 'win':
        g.mat.emissiveIntensity = 2.6; g.light.intensity = 4.5; g.glow.material.opacity = 0.5
        r.mat.emissiveIntensity = 0.06; r.light.intensity = 0; r.glow.material.opacity = 0.04
        break
      case 'lose':
        r.mat.emissiveIntensity = 2.6; r.light.intensity = 4.5; r.glow.material.opacity = 0.5
        g.mat.emissiveIntensity = 0.06; g.light.intensity = 0; g.glow.material.opacity = 0.04
        break
      case 'blink':
        g.mat.emissiveIntensity = 2.2; g.light.intensity = 3.5; g.glow.material.opacity = 0.42
        r.mat.emissiveIntensity = 0.08; r.light.intensity = 0.1; r.glow.material.opacity = 0.05
        break
      default:
        g.mat.emissiveIntensity = 0.35; g.light.intensity = 0.35; g.glow.material.opacity = 0.14
        r.mat.emissiveIntensity = 0.35; r.light.intensity = 0.35; r.glow.material.opacity = 0.14
        break
    }
  }

  function update(dt, now) {
    led.update(now)
    updateTrapdoor(dt)

    // 按钮按压
    for (const action of ['left', 'right']) {
      const g = buttons[action]
      if (!g) continue
      if (g.userData.press < 1) {
        g.userData.press = Math.min(1, g.userData.press + dt * 5.5)
        const t = g.userData.press
        g.position.z = g.userData.baseZ - 0.055 * Math.sin(t * Math.PI)
      } else {
        g.position.z = g.userData.baseZ
      }
    }

    // 红绿灯交替
    if (state.lightMode === 'blink') {
      state.blinkTimer += dt
      if (state.blinkTimer > 0.22) {
        state.blinkTimer = 0
        state.blinkPhase = 1 - state.blinkPhase
        const onGreen = state.blinkPhase === 0
        tubes.green.mat.emissiveIntensity = onGreen ? 2.2 : 0.08
        tubes.green.light.intensity = onGreen ? 3.5 : 0.1
        tubes.green.glow.material.opacity = onGreen ? 0.42 : 0.05
        tubes.red.mat.emissiveIntensity = onGreen ? 0.08 : 2.2
        tubes.red.light.intensity = onGreen ? 0.1 : 3.5
        tubes.red.glow.material.opacity = onGreen ? 0.05 : 0.42
      }
    } else if (state.lightMode === 'idle') {
      const breathe = 0.32 + 0.08 * Math.sin(now * 0.0016)
      tubes.green.mat.emissiveIntensity = breathe
      tubes.red.mat.emissiveIntensity = breathe
    }

    // 招牌轻微闪烁
    state.marqueeFlicker += dt
    if (state.marqueeFlicker > 0.12) {
      state.marqueeFlicker = 0
      signMesh.material.opacity = 0.9 + Math.random() * 0.1
    }

    // 震动
    if (state.shake > 0.0001) {
      state.shake *= Math.pow(0.001, dt)
      group.position.x = (Math.random() - 0.5) * state.shake
      group.position.y = (Math.random() - 0.5) * state.shake * 0.6
    } else {
      group.position.x = 0
      group.position.y = 0
    }
  }

  function setBankrupt(bankrupt) {
    if (state.bankrupt === bankrupt) return
    state.bankrupt = bankrupt
    if (bankrupt) {
      setLightMode('off')
      led.setText('BANKRUPT')
      setButtonEnabled('left', false)
      setButtonEnabled('right', false)
      bodyMat.color.setHex(0x05070b)
      front.material.color.setHex(0x030407)
    }
  }

  function shake(intensity) {
    state.shake = Math.max(state.shake, intensity)
  }

  applyLights()

  return {
    group,
    interactive,
    buttons,
    led,
    trayCenter: new THREE.Vector3(0, 0.53, 0.86),
    traySize: { w: 1.28, d: 0.56 },
    chutePosition: new THREE.Vector3(0, 1.15, 0.7),
    slotPosition: new THREE.Vector3(0, 1.02, 0.7),
    walletSidePosition: new THREE.Vector3(-2.6, 1.9, 1.4),
    setButtonLabel,
    setButtonEnabled,
    pressButton,
    setLightMode,
    setBankrupt,
    openTrapdoor,
    isTrapdoorBusy,
    shake,
    update
  }
}
