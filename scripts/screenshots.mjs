// README screenshots and a browser smoke test, against a demo instance (DEMO_DATA=true seeds a household with two months of history):
//
//   DATA_DIR=/tmp/network-demo DEMO_DATA=true ADMIN_EMAIL=demo@example.com ADMIN_PASSWORD=demodemo1 ADMIN_NAME=Maya PORT=18407 ./pocket-network
//   CDP=http://127.0.0.1:19271 BASE=http://127.0.0.1:18407 node scripts/screenshots.mjs   # any Chrome with remote debugging
//   SHOTS=0 … node scripts/screenshots.mjs                                                  # checks only
// playwright-core comes from web/node_modules, or set PW_FROM to a package.json next to an install of it.
import { createRequire } from 'node:module'
import assert from 'node:assert/strict'

const require = createRequire(process.env.PW_FROM || new URL('../web/package.json', import.meta.url))
let pw
try { pw = require('playwright') } catch { pw = require('playwright-core') }
const { chromium } = pw
const base = process.env.BASE || 'http://127.0.0.1:18407'
const shots = process.env.SHOTS !== '0'
const dir = new URL('../docs/screenshots/', import.meta.url).pathname
const browser = process.env.CDP ? await chromium.connectOverCDP(process.env.CDP) : await chromium.launch()
const errors = []

async function session({ phone = false, dark = false } = {}) {
  const ctx = await browser.newContext({
    viewport: phone ? { width: 390, height: 844 } : { width: 1440, height: 900 },
    deviceScaleFactor: phone ? 3 : 2, isMobile: phone, hasTouch: phone, colorScheme: dark ? 'dark' : 'light',
    userAgent: phone ? 'Mozilla/5.0 (iPhone; CPU iPhone OS 18_0 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/18.0 Mobile/15E148 Safari/604.1' : undefined,
  })
  const page = await ctx.newPage()
  page.on('pageerror', e => errors.push(`pageerror: ${e}`))
  page.on('console', m => { if (m.type() === 'error' && !/Failed to load resource/.test(m.text())) errors.push(m.text()) })
  await page.goto(base + '/login')
  const res = await page.evaluate(() => fetch('/api/v1/auth/login', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ email: 'demo@example.com', password: 'demodemo1' }) }).then(r => r.status))
  assert.equal(res, 200, 'demo sign-in')
  return { ctx, page }
}
async function go(page, path, wait = 1200) {
  await page.goto(base + path, { waitUntil: 'networkidle' })
  // headless Chrome freezes CSS fade-ins: show everything at once
  await page.addStyleTag({ content: '*, *::before, *::after { transition: none !important; animation: none !important; }' })
  await page.evaluate(() => document.fonts.ready)
  await page.waitForTimeout(wait)
}
async function shot(page, name) { if (shots) await page.screenshot({ path: dir + name + '.png' }) }
async function deviceID(page, name) {
  return page.evaluate(n => fetch('/api/v1/devices').then(r => r.json()).then(l => l.find(d => d.name === n)?.id), name)
}

// ---- desktop ----
{
  const { ctx, page } = await session()
  await go(page, '/')
  assert.ok((await page.content()).includes('Online now'), 'the overview shows the numbers')
  await shot(page, 'desktop-overview')
  await go(page, '/devices')
  assert.ok((await page.content()).includes('Photo NAS'), 'the list shows the NAS')
  await shot(page, 'desktop-devices')
  const nas = await deviceID(page, 'Photo NAS')
  await go(page, `/devices/${nas}`, 1500)
  assert.ok((await page.content()).includes('How it is recognised'), 'the device page')
  await shot(page, 'desktop-device')
  await go(page, '/review')
  assert.ok((await page.content()).includes('look like one device'), 'the review groups the look-alikes')
  await shot(page, 'desktop-review')
  await go(page, '/activity')
  await shot(page, 'desktop-activity')
  await go(page, '/sources')
  assert.ok((await page.content()).includes('Garden office'), 'the sources')
  await shot(page, 'desktop-sources')
  await go(page, '/settings/alerts')
  await shot(page, 'desktop-settings')
  await ctx.close()
}

// ---- phone ----
{
  const { ctx, page } = await session({ phone: true })
  await go(page, '/')
  await shot(page, 'phone-overview')
  const phone = await deviceID(page, "Maya's iPhone")
  await go(page, `/devices/${phone}`, 1500)
  await shot(page, 'phone-device')
  await go(page, '/review')
  await shot(page, 'phone-review')
  const overflow = await page.evaluate(() => document.documentElement.scrollWidth - window.innerWidth)
  assert.ok(overflow <= 1, `the phone layout is ${overflow}px too wide`)
  await ctx.close()
}

// ---- dark ----
{
  const { ctx, page } = await session({ dark: true })
  await go(page, '/devices')
  await shot(page, 'desktop-devices-dark')
  await ctx.close()
}

await browser.close()
if (errors.length) { console.error(errors.join('\n')); process.exit(1) }
console.log('ok' + (shots ? `, screenshots in ${dir}` : ''))
