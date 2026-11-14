import * as THREE from 'three'
import { EffectComposer } from 'three/addons/postprocessing/EffectComposer.js'
import { RenderPass } from 'three/addons/postprocessing/RenderPass.js'
import { UnrealBloomPass } from 'three/addons/postprocessing/UnrealBloomPass.js'
import { OutputPass } from 'three/addons/postprocessing/OutputPass.js'
import { createMachine, makeTextTexture } from './machine.js'
import { createCoins } from './coins.js'

const easeOutCubic = p => 1 - Math.pow(1 - p, 3)
const easeInOutQuad = p => (p < 0.5 ? 2 * p * p : 1 - Math.pow(-2 * p + 2, 2) / 2)

function coinCount(poolCents) {
  const coins = Math.floor(poolCents / 100)
  if (coins <= 0) return 0
  return Math.min(1000, coins)
}

function makeGridTexture() {
  const size = 512
  const c = document.createElement('canvas')
  c.width = c.height = size
  const ctx = c.getContext('2d')
  ctx.fillStyle = '#04060b'
  ctx.fillRect(0, 0, size, size)
  ctx.strokeStyle = 'rgba(0, 240, 255, 0.16)'
  ctx.lineWidth = 1
  for (let i = 0; i <= size; i += 64) {
    ctx.beginPath(); ctx.moveTo(i, 0); ctx.lineTo(i, size); ctx.stroke()
    ctx.beginPath(); ctx.moveTo(0, i); ctx.lineTo(size, i); ctx.stroke()
  }
  ctx.strokeStyle = 'rgba(0, 240, 255, 0.42)'
  ctx.lineWidth = 2
  for (let i = 0; i <= size; i += 256) {
    ctx.beginPath(); ctx.moveTo(i, 0); ctx.lineTo(i, size); ctx.stroke()
    ctx.beginPath(); ctx.moveTo(0, i); ctx.lineTo(size, i); ctx.stroke()
  }
  // 中心微光
  const g = ctx.createRadialGradient(size / 2, size / 2, 10, size / 2, size / 2, size / 2)
  g.addColorStop(0, 'rgba(0,240,255,0.10)')
  g.addColorStop(1, 'rgba(0,240,255,0)')
  ctx.fillStyle = g
  ctx.fillRect(0, 0, size, size)
  const tex = new THREE.CanvasTexture(c)
  tex.wrapS = tex.wrapT = THREE.RepeatWrapping
  tex.repeat.set(12, 12)
  tex.colorSpace = THREE.SRGBColorSpace
  tex.anisotropy = 4
  return tex
}

export class SceneView {
  constructor(canvas, callbacks = {}) {
    this.canvas = canvas
    this.callbacks = callbacks
    this.inputEnabled = true
    this.initialized = false
    this.animatingCoinsUntil = 0
    this.tweens = []
    this.quality = 'high'
    this.fps = 0
    this.frameCount = 0
    this.fpsTime = 0
    this.qualityChecked = false
    this.pointerTarget = new THREE.Vector2(0, 0)
    this.pointerOffset = new THREE.Vector2(0, 0)
    this.cameraOffset = new THREE.Vector3()
    this.cameraShake = 0
    this.cameraBase = new THREE.Vector3()
    this.cameraTarget = new THREE.Vector3()
    this.currentState = null
    this.trapdoorLocked = false
    this.betFlyLock = false

    const renderer = new THREE.WebGLRenderer({
      canvas,
      antialias: true,
      powerPreference: 'high-performance',
      alpha: false
    })
    renderer.setSize(canvas.clientWidth || window.innerWidth, canvas.clientHeight || window.innerHeight, false)
    renderer.setPixelRatio(Math.min(window.devicePixelRatio || 1, 2))
    renderer.outputColorSpace = THREE.SRGBColorSpace
    renderer.toneMapping = THREE.ACESFilmicToneMapping
    renderer.toneMappingExposure = 1.18
    renderer.shadowMap.enabled = true
    renderer.shadowMap.type = THREE.PCFSoftShadowMap
    this.renderer = renderer

    const scene = new THREE.Scene()
    scene.background = new THREE.Color(0x05060a)
    scene.fog = new THREE.FogExp2(0x05060a, 0.042)
    this.scene = scene

    const camera = new THREE.PerspectiveCamera(38, 1, 0.1, 100)
    this.camera = camera

    this.setupEnvironment()
    this.setupLights()
    this.setupFloor()
    this.setupBackdrop()
    this.setupDust()

    this.machine = createMachine(scene)
    this.coins = createCoins(scene, this.machine, {
      onClink: intensity => this.callbacks.onCoinClink?.(intensity)
    })

    const composer = new EffectComposer(renderer)
    composer.addPass(new RenderPass(scene, camera))
    this.bloomPass = new UnrealBloomPass(new THREE.Vector2(512, 512), 0.85, 0.6, 0.55)
    composer.addPass(this.bloomPass)
    composer.addPass(new OutputPass())
    this.composer = composer

    this.raycaster = new THREE.Raycaster()
    this.pointer = new THREE.Vector2()

    this.clock = new THREE.Clock()
    this.resize()
    const isMobile = (window.matchMedia && window.matchMedia('(pointer: coarse)').matches) || window.innerWidth < 700
    this.setQuality(isMobile ? 'medium' : 'high')
    this.bindEvents()

    this.loop = this.loop.bind(this)
    requestAnimationFrame(this.loop)
  }

  setupEnvironment() {
    const pmrem = new THREE.PMREMGenerator(this.renderer)
    const envScene = new THREE.Scene()
    envScene.background = new THREE.Color(0x05060a)

    const addPanel = (color, pos, rot, size) => {
      const mesh = new THREE.Mesh(
        new THREE.PlaneGeometry(size[0], size[1]),
        new THREE.MeshBasicMaterial({ color, side: THREE.DoubleSide })
      )
      mesh.position.set(pos[0], pos[1], pos[2])
      mesh.rotation.set(rot[0], rot[1], rot[2])
      envScene.add(mesh)
    }
    addPanel(0x00f0ff, [-6, 3, 0], [0, Math.PI / 2, 0], [10, 6])
    addPanel(0xff2bd6, [6, 3, 0], [0, -Math.PI / 2, 0], [10, 6])
    addPanel(0xbfeaff, [0, 8, 0], [Math.PI / 2, 0, 0], [12, 12])
    addPanel(0x1a2a4a, [0, 2, -7], [0, 0, 0], [14, 8])
    addPanel(0x080a12, [0, 0, 0], [-Math.PI / 2, 0, 0], [16, 16])

    const rt = pmrem.fromScene(envScene, 0.04)
    this.scene.environment = rt.texture
    this.scene.environmentIntensity = 0.9
    pmrem.dispose()
  }

  setupLights() {
    const hemi = new THREE.HemisphereLight(0x224466, 0x05060a, 0.55)
    this.scene.add(hemi)

    const spot = new THREE.SpotLight(0xbfeaff, 260, 26, 0.52, 0.72, 1.6)
    spot.position.set(2.2, 9, 5)
    spot.target.position.set(0, 1.6, 0)
    spot.castShadow = true
    spot.shadow.mapSize.set(1024, 1024)
    spot.shadow.bias = -0.0004
    spot.shadow.camera.near = 1
    spot.shadow.camera.far = 26
    this.scene.add(spot)
    this.scene.add(spot.target)
    this.keyLight = spot


    const magenta = new THREE.PointLight(0xff2bd6, 40, 14, 2)
    magenta.position.set(-4.5, 2.8, 2.2)
    this.scene.add(magenta)

    const cyan = new THREE.PointLight(0x00f0ff, 34, 14, 2)
    cyan.position.set(4.5, 2.2, 1.8)
    this.scene.add(cyan)

    // 顶部光锥
    const cone = new THREE.Mesh(
      new THREE.ConeGeometry(3.0, 9, 32, 1, true),
      new THREE.MeshBasicMaterial({
        color: 0x2a8fb0,
        transparent: true,
        opacity: 0.028,
        side: THREE.DoubleSide,
        blending: THREE.AdditiveBlending,
        depthWrite: false
      })
    )
    cone.position.set(0.4, 4.6, 1.2)
    this.scene.add(cone)
  }

  setupFloor() {
    const grid = makeGridTexture()
    const floorMat = new THREE.MeshStandardMaterial({
      map: grid,
      emissiveMap: grid,
      emissive: 0x00b8d4,
      emissiveIntensity: 0.32,
      color: 0x05070c,
      metalness: 0.65,
      roughness: 0.35
    })
    const floor = new THREE.Mesh(new THREE.PlaneGeometry(60, 60), floorMat)
    floor.rotation.x = -Math.PI / 2
    floor.position.y = 0
    floor.receiveShadow = true
    this.scene.add(floor)

    // 圆台
    const platform = new THREE.Mesh(
      new THREE.CylinderGeometry(3.4, 3.6, 0.14, 64),
      new THREE.MeshStandardMaterial({ color: 0x070a12, metalness: 0.85, roughness: 0.35 })
    )
    platform.position.y = -0.07
    platform.receiveShadow = true
    this.scene.add(platform)

    const ring = new THREE.Mesh(
      new THREE.TorusGeometry(3.25, 0.016, 8, 96),
      new THREE.MeshBasicMaterial({ color: 0x00f0ff, toneMapped: false })
    )
    ring.rotation.x = Math.PI / 2
    ring.position.y = 0.005
    this.scene.add(ring)
  }

  setupBackdrop() {
    const wall = new THREE.Mesh(
      new THREE.PlaneGeometry(44, 20),
      new THREE.MeshStandardMaterial({ color: 0x05070c, metalness: 0.3, roughness: 0.9 })
    )
    wall.position.set(0, 6, -10)
    this.scene.add(wall)

    const signs = [
      { text: 'ALL IN', color: '#ff2bd6', x: -5.2, y: 4.4, z: -7.5, w: 2.6, h: 0.7, size: 90 },
      { text: 'DEBT', color: '#00f0ff', x: 5.0, y: 5.2, z: -8.2, w: 2.2, h: 0.65, size: 90 },
      { text: '1%', color: '#ff2a3c', x: 3.2, y: 2.4, z: -6.8, w: 1.1, h: 0.55, size: 100 },
      { text: 'NO WAY OUT', color: '#ffcc66', x: -4.6, y: 1.8, z: -6.2, w: 2.6, h: 0.5, size: 64 },
      { text: '梭哈', color: '#7c3aed', x: 0.2, y: 5.8, z: -8.8, w: 1.8, h: 0.7, size: 100 },
      { text: 'HOUSE ALWAYS WINS', color: '#ff2a3c', x: -1.8, y: 3.2, z: -9.4, w: 3.0, h: 0.55, size: 56 },
      { text: 'BORROW 1%/DAY', color: '#ffcc66', x: 6.4, y: 3.2, z: -7.8, w: 2.4, h: 0.5, size: 54 },
      { text: 'PAY UP', color: '#00f0ff', x: -7.0, y: 3.2, z: -8.4, w: 1.8, h: 0.55, size: 72 },
      { text: 'NO ESCAPE', color: '#ff2bd6', x: 2.2, y: 1.2, z: -7.4, w: 2.2, h: 0.5, size: 62 },
      { text: '理性游戏', color: '#39ff88', x: 0.4, y: 3.9, z: -9.6, w: 1.6, h: 0.6, size: 70 },
      { text: '借来的钱', color: '#ffaa33', x: -2.2, y: 5.0, z: -8.8, w: 1.6, h: 0.5, size: 64 }
    ]
    this.neonSigns = []
    for (const s of signs) {
      const t = makeTextTexture({
        text: s.text,
        color: s.color,
        width: 1024,
        height: 256,
        font: `800 ${s.size}px "Arial Black", "PingFang SC", sans-serif`,
        glow: 30
      })
      const mesh = new THREE.Mesh(
        new THREE.PlaneGeometry(s.w, s.h),
        new THREE.MeshBasicMaterial({
          map: t.texture,
          transparent: true,
          blending: THREE.AdditiveBlending,
          depthWrite: false,
          opacity: 0.72
        })
      )
      mesh.position.set(s.x, s.y, s.z)
      this.scene.add(mesh)
      this.neonSigns.push({ mesh, base: 0.6 + Math.random() * 0.2, phase: Math.random() * 10 })
    }

    // 背景竖灯
    for (const [x, color] of [[-7, 0x00f0ff], [-3.6, 0xff2bd6], [3.6, 0x00f0ff], [7, 0xff2bd6]]) {
      const bar = new THREE.Mesh(
        new THREE.BoxGeometry(0.035, 9, 0.035),
        new THREE.MeshBasicMaterial({ color, toneMapped: false })
      )
      bar.position.set(x, 4.5, -9.2)
      this.scene.add(bar)
    }
  }

  setupDust() {
    const count = 320
    const positions = new Float32Array(count * 3)
    this.dustData = []
    for (let i = 0; i < count; i++) {
      const x = (Math.random() - 0.5) * 26
      const y = Math.random() * 9
      const z = (Math.random() - 0.5) * 20 - 1
      positions[i * 3] = x
      positions[i * 3 + 1] = y
      positions[i * 3 + 2] = z
      this.dustData.push({ speed: 0.05 + Math.random() * 0.12, phase: Math.random() * 10 })
    }
    const geo = new THREE.BufferGeometry()
    geo.setAttribute('position', new THREE.BufferAttribute(positions, 3))
    const mat = new THREE.PointsMaterial({
      color: 0x66e0ff,
      size: 0.035,
      transparent: true,
      opacity: 0.38,
      blending: THREE.AdditiveBlending,
      depthWrite: false
    })
    this.dust = new THREE.Points(geo, mat)
    this.scene.add(this.dust)
  }

  bindEvents() {
    this.canvas.addEventListener('pointerdown', e => this.onPointerDown(e))
    this.canvas.addEventListener('pointermove', e => this.onPointerMove(e))
    this.canvas.addEventListener('pointerleave', () => { this.canvas.style.cursor = 'default' })
    window.addEventListener('resize', () => this.resize())
    if (window.ResizeObserver) {
      this.resizeObserver = new ResizeObserver(() => this.resize())
      this.resizeObserver.observe(this.canvas)
    }
    document.addEventListener('visibilitychange', () => {
      if (!document.hidden) this.clock.getDelta()
    })
  }

  pick(e) {
    const rect = this.canvas.getBoundingClientRect()
    this.pointer.x = ((e.clientX - rect.left) / rect.width) * 2 - 1
    this.pointer.y = -((e.clientY - rect.top) / rect.height) * 2 + 1
    this.raycaster.setFromCamera(this.pointer, this.camera)
    const hits = this.raycaster.intersectObjects(this.machine.interactive, true)
    for (const hit of hits) {
      let obj = hit.object
      while (obj) {
        if (obj.userData && obj.userData.action) return obj
        obj = obj.parent
      }
    }
    return null
  }

  onPointerDown(e) {
    if (!this.inputEnabled) return
    const g = this.pick(e)
    if (!g) return
    if (!g.userData.enabled) return
    this.machine.pressButton(g.userData.action)
    if (this.callbacks.onButton) this.callbacks.onButton(g.userData.action)
  }

  onPointerMove(e) {
    if (e.pointerType === 'mouse') {
      const g = this.pick(e)
      this.canvas.style.cursor = g && g.userData.enabled ? 'pointer' : 'default'
      const rect = this.canvas.getBoundingClientRect()
      this.pointerTarget.set(
        ((e.clientX - rect.left) / rect.width) * 2 - 1,
        -((e.clientY - rect.top) / rect.height) * 2 + 1
      )
    }
  }

  resize() {
    const w = this.canvas.clientWidth || window.innerWidth
    const h = this.canvas.clientHeight || window.innerHeight
    if (!w || !h) return
    this.renderer.setSize(w, h, false)
    this.camera.aspect = w / h
    this.camera.updateProjectionMatrix()
    this.composer.setSize(w, h)
    const scale = this.quality === 'low' ? 0.4 : this.quality === 'medium' ? 0.5 : 0.6
    this.bloomPass.setSize(Math.max(1, Math.floor(w * scale)), Math.max(1, Math.floor(h * scale)))
    this.fitCamera()
  }

  fitCamera() {
    const box = new THREE.Box3(new THREE.Vector3(-1.75, 0, -1.3), new THREE.Vector3(1.75, 3.7, 1.55))
    const center = box.getCenter(new THREE.Vector3())
    const size = box.getSize(new THREE.Vector3())
    const maxSize = Math.max(size.x, size.y, size.z)
    const fovRad = (this.camera.fov * Math.PI) / 180
    const fitHeight = maxSize / (2 * Math.atan(Math.tan(fovRad / 2)))
    const fitWidth = fitHeight / Math.max(0.2, this.camera.aspect)
    const margin = this.camera.aspect < 1 ? 1.28 : 1.16
    const distance = Math.max(fitHeight, fitWidth) * margin
    const dir = new THREE.Vector3(0.14, 0.16, 1).normalize()
    this.cameraBase.copy(center).addScaledVector(dir, distance)
    this.cameraTarget.copy(center)
    // 镜头略微向下看，让机器和出币槽在画面里更靠上、更大
    this.cameraTarget.y -= 0.26
    this.camera.position.copy(this.cameraBase)
    this.camera.lookAt(this.cameraTarget)
  }

  getButtonScreenPosition(action) {
    const g = this.machine.buttons[action]
    if (!g) return null
    const v = new THREE.Vector3()
    g.getWorldPosition(v)
    v.project(this.camera)
    const rect = this.canvas.getBoundingClientRect()
    return {
      x: rect.left + (v.x * 0.5 + 0.5) * rect.width,
      y: rect.top + (-v.y * 0.5 + 0.5) * rect.height
    }
  }

  setInputEnabled(enabled) {
    this.inputEnabled = enabled
    if (!enabled) this.canvas.style.cursor = 'default'
  }

  setQuality(level) {
    this.quality = level
    const dpr = window.devicePixelRatio || 1
    if (level === 'high') {
      this.renderer.setPixelRatio(Math.min(dpr, 2))
      this.renderer.shadowMap.enabled = true
      this.bloomPass.strength = 0.85
      this.bloomPass.enabled = true
      this.coins.setMaxCoins(60)
    } else if (level === 'medium') {
      this.renderer.setPixelRatio(Math.min(dpr, 1.5))
      this.renderer.shadowMap.enabled = false
      this.bloomPass.strength = 0.6
      this.bloomPass.enabled = true
      this.coins.setMaxCoins(40)
    } else {
      this.renderer.setPixelRatio(1)
      this.renderer.shadowMap.enabled = false
      this.bloomPass.strength = 0.35
      this.bloomPass.enabled = false
      this.coins.setMaxCoins(20)
    }
    this.resize()
  }

  tween(duration, update, complete, ease = easeOutCubic) {
    this.tweens.push({ t: 0, duration, update, complete, ease })
  }

  cameraPunch() {
    this.cameraOffset.set(0, 0.1, 0.35)
    this.tween(0.7, p => {
      this.cameraOffset.set(0, 0.1 * (1 - p), 0.35 * (1 - p))
    })
  }

  setState(state) {
    this.currentState = state
    const bankrupt = state.bankrupt
    const closed = state.isClosed
    const connected = state.connected
    const phase = state.phase
    const pool = state.poolCents

    this.machine.setBankrupt(bankrupt)
    this.machine.setButtonLabel('left', phase === 'bet' ? '收手' : '投币')
    this.machine.setButtonLabel('right', '搏一把')
    const trapBusy = this.machine.isTrapdoorBusy()
    const flying = this.coins.flyingIn
    this.machine.setButtonEnabled('left', !bankrupt && connected && !closed && !trapBusy && !flying && phase !== 'revealing')
    this.machine.setButtonEnabled('right', !bankrupt && connected && !closed && !trapBusy && !flying && phase === 'bet' && pool > 0)

    const poolCoins = Math.floor(pool / 100)
    if (!this.initialized) {
      this.coins.snap(coinCount(pool))
      if (bankrupt) this.machine.led.setText('BANKRUPT')
      else this.machine.led.setValue(poolCoins)
      this.initialized = true
    } else if (performance.now() > this.animatingCoinsUntil) {
      const n = coinCount(pool)
      if (n !== this.coins.target) {
        this.coins.setTarget(n, n > this.coins.target ? 'fall' : 'out')
      }
      if (phase !== 'revealing' && !bankrupt) this.machine.led.setValue(poolCoins)
    }

    if (bankrupt) {
      this.machine.setLightMode('off')
      return
    }

    if (phase === 'revealing') {
      const remain = (state.round?.revealAt || 0) - state.serverNow()
      if (remain > 0) {
        this.machine.led.startRoll(Math.min(remain, 4000))
        this.machine.setLightMode('blink')
      }
    } else if (state.round?.result === 'win') {
      this.machine.setLightMode('win')
    } else if (state.round?.result === 'lose') {
      this.machine.setLightMode('lose')
    } else {
      this.machine.setLightMode('idle')
    }
  }

  spillOverflow(totalCents) {
    const total = Math.floor(totalCents / 100)
    const cap = this.coins.capacity
    if (total > cap) {
      this.coins.spill(Math.min(16, Math.max(6, Math.ceil((total - cap) / 3))))
    }
  }

  onEvent(ev, state) {
    const now = performance.now()
    switch (ev.action) {
      case 'bet': {
        this.machine.pressButton('left')
        this.coins.flyIn(coinCount(ev.poolCents))
        this.spillOverflow(ev.poolCents)
        this.betFlyLock = true
        this.machine.setButtonEnabled('left', false)
        this.machine.setButtonEnabled('right', false)
        this.machine.led.setValue(Math.floor(ev.poolCents / 100))
        this.machine.setLightMode('idle')
        this.machine.shake(0.012)
        this.animatingCoinsUntil = now + 900
        break
      }
      case 'reveal_start': {
        this.machine.pressButton('right')
        const remain = Math.max(200, (ev.revealAt || 0) - (state?.serverNow?.() || Date.now()))
        this.machine.led.startRoll(remain)
        this.machine.setLightMode('blink')
        this.machine.shake(0.02)
        this.animatingCoinsUntil = now + remain + 300
        break
      }
      case 'reveal_result': {
        if (ev.result === 'win') {
          this.machine.setLightMode('win')
          this.machine.led.setValue(Math.floor(ev.poolCents / 100))
          this.coins.setTarget(coinCount(ev.poolCents), 'fall')
          this.spillOverflow(ev.poolCents)
          this.cameraPunch()
          this.machine.shake(0.025)
        } else {
          this.machine.setLightMode('lose')
          this.machine.led.setValue(0)
          this.machine.openTrapdoor()
          this.coins.dropThrough()
          this.coins.clearOverflow()
          this.machine.setButtonEnabled('left', false)
          this.machine.setButtonEnabled('right', false)
          this.trapdoorLocked = true
          this.machine.shake(0.09)
          this.cameraShake = 0.1
        }
        this.animatingCoinsUntil = now + 1200
        break
      }
      case 'cashout': {
        this.machine.pressButton('left')
        this.coins.flyOut()
        this.coins.collectOverflow()
        this.machine.led.setValue(0)
        this.machine.setLightMode('idle')
        this.animatingCoinsUntil = now + 1000
        break
      }
      case 'bankrupt': {
        this.machine.setBankrupt(true)
        this.cameraShake = 0.15
        break
      }
      case 'gift': {
        // 金币进钱包的反馈由 HUD 数字动画负责
        break
      }
    }
  }

  loop() {
    requestAnimationFrame(this.loop)
    const dt = Math.min(0.05, this.clock.getDelta())
    const now = performance.now()

    if (document.hidden) return

    // tweens
    for (let i = this.tweens.length - 1; i >= 0; i--) {
      const tw = this.tweens[i]
      tw.t += dt
      const p = Math.min(1, tw.t / tw.duration)
      tw.update(tw.ease(p))
      if (p >= 1) {
        if (tw.complete) tw.complete()
        this.tweens.splice(i, 1)
      }
    }

    this.machine.update(dt, now)
    this.coins.update(dt)

    // 指针视差
    this.pointerOffset.x += (this.pointerTarget.x * 0.08 - this.pointerOffset.x) * Math.min(1, dt * 3)
    this.pointerOffset.y += (-this.pointerTarget.y * 0.05 - this.pointerOffset.y) * Math.min(1, dt * 3)

    // 呼吸镜头
    const t = now * 0.001
    const swayX = Math.sin(t * 0.31) * 0.035
    const swayY = Math.sin(t * 0.47) * 0.016
    this.camera.position.copy(this.cameraBase)
    this.camera.position.x += swayX + this.pointerOffset.x + this.cameraOffset.x
    this.camera.position.y += swayY + this.pointerOffset.y + this.cameraOffset.y
    this.camera.position.z += this.cameraOffset.z
    if (this.cameraShake > 0.0005) {
      this.cameraShake *= Math.pow(0.0015, dt)
      this.camera.position.x += (Math.random() - 0.5) * this.cameraShake
      this.camera.position.y += (Math.random() - 0.5) * this.cameraShake * 0.7
    } else {
      this.cameraShake = 0
    }
    this.camera.lookAt(this.cameraTarget)

    // 背景霓虹闪烁
    if (this.neonSigns) {
      for (const s of this.neonSigns) {
        s.mesh.material.opacity = s.base + Math.sin(t * 1.7 + s.phase) * 0.08
      }
    }

    // 尘埃
    if (this.dust) {
      const pos = this.dust.geometry.attributes.position
      for (let i = 0; i < this.dustData.length; i++) {
        let y = pos.getY(i) + this.dustData[i].speed * dt
        if (y > 9) y = 0
        pos.setY(i, y)
      }
      pos.needsUpdate = true
      this.dust.rotation.y += dt * 0.008
    }

    if (this.trapdoorLocked && !this.machine.isTrapdoorBusy()) {
      this.trapdoorLocked = false
      if (this.currentState) this.setState(this.currentState)
    } else if (!this.trapdoorLocked && this.machine.isTrapdoorBusy()) {
      this.trapdoorLocked = true
    }

    if (this.betFlyLock && !this.coins.flyingIn) {
      this.betFlyLock = false
      if (this.currentState) this.setState(this.currentState)
    } else if (!this.betFlyLock && this.coins.flyingIn) {
      this.betFlyLock = true
    }

    this.composer.render()

    // FPS
    this.frameCount++
    this.fpsTime += dt
    if (this.fpsTime >= 1) {
      this.fps = this.frameCount / this.fpsTime
      this.frameCount = 0
      this.fpsTime = 0
      if (!this.qualityChecked && now > 3500) {
        this.qualityChecked = true
        if (this.fps < 26) this.setQuality('low')
        else if (this.fps < 42) this.setQuality('medium')
      }
    }
  }
}
