import * as THREE from 'three'
import { RoundedBoxGeometry } from 'three/addons/geometries/RoundedBoxGeometry.js'

// 筹码面额：1 金条 = 100 金币，1 元宝 = 10 金币。
// 奖池最多展示 1000 金币；超出部分用地面溢出金币表现。
const MAX_TOTAL = 1000
const MAX_BARS = 20
const MAX_YB = 4
const MAX_COINS = 9
const MAX_OVERFLOW = 28
const GRAVITY = 7.5

function hash(n) {
  const x = Math.sin(n * 127.1 + 311.7) * 43758.5453
  return x - Math.floor(x)
}

function clamp(n, a, b) {
  return Math.max(a, Math.min(b, n))
}

function decompose(n) {
  n = clamp(Math.floor(n), 0, MAX_TOTAL)
  return {
    bars: Math.floor(n / 50),
    yb: Math.floor((n % 50) / 10),
    coins: n % 10
  }
}

export function createCoins(scene, machine, opts = {}) {
  const onClink = opts.onClink || null

  // 金币比上一版放大，保证几枚也能看清
  const coinGeo = new THREE.CylinderGeometry(0.05, 0.05, 0.009, 20)
  // 元宝：船形侧面挤出
  const ybShape = new THREE.Shape()
  ybShape.moveTo(-0.12, 0)
  ybShape.bezierCurveTo(-0.12, 0.024, -0.08, 0.03, -0.055, 0.048)
  ybShape.bezierCurveTo(-0.028, 0.075, 0.028, 0.075, 0.055, 0.048)
  ybShape.bezierCurveTo(0.08, 0.03, 0.12, 0.024, 0.12, 0)
  ybShape.lineTo(-0.12, 0)
  const ybGeo = new THREE.ExtrudeGeometry(ybShape, {
    depth: 0.08,
    bevelEnabled: true,
    bevelSize: 0.006,
    bevelThickness: 0.006,
    bevelSegments: 2,
    steps: 1
  })
  ybGeo.center()
  const barGeo = new RoundedBoxGeometry(0.3, 0.1, 0.14, 3, 0.025)

  const coinMat = new THREE.MeshStandardMaterial({
    color: 0xffcc66,
    metalness: 0.95,
    roughness: 0.22,
    emissive: 0x3a1f00,
    emissiveIntensity: 0.38,
    envMapIntensity: 1.7
  })
  const ybMat = new THREE.MeshStandardMaterial({
    color: 0xffd27a,
    metalness: 0.95,
    roughness: 0.2,
    emissive: 0x4a2600,
    emissiveIntensity: 0.4,
    envMapIntensity: 1.8
  })
  const barMat = new THREE.MeshStandardMaterial({
    color: 0xffc247,
    metalness: 0.96,
    roughness: 0.18,
    emissive: 0x4a2600,
    emissiveIntensity: 0.45,
    envMapIntensity: 1.9
  })

  const tray = machine.trayCenter.clone()
  const bounds = {
    floorY: tray.y - 0.026
  }

  let lastClink = 0
  function clink(intensity = 0.5) {
    if (!onClink) return
    const now = performance.now()
    if (now - lastClink < 45) return
    lastClink = now
    onClink(intensity)
  }

  // ---------- 面额筹码位置 ----------
  // 金条在下、元宝居中、金币最上层；层高随面额数量动态变化。
  function baseTopY() {
    const bars = typeState.bar.count
    if (bars <= 0) return tray.y - 0.035
    return tray.y + 0.065 + (bars > 16 ? 0.1 : 0)
  }
  function barPos(i) {
    if (i < 16) {
      const col = i % 4
      const row = Math.floor(i / 4)
      return new THREE.Vector3(
        tray.x + (col - 1.5) * 0.32,
        tray.y + 0.015,
        tray.z + (row - 1.5) * 0.15
      )
    }
    const col = i - 16
    return new THREE.Vector3(
      tray.x + (col - 1.5) * 0.32,
      tray.y + 0.115,
      tray.z - 0.225
    )
  }
  function ybPos(i) {
    const col = i % 2
    const row = Math.floor(i / 2)
    return new THREE.Vector3(
      tray.x + (col - 0.5) * 0.24,
      baseTopY() + 0.0395,
      tray.z + (row - 0.5) * 0.2
    )
  }
  function coinPos(i) {
    const col = i % 3
    const row = Math.floor(i / 3)
    const top = baseTopY() + (typeState.yb.count > 0 ? 0.077 : 0)
    return new THREE.Vector3(
      tray.x + (col - 1) * 0.11,
      top + 0.0055,
      tray.z + (row - 1) * 0.09
    )
  }
  function piecePos(type, i) {
    if (type === 'bar') return barPos(i)
    if (type === 'yb') return ybPos(i)
    return coinPos(i)
  }
  function relayout() {
    for (const type of ['bar', 'yb', 'coin']) {
      const state = typeState[type]
      for (let i = 0; i < state.count; i++) {
        const p = state.arr[i]
        if (!p) continue
        const pos = piecePos(type, i)
        p.target.copy(pos)
        if (p.mode === 'pile') p.mesh.position.copy(pos)
      }
    }
  }

  // ---------- 筹码池 ----------
  function makePiece(type, index, geo, mat) {
    const mesh = new THREE.Mesh(geo, mat)
    mesh.visible = false
    mesh.castShadow = false
    mesh.receiveShadow = false
    scene.add(mesh)
    return {
      type,
      index,
      mesh,
      mode: 'hidden',
      target: new THREE.Vector3(),
      start: new THREE.Vector3(),
      velocity: new THREE.Vector3(),
      angular: new THREE.Vector3(),
      t: 0,
      duration: 0.4,
      arc: 0.3
    }
  }

  const barPieces = []
  const ybPieces = []
  const coinPieces = []
  for (let i = 0; i < MAX_BARS; i++) barPieces.push(makePiece('bar', i, barGeo, barMat))
  for (let i = 0; i < MAX_YB; i++) ybPieces.push(makePiece('yb', i, ybGeo, ybMat))
  for (let i = 0; i < MAX_COINS; i++) coinPieces.push(makePiece('coin', i, coinGeo, coinMat))
  const allPieces = [...barPieces, ...ybPieces, ...coinPieces]
  const typeState = {
    bar: { arr: barPieces, count: 0 },
    yb: { arr: ybPieces, count: 0 },
    coin: { arr: coinPieces, count: 0 }
  }

  let targetTotal = 0
  let flyInActive = false

  function resetPieces() {
    for (const p of allPieces) {
      p.mode = 'hidden'
      p.mesh.visible = false
      p.mesh.scale.setScalar(1)
    }
    typeState.bar.count = 0
    typeState.yb.count = 0
    typeState.coin.count = 0
  }

  function spawnPiece(p, action) {
    const pos = piecePos(p.type, p.index)
    p.target.copy(pos)
    p.mesh.visible = true
    p.mesh.scale.setScalar(1)
    if (action === 'snap') {
      p.mesh.position.copy(pos)
      p.mesh.rotation.set(0, hash(p.index * 37 + p.type.length) * Math.PI * 2, 0)
      p.mode = 'pile'
      return
    }
    if (action === 'fly') {
      p.mode = 'fly'
      p.t = 0
      p.duration = 0.34 + Math.random() * 0.22
      p.arc = 0.18 + Math.random() * 0.22
      p.start.set(
        machine.walletSidePosition.x + (Math.random() - 0.5) * 0.7,
        machine.walletSidePosition.y + (Math.random() - 0.5) * 0.6,
        machine.walletSidePosition.z + (Math.random() - 0.5) * 0.6
      )
      p.mesh.position.copy(p.start)
      p.mesh.rotation.set(Math.random() * Math.PI, Math.random() * Math.PI, Math.random() * Math.PI)
      return
    }
    // fall：从出币口掉到目标位置
    p.mode = 'fall'
    p.mesh.position.set(
      machine.chutePosition.x + (Math.random() - 0.5) * 0.25,
      machine.chutePosition.y + 0.02,
      machine.chutePosition.z + (Math.random() - 0.5) * 0.15
    )
    p.velocity.set(
      (Math.random() - 0.5) * 0.45,
      0.08 + Math.random() * 0.2,
      (Math.random() - 0.5) * 0.3
    )
    p.angular.set(
      (Math.random() - 0.5) * 6,
      (Math.random() - 0.5) * 6,
      (Math.random() - 0.5) * 6
    )
    p.mesh.rotation.set(Math.random() * Math.PI, Math.random() * Math.PI, Math.random() * Math.PI)
  }

  function removePiece(p, action) {
    if (action === 'hide' || action === 'snap') {
      p.mode = 'hidden'
      p.mesh.visible = false
      p.mesh.scale.setScalar(1)
      return
    }
    p.start.copy(p.mesh.position)
    p.t = 0
    p.duration = 0.32 + Math.random() * 0.2
    p.arc = 0.16 + Math.random() * 0.2
    if (action === 'drop') {
      p.mode = 'drop'
      p.velocity.set(
        (Math.random() - 0.5) * 0.25,
        -0.04 - Math.random() * 0.16,
        (Math.random() - 0.5) * 0.2
      )
      p.angular.set(
        (Math.random() - 0.5) * 4,
        (Math.random() - 0.5) * 4,
        (Math.random() - 0.5) * 4
      )
    } else {
      p.mode = 'out'
      p.target.set(
        machine.walletSidePosition.x + (Math.random() - 0.5) * 0.7,
        machine.walletSidePosition.y + (Math.random() - 0.5) * 0.6,
        machine.walletSidePosition.z + (Math.random() - 0.5) * 0.6
      )
    }
  }

  function applyCounts(counts, action, spawnAction) {
    for (const type of ['bar', 'yb', 'coin']) {
      const state = typeState[type]
      const arr = state.arr
      const next = counts[type === 'bar' ? 'bars' : type === 'yb' ? 'yb' : 'coins']
      const old = state.count
      if (next > old) {
        state.count = next
        for (let i = old; i < next; i++) {
          const p = arr[i]
          if (!p) continue
          spawnPiece(p, spawnAction || (action === 'fly' ? 'fly' : action === 'snap' ? 'snap' : 'fall'))
        }
      } else if (next < old) {
        for (let i = next; i < old; i++) {
          const p = arr[i]
          if (!p) continue
          removePiece(p, action === 'out' ? 'out' : action === 'drop' ? 'drop' : 'hide')
        }
        state.count = next
      }
    }
    relayout()
  }

  function setTarget(n, mode = 'fall') {
    n = clamp(Math.floor(n), 0, MAX_TOTAL)
    const counts = decompose(n)
    if (n >= targetTotal) {
      // 增加：新筹码从出币口掉下；元宝/金条换算时被替换的旧筹码直接隐藏
      applyCounts(counts, 'fall', 'fall')
    } else {
      // 减少：默认飞向外部；损失时由 dropThrough 走掉落
      applyCounts(counts, mode === 'out' ? 'out' : 'fall', 'fall')
    }
    targetTotal = n
    flyInActive = false
  }

  function flyIn(n) {
    n = clamp(Math.floor(n), 0, MAX_TOTAL)
    resetPieces()
    const counts = decompose(n)
    applyCounts(counts, 'fly', 'fly')
    targetTotal = n
    flyInActive = n > 0
  }

  function snap(n) {
    n = clamp(Math.floor(n), 0, MAX_TOTAL)
    resetPieces()
    applyCounts(decompose(n), 'snap', 'snap')
    targetTotal = n
    flyInActive = false
  }

  function flyOut() {
    applyCounts({ bars: 0, yb: 0, coins: 0 }, 'out', 'fall')
    targetTotal = 0
    flyInActive = false
  }

  function dropThrough() {
    applyCounts({ bars: 0, yb: 0, coins: 0 }, 'drop', 'fall')
    targetTotal = 0
    flyInActive = false
  }

  function suckAll() {
    flyOut()
  }

  function setMaxCoins() {
    // 面额化后筹码数量很少，画质不再限制奖池显示
  }

  // ---------- 溢出金币（超过 1000 的部分 / 抽奖表现） ----------
  const overflow = []
  for (let i = 0; i < MAX_OVERFLOW; i++) {
    const mesh = new THREE.Mesh(coinGeo, coinMat)
    mesh.visible = false
    scene.add(mesh)
    overflow.push({
      mesh,
      active: false,
      collecting: false,
      velocity: new THREE.Vector3(),
      angular: new THREE.Vector3(),
      life: 0,
      collectT: 0,
      collectDuration: 0.4,
      collectArc: 0.25,
      collectStart: new THREE.Vector3(),
      collectTarget: new THREE.Vector3()
    })
  }

  function takeOverflow() {
    let o = overflow.find(x => !x.active)
    if (o) return o
    let oldest = null
    for (const x of overflow) {
      if (x.collecting) continue
      if (!oldest || x.life > oldest.life) oldest = x
    }
    if (!oldest) return null
    oldest.active = false
    oldest.mesh.visible = false
    return oldest
  }

  function spill(count) {
    count = clamp(Math.floor(count), 0, MAX_OVERFLOW)
    for (let n = 0; n < count; n++) {
      const o = takeOverflow()
      if (!o) break
      o.active = true
      o.collecting = false
      o.life = 0
      o.mesh.visible = true
      o.mesh.position.set(
        tray.x + (Math.random() - 0.5) * 0.9,
        tray.y + 0.03 + Math.random() * 0.05,
        tray.z + 0.22 + Math.random() * 0.06
      )
      o.velocity.set(
        (Math.random() - 0.5) * 0.35,
        0.1 + Math.random() * 0.25,
        0.05 + Math.random() * 0.3
      )
      o.angular.set(
        (Math.random() - 0.5) * 10,
        (Math.random() - 0.5) * 10,
        (Math.random() - 0.5) * 10
      )
      o.mesh.scale.setScalar(1)
      o.mesh.rotation.set(Math.random() * Math.PI, Math.random() * Math.PI, Math.random() * Math.PI)
    }
  }

  function payout(count) {
    count = clamp(Math.floor(count), 0, MAX_OVERFLOW)
    for (let n = 0; n < count; n++) {
      const o = takeOverflow()
      if (!o) break
      o.active = true
      o.collecting = false
      o.life = 0
      o.mesh.visible = true
      o.mesh.position.set(
        machine.chutePosition.x + (Math.random() - 0.5) * 0.35,
        machine.chutePosition.y + 0.02 + Math.random() * 0.05,
        machine.chutePosition.z + (Math.random() - 0.5) * 0.2
      )
      o.velocity.set(
        (Math.random() - 0.5) * 0.65,
        -0.02 + Math.random() * 0.2,
        0.05 + Math.random() * 0.55
      )
      o.angular.set(
        (Math.random() - 0.5) * 10,
        (Math.random() - 0.5) * 10,
        (Math.random() - 0.5) * 10
      )
      o.mesh.scale.setScalar(1)
      o.mesh.rotation.set(Math.random() * Math.PI, Math.random() * Math.PI, Math.random() * Math.PI)
    }
  }

  function collectOverflow() {
    for (const o of overflow) {
      if (!o.active || o.collecting) continue
      o.collecting = true
      o.collectT = 0
      o.collectDuration = 0.34 + Math.random() * 0.2
      o.collectArc = 0.18 + Math.random() * 0.22
      o.collectStart.copy(o.mesh.position)
      o.collectTarget.set(
        machine.walletSidePosition.x + (Math.random() - 0.5) * 0.7,
        machine.walletSidePosition.y + (Math.random() - 0.5) * 0.6,
        machine.walletSidePosition.z + (Math.random() - 0.5) * 0.6
      )
    }
  }

  function clearOverflow() {
    for (const o of overflow) {
      o.active = false
      o.collecting = false
      o.mesh.visible = false
      o.mesh.scale.setScalar(1)
    }
  }

  function updatePieces(dt) {
    let flying = false
    for (const p of allPieces) {
      if (p.mode === 'hidden' || p.mode === 'pile') continue
      if (p.mode === 'fall') {
        p.velocity.y -= GRAVITY * dt
        p.mesh.position.addScaledVector(p.velocity, dt)
        p.mesh.rotation.x += p.angular.x * dt
        p.mesh.rotation.y += p.angular.y * dt
        p.mesh.rotation.z += p.angular.z * dt
        if (p.mesh.position.y <= p.target.y && p.velocity.y < 0) {
          p.mesh.position.copy(p.target)
          p.mesh.rotation.set(0, hash(p.index * 37 + p.type.length) * Math.PI * 2, 0)
          p.mode = 'pile'
          clink(0.5)
        }
      } else if (p.mode === 'fly') {
        flying = true
        p.t = Math.min(1, p.t + dt / p.duration)
        p.mesh.position.lerpVectors(p.start, p.target, p.t)
        p.mesh.position.y += Math.sin(p.t * Math.PI) * p.arc
        p.mesh.rotation.x += dt * 8
        p.mesh.rotation.z += dt * 5
        if (p.t >= 1) {
          p.mesh.position.copy(p.target)
          p.mesh.rotation.set(0, hash(p.index * 37 + p.type.length) * Math.PI * 2, 0)
          p.mode = 'pile'
          clink(0.45)
        }
      } else if (p.mode === 'out') {
        p.t = Math.min(1, p.t + dt / p.duration)
        p.mesh.position.lerpVectors(p.start, p.target, p.t)
        p.mesh.position.y += Math.sin(p.t * Math.PI) * p.arc
        p.mesh.rotation.x += dt * 8
        p.mesh.rotation.z += dt * 5
        p.mesh.scale.setScalar(Math.max(0.05, 1 - p.t * 0.9))
        if (p.t >= 1) {
          p.mode = 'hidden'
          p.mesh.visible = false
          p.mesh.scale.setScalar(1)
        }
      } else if (p.mode === 'drop') {
        p.velocity.y -= GRAVITY * dt
        p.mesh.position.addScaledVector(p.velocity, dt)
        p.mesh.rotation.x += p.angular.x * dt
        p.mesh.rotation.y += p.angular.y * dt
        p.mesh.rotation.z += p.angular.z * dt
        if (p.mesh.position.y < tray.y - 0.22) {
          p.mode = 'hidden'
          p.mesh.visible = false
          p.mesh.scale.setScalar(1)
        }
      }
    }
    if (flyInActive && !flying) flyInActive = false
  }

  function updateOverflow(dt) {
    for (const o of overflow) {
      if (!o.active) continue
      if (o.collecting) {
        o.collectT = Math.min(1, o.collectT + dt / o.collectDuration)
        o.mesh.position.lerpVectors(o.collectStart, o.collectTarget, o.collectT)
        o.mesh.position.y += Math.sin(o.collectT * Math.PI) * o.collectArc
        o.mesh.rotation.x += dt * 8
        o.mesh.rotation.z += dt * 5
        o.mesh.scale.setScalar(Math.max(0.05, 1 - o.collectT * 0.9))
        if (o.collectT >= 1) {
          o.active = false
          o.collecting = false
          o.mesh.visible = false
          o.mesh.scale.setScalar(1)
        }
        continue
      }
      o.life += dt
      o.velocity.y -= GRAVITY * dt
      o.mesh.position.addScaledVector(o.velocity, dt)
      o.mesh.rotation.x += o.angular.x * dt
      o.mesh.rotation.y += o.angular.y * dt
      o.mesh.rotation.z += o.angular.z * dt
      if (o.mesh.position.y < 0.006) {
        o.mesh.position.y = 0.006
        if (o.velocity.y < -0.1) {
          o.velocity.y *= -0.32
          o.velocity.x *= 0.78
          o.velocity.z *= 0.78
          clink(0.4)
        } else {
          o.velocity.y = 0
        }
      }
      if (o.mesh.position.y <= 0.009 && Math.abs(o.velocity.y) < 0.3) {
        o.velocity.x *= Math.max(0, 1 - dt * 3)
        o.velocity.z *= Math.max(0, 1 - dt * 3)
      }
      if (o.life > 7) o.mesh.scale.setScalar(Math.max(0.01, 1 - (o.life - 7)))
      if (o.life > 8) {
        o.active = false
        o.mesh.visible = false
        o.mesh.scale.setScalar(1)
      }
    }
  }

  function update(dt) {
    updatePieces(dt)
    updateOverflow(dt)
  }

  return {
    snap,
    setTarget,
    flyIn,
    flyOut,
    dropThrough,
    suckAll,
    spill,
    payout,
    collectOverflow,
    clearOverflow,
    setMaxCoins,
    update,
    get target() { return targetTotal },
    get current() { return targetTotal },
    get flyingIn() { return flyInActive },
    get capacity() { return MAX_TOTAL }
  }
}
