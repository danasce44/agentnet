// Real Comic package and guarded file API: each thumbnail opens its original.
const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const { chromium } = require(process.env.AGENTNET_PLAYWRIGHT);

(async () => {
  const browser = await chromium.launch({ headless: true, executablePath: process.env.AGENTNET_CHROMIUM || '/usr/bin/chromium', args: ['--no-sandbox', '--disable-dev-shm-usage'] });
  try {
    for (const phone of [false, true]) {
      const ctx = await browser.newContext({ viewport: phone ? { width: 390, height: 844 } : { width: 1440, height: 900 }, hasTouch: phone, acceptDownloads: true });
      if (phone) await ctx.addInitScript(() => {
        window.__agentnetPlatform = 'android';
        window.__agentnetOnBack = fn => { window.imageViewerBack = fn; return () => { delete window.imageViewerBack; }; };
      });
      const p = await ctx.newPage(), errors = [];
      p.setDefaultTimeout(10000);
      p.on('pageerror', e => errors.push(String(e)));
      p.on('console', m => { if (m.type() === 'error') errors.push({ text: m.text(), url: m.location().url }); });
      await p.goto(process.argv[2]);
      assert.equal(await p.title(), 'AgentNet');
      assert.equal(new URL(p.url()).origin, new URL(process.argv[2]).origin);
      await p.getByRole('button', { name: /^Bob/ }).first().click();
      for (const [name, index, width, height] of [['wide.png', 2, 2400, 1600], ['tall.png', 5, 1000, 2000]]) {
        const thumb = p.getByRole('button', { name: 'Open picture ' + name, exact: true });
        await thumb.locator('img').waitFor();
        await thumb.locator('img').evaluate(im => im.decode());
        const preview = await thumb.locator('img').evaluate(im => ({ width: im.clientWidth, height: im.clientHeight, natural: im.naturalWidth / im.naturalHeight }));
        assert(preview.width > 0 && preview.height > 0, 'preview remains visible');
        assert(Math.abs(preview.width / preview.height - preview.natural) < .02, 'portrait and landscape previews retain whole original ratio');
        assert(preview.height <= (phone ? 844 : 900) * .6 + 1, 'media preview remains bounded by viewport');
        await thumb.click();
        const dialog = p.getByRole('dialog', { name, exact: true }), image = dialog.getByRole('img', { name, exact: true });
        await dialog.waitFor();
        await image.evaluate(im => im.decode());
        assert.deepEqual(await dialog.evaluate(d => {
          const b = d.getBoundingClientRect();
          return [b.x, b.y, b.width, b.height];
        }), [0, 0, phone ? 390 : 1440, phone ? 844 : 900], 'viewer fills the viewport');
        assert.deepEqual(await image.evaluate(im => [im.naturalWidth, im.naturalHeight]), [width, height], 'selected original resolution');
        assert.equal(await image.getAttribute('src'), await thumb.locator('img').getAttribute('src'), 'viewer uses this thumbnail’s original');
        const original = await ctx.request.get(new URL(process.argv[2]).origin + `/api/files/picture-message/${index}?dir=in`);
        assert(original.ok(), 'guarded original attachment read succeeds');
        const originalBytes = await original.body();
        const stage = dialog.getByRole('region');
        await p.waitForFunction(im => im.clientWidth > 0 && im.clientWidth <= im.parentElement.clientWidth + 1 && im.clientHeight <= im.parentElement.clientHeight + 1, await image.elementHandle(), { timeout: 5000 }).catch(async e => {
          console.error('Fit geometry:', await image.evaluate(im => ({ image: [im.clientWidth, im.clientHeight], stage: [im.parentElement.clientWidth, im.parentElement.clientHeight], style: im.getAttribute('style') })));
          throw e;
        });
        const fitted = await image.boundingBox();
        const box = await stage.boundingBox();
        assert(fitted.width <= box.width + 1 && fitted.height <= box.height + 1, 'whole image fits initially');
        await dialog.getByRole('button', { name: '100%', exact: true }).click();
        assert.equal(await dialog.getByLabel('Zoom level').textContent(), '100%');
        assert.deepEqual(await image.evaluate(im => [im.getBoundingClientRect().width, im.getBoundingClientRect().height]), [width, height], '100% displays original pixels');
        await p.mouse.move(box.x + box.width / 2, box.y + box.height / 2);
        await p.mouse.down();
        await p.mouse.move(box.x + box.width / 2 + 60, box.y + box.height / 2 + 40);
        await p.mouse.up();
        assert.notEqual(await image.evaluate(im => im.style.transform), 'translate(0px, 0px)', 'drag pans the enlarged image');
        await stage.press('ArrowRight');
        await dialog.getByRole('button', { name: 'Fit', exact: true }).click();
        assert.equal(await image.evaluate(im => im.style.transform), 'translate(0px, 0px)');
        const before = Number((await dialog.getByLabel('Zoom level').textContent()).replace('%', ''));
        await dialog.getByRole('button', { name: 'Zoom in', exact: true }).click();
        assert(Number((await dialog.getByLabel('Zoom level').textContent()).replace('%', '')) > before);
        await dialog.getByRole('button', { name: 'Zoom out', exact: true }).click();
        await stage.hover();
        await p.mouse.wheel(0, -120);
        await p.waitForTimeout(100);
        assert(Number((await dialog.getByLabel('Zoom level').textContent()).replace('%', '')) > before, 'wheel zooms');
        await dialog.getByRole('button', { name: 'Fit', exact: true }).click();
        if (phone) {
          const cdp = await ctx.newCDPSession(p);
          const touches = distance => [{ x: box.x + box.width / 2 - distance / 2, y: box.y + box.height / 2 }, { x: box.x + box.width / 2 + distance / 2, y: box.y + box.height / 2 }];
          await cdp.send('Input.dispatchTouchEvent', { type: 'touchStart', touchPoints: touches(60) });
          await cdp.send('Input.dispatchTouchEvent', { type: 'touchMove', touchPoints: touches(120) });
          await cdp.send('Input.dispatchTouchEvent', { type: 'touchEnd', touchPoints: [] });
          assert(Number((await dialog.getByLabel('Zoom level').textContent()).replace('%', '')) > before, 'real touch pinch zooms');
          assert.equal(await dialog.getByRole('button', { name: 'Open in default app', exact: true }).count(), 0, 'Android has no desktop action');
        } else {
          await dialog.getByRole('button', { name: 'Open in default app', exact: true }).click();
          await p.getByRole('status').filter({ hasText: 'Asked your default image app to open it.' }).waitFor();
        }
        const download = p.waitForEvent('download');
        await dialog.getByRole('button', { name: 'Download', exact: true }).click();
        const d = await download;
        assert.equal(d.suggestedFilename(), name);
        assert.deepEqual(fs.readFileSync(await d.path()), originalBytes, 'Download keeps exact original');
        assert.equal(await dialog.getByRole('button', { name: 'Use as my picture', exact: true }).count(), 1);
        await dialog.getByRole('button', { name: 'Close', exact: true }).focus();
        await p.keyboard.press('Tab');
        assert(await dialog.evaluate(d => d.contains(d.getRootNode().activeElement)), 'Tab stays inside the viewer');
        assert.equal(await p.locator('.an-app').evaluate(el => el.inert), true);
        assert.equal(await p.evaluate(() => document.documentElement.scrollWidth > innerWidth), false);
        if (process.env.AGENTNET_SCREENSHOTS) {
          fs.mkdirSync(process.env.AGENTNET_SCREENSHOTS, { recursive: true, mode: 0o700 });
          await dialog.getByRole('button', { name: 'Fit', exact: true }).click();
          await p.screenshot({ path: path.join(process.env.AGENTNET_SCREENSHOTS, `image-viewer-${phone ? '390' : '1440'}-${name}.png`) });
        }
        if (phone) assert.equal(await p.evaluate(() => window.imageViewerBack()), true, 'Android Back closes the viewer');
        else await p.keyboard.press('Escape');
        await dialog.waitFor({ state: 'hidden' });
        assert.equal(await p.locator('.an-app').evaluate(el => el.inert), false);
        assert(await thumb.evaluate(el => el.getRootNode().activeElement === el), 'focus returns to the exact thumbnail');
      }
      assert.deepEqual(errors, [], 'no runtime or console errors');
      await ctx.close();
    }
    console.log('image viewer rendered PASS: fullscreen, originals, zoom/pan/pinch, download, desktop open, focus and Android Back');
  } finally {
    await browser.close();
  }
})().catch(e => { console.error(e); process.exit(1); });
