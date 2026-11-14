// 所有金额在协议里都是「分」，1 金币 = 100 分。
const nf = new Intl.NumberFormat('zh-CN', {
  minimumFractionDigits: 2,
  maximumFractionDigits: 2
})

export function fmtCents(cents) {
  const n = Number(cents || 0) / 100
  return nf.format(n)
}

export function fmtCentsSigned(cents) {
  const n = Number(cents || 0) / 100
  const s = nf.format(Math.abs(n))
  return n < 0 ? '-' + s : s
}

export function fmtCoins(cents) {
  return Math.round(Number(cents || 0) / 100).toLocaleString('zh-CN')
}

export function parseCoinsToCents(value) {
  const n = Number(String(value).trim())
  if (!Number.isFinite(n) || n < 0) return 0
  return Math.round(n * 100)
}

export function parseIntegerCoinsToCents(value) {
  const n = Number(String(value).trim())
  if (!Number.isFinite(n) || n < 0) return 0
  return Math.round(n) * 100
}

export function clamp(n, min, max) {
  return Math.max(min, Math.min(max, n))
}

export function sleep(ms) {
  return new Promise(r => setTimeout(r, ms))
}

export function uuid() {
  if (crypto && crypto.randomUUID) return crypto.randomUUID()
  return 'r-' + Date.now().toString(36) + '-' + Math.random().toString(36).slice(2, 10)
}
