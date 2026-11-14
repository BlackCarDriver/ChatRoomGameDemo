import puppeteer from 'puppeteer-core'
const browser = await puppeteer.launch({
  executablePath: '/Applications/Google Chrome.app/Contents/MacOS/Google Chrome',
  headless: 'new',
  args: ['--no-sandbox', '--enable-unsafe-swiftshader', '--use-gl=angle', '--use-angle=swiftshader']
})
const page = await browser.newPage()
await page.setViewport({ width: 1280, height: 800 })
const errors = []
page.on('pageerror', e => errors.push('PAGEERROR ' + e.message))
page.on('console', m => { if (m.type() === 'error') errors.push('CONSOLE ' + m.text()) })
await page.goto('http://127.0.0.1:8091/?mock=1', { waitUntil: 'networkidle0', timeout: 30000 })
await new Promise(r => setTimeout(r, 2500))
const f = page.mainFrame()
console.log('icons:', await f.evaluate(() => ({
  rules: !!document.querySelector('#btnRules svg'),
  board: !!document.querySelector('#btnLeaderboard svg'),
  bank: !!document.querySelector('#btnBank svg'),
  soundOn: !!document.querySelector('#btnSound .icon-sound-on'),
  soundOff: !!document.querySelector('#btnSound .icon-sound-off'),
  muted: document.querySelector('#btnSound').classList.contains('muted')
})))
const labels = await f.evaluate(() => ({
  left: window.__coin.scene.machine.buttons.left.userData.label,
  right: window.__coin.scene.machine.buttons.right.userData.label,
  status: document.querySelector('#statusText').textContent
}))
console.log('labels:', JSON.stringify(labels))
// open leaderboard and check header/row right edges
await f.evaluate(() => document.querySelector('#btnLeaderboard').click())
await new Promise(r => setTimeout(r, 600))
const align = await f.evaluate(() => {
  const head = [...document.querySelectorAll('.leaderboard-head span')].map(e => e.getBoundingClientRect())
  const row = document.querySelector('.leaderboard-row')
  const nums = row ? [...row.querySelectorAll('.num')].map(e => e.getBoundingClientRect()) : []
  return {
    head: head.map(r => ({ l: Math.round(r.left), r: Math.round(r.right), t: e => 0 })),
    nums: nums.map(r => ({ l: Math.round(r.left), r: Math.round(r.right) }))
  }
})
console.log('align:', JSON.stringify(align))
// sound toggle
await f.evaluate(() => document.querySelector('#btnSound').click())
console.log('muted after click:', await f.$eval('#btnSound', el => el.classList.contains('muted')), 'text:', await f.$eval('#btnSound', el => el.textContent.trim()))
console.log('errors:', errors.length ? errors.join(' | ') : 'none')
await page.screenshot({ path: '/tmp/ui.png' })
await browser.close().catch(()=>{})
process.exit(0)
