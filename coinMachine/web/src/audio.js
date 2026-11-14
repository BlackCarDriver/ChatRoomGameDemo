// WebAudio 合成音效：没有 BGM，只有操作反馈音。
export class AudioEngine {
  constructor() {
    this.ctx = null
    this.master = null
    this.muted = localStorage.getItem('coin-muted') === '1'
    this.noiseBuf = null
  }

  unlock() {
    if (!this.ctx) {
      const Ctx = window.AudioContext || window.webkitAudioContext
      if (!Ctx) return
      this.ctx = new Ctx()
      this.master = this.ctx.createGain()
      this.master.gain.value = this.muted ? 0 : 0.32
      this.master.connect(this.ctx.destination)
      this.noiseBuf = this.makeNoise()
    }
    if (this.ctx.state === 'suspended') this.ctx.resume().catch(() => {})
  }

  setMuted(muted) {
    this.muted = muted
    localStorage.setItem('coin-muted', muted ? '1' : '0')
    if (this.master) this.master.gain.value = muted ? 0 : 0.32
  }

  toggleMuted() {
    this.setMuted(!this.muted)
    return this.muted
  }

  makeNoise() {
    const len = this.ctx.sampleRate * 1.2
    const buf = this.ctx.createBuffer(1, len, this.ctx.sampleRate)
    const data = buf.getChannelData(0)
    for (let i = 0; i < len; i++) data[i] = Math.random() * 2 - 1
    return buf
  }

  tone({ freq = 440, endFreq = null, type = 'sine', dur = 0.15, gain = 0.5, delay = 0 }) {
    if (!this.ctx) return
    const t0 = this.ctx.currentTime + delay
    const osc = this.ctx.createOscillator()
    const g = this.ctx.createGain()
    osc.type = type
    osc.frequency.setValueAtTime(freq, t0)
    if (endFreq != null) osc.frequency.exponentialRampToValueAtTime(Math.max(20, endFreq), t0 + dur)
    g.gain.setValueAtTime(0.0001, t0)
    g.gain.exponentialRampToValueAtTime(gain, t0 + 0.008)
    g.gain.exponentialRampToValueAtTime(0.0001, t0 + dur)
    osc.connect(g); g.connect(this.master)
    osc.start(t0); osc.stop(t0 + dur + 0.02)
  }

  noise({ dur = 0.3, gain = 0.3, filter = 1800, delay = 0, q = 0.7 }) {
    if (!this.ctx || !this.noiseBuf) return
    const t0 = this.ctx.currentTime + delay
    const src = this.ctx.createBufferSource()
    src.buffer = this.noiseBuf
    const bp = this.ctx.createBiquadFilter()
    bp.type = 'bandpass'
    bp.frequency.value = filter
    bp.Q.value = q
    const g = this.ctx.createGain()
    g.gain.setValueAtTime(0.0001, t0)
    g.gain.exponentialRampToValueAtTime(gain, t0 + 0.01)
    g.gain.exponentialRampToValueAtTime(0.0001, t0 + dur)
    src.connect(bp); bp.connect(g); g.connect(this.master)
    src.start(t0); src.stop(t0 + dur + 0.02)
  }

  play(name, intensity = 1) {
    if (!this.ctx || this.muted) return
    switch (name) {
      case 'click':
        this.tone({ freq: 900, endFreq: 620, type: 'square', dur: 0.05, gain: 0.18 })
        break
      case 'coin':
        this.tone({ freq: 1800, endFreq: 1350, type: 'triangle', dur: 0.055, gain: 0.18 })
        this.noise({ dur: 0.04, gain: 0.06, filter: 4200 })
        break
      case 'clink': {
        const g = 0.04 + 0.06 * Math.max(0, Math.min(1, intensity || 0.5))
        this.tone({
          freq: 2400 + Math.random() * 900,
          endFreq: 1600 + Math.random() * 300,
          type: 'triangle',
          dur: 0.03,
          gain: g
        })
        this.noise({ dur: 0.02, gain: g * 0.35, filter: 5200 })
        break
      }
      case 'tick':
        this.tone({ freq: 1600, type: 'square', dur: 0.035, gain: 0.12 })
        break
      case 'win':
        [523, 659, 784, 1047].forEach((f, i) => this.tone({ freq: f, type: 'triangle', dur: 0.18, gain: 0.24, delay: i * 0.08 }))
        this.noise({ dur: 0.8, gain: 0.16, filter: 3200, delay: 0.18 })
        break
      case 'lose':
        this.tone({ freq: 220, endFreq: 55, type: 'sawtooth', dur: 0.55, gain: 0.3 })
        this.noise({ dur: 0.3, gain: 0.18, filter: 300, delay: 0.05 })
        break
      case 'cashout':
        this.noise({ dur: 0.4, gain: 0.2, filter: 2200 })
        this.tone({ freq: 1200, type: 'sine', dur: 0.12, gain: 0.18, delay: 0.05 })
        break
      case 'loan':
        this.tone({ freq: 440, endFreq: 880, type: 'square', dur: 0.18, gain: 0.16 })
        break
      case 'repay':
        this.tone({ freq: 880, endFreq: 440, type: 'square', dur: 0.18, gain: 0.16 })
        break
      case 'warn':
        this.tone({ freq: 880, type: 'square', dur: 0.12, gain: 0.16 })
        this.tone({ freq: 660, type: 'square', dur: 0.14, gain: 0.16, delay: 0.18 })
        break
      case 'bankrupt':
        this.tone({ freq: 110, endFreq: 28, type: 'sawtooth', dur: 0.9, gain: 0.3 })
        this.noise({ dur: 0.7, gain: 0.12, filter: 180, delay: 0.1 })
        break
      case 'spark':
        this.noise({ dur: 0.08, gain: 0.08, filter: 5000 })
        break
    }
  }
}
