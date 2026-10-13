// Actual injected shell script; native callbacks are mocked deliberately.
// Proves per-click identity/lifetime, not Windows native runtime behavior.
const assert = require('node:assert/strict');
const fs = require('node:fs');
const { chromium } = require(process.env.AGENTNET_PLAYWRIGHT);
(async () => {
  const browser = await chromium.launch({ executablePath: process.env.AGENTNET_CHROMIUM, headless: true, args: ['--no-sandbox'] });
  try {
    const page = await browser.newPage();
    await page.route('http://agentnet.local/**', route => route.fulfill({ contentType: 'text/html', body: '<!doctype html><body>Fixture</body>' }));
    await page.goto('http://agentnet.local/');
    await page.evaluate(() => { window.__TAURI_INTERNALS__ = { invoke: async () => {} }; window.__agentnetLinuxDownloads = false; });
    await page.addScriptTag({ content: fs.readFileSync('desktop/src-tauri/src/download.js', 'utf8') });
    const result = await page.evaluate(async () => {
      const original = URL.createObjectURL(new Blob(['fixture'], { type: 'text/plain' })), clicked = [];
      document.addEventListener('click', event => {
        const link = event.composedPath().find(n => n instanceof HTMLAnchorElement);
        if (link?.href !== original) { event.preventDefault(); clicked.push(link.href); }
      }, true);
      const a = document.createElement('a'); a.href = original; a.download = 'same.txt'; document.body.append(a);
      a.click(); a.click(); a.click(); a.remove(); URL.revokeObjectURL(original);
      const rows = [...document.getElementById('agentnet-native-downloads').children];
      const live = async url => { try { const r = await fetch(url); return r.ok && await r.text() === 'fixture'; } catch { return false; } };
      const retained = await Promise.all(clicked.map(live));
      window.__agentnetDownloadProgress({ url: clicked[0], state: 'saved' });
      const firstEnded = !await live(clicked[0]), othersLive = await live(clicked[1]) && await live(clicked[2]);
      window.__agentnetDownloadProgress({ url: clicked[1], state: 'failed' });
      window.__agentnetDownloadProgress({ url: clicked[2], state: 'saved' });
      return { clicked, retained, rows: rows.length, firstEnded, othersLive, released: (await Promise.all(clicked.map(live))).every(v => !v), states: rows.map(n => n.querySelector('[role=status]').textContent) };
    });
    assert.equal(new Set(result.clicked).size, 3);
    assert.equal(result.rows, 3);
    assert.ok(result.retained.every(Boolean) && result.firstEnded && result.othersLive && result.released);
    assert.deepEqual(result.states, ['Saved to Downloads.', 'Download failed. Retry from its message.', 'Saved to Downloads.']);
    console.log(JSON.stringify({ ok: true, distinctClicks: 3, independentTerminalCleanup: true }));
  } finally { await browser.close(); }
})().catch(error => { console.error(error); process.exitCode = 1; });
