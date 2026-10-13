// Comic's "Update AgentNet to vX to continue" banner (owner policy "latest
// only", v0.8.17): shown on top, as an alert, while the overview carries
// update_required, with what the automatic update does; in the AgentNet
// app's window it offers the app's update when the app's own status says it
// can update itself here (host.appStatus), and offers no button otherwise; a
// page served by a daemon without the app never shows an update button. At
// 1280 and 390 wide.
const assert = require('node:assert/strict'), fs = require('node:fs');
const {chromium} = require(process.env.AGENTNET_PLAYWRIGHT);
(async () => {
  const browser = await chromium.launch({headless: true, executablePath: process.env.AGENTNET_CHROMIUM || '/usr/bin/chromium', args: ['--no-sandbox', '--disable-dev-shm-usage']});
  try {
    for (const [app, supported] of [[false, false], [true, true], [true, false]]) for (const width of [1280, 390]) {
      const mode = !app ? 'daemon' : supported ? 'app' : 'app-unsupported';
      const ctx = await browser.newContext({reducedMotion: 'reduce', viewport: {width, height: 900}}), p = await ctx.newPage(), errors = [], updates = [];
      p.setDefaultTimeout(6000); p.on('pageerror', (e) => errors.push(String(e)));
      await p.route('**/api/overview*', async (route) => {
        const response = await route.fetch(), o = await response.json();
        o.app = app;
        o.update_required = {latest: 'v0.8.18', url: 'https://github.com/misunders2d/agentnet/releases/tag/v0.8.18', auto: 'Automatic update to v0.8.18 is under way.'};
        await route.fulfill({response, json: o});
      });
      let statusAnswered = false;
      await p.route('**/api/app/status', (route) => { statusAnswered = true; return route.fulfill({json: {version: 'v0.8.17', app_update_supported: supported, cli_path: '/usr/local/bin/agentnet', cli_state: 'installed', ...(supported ? {} : {problem: 'This copy of the app was moved, so it cannot update itself.'})}}); });
      await p.route('**/api/app/update', async (route) => { updates.push(route.request().postDataJSON()); await route.fulfill({json: {state: 'restarting', message: 'Restarting AgentNet with the update…'}}); });
      await p.goto(process.argv[2]);
      const banner = p.getByRole('alert').filter({hasText: 'Update AgentNet to v0.8.18 to continue'});
      await banner.waitFor();
      assert.match(await banner.innerText(), /Automatic update to v0\.8\.18 is under way/);
      const button = banner.getByRole('button', {name: 'Update now'});
      if (app && supported) await button.waitFor();
      else if (app) { for (let i = 0; i < 100 && !statusAnswered; i++) await p.waitForTimeout(20); assert.ok(statusAnswered, 'the app status was asked'); await p.evaluate(() => new Promise(requestAnimationFrame)); }
      assert.equal(await button.count(), app && supported ? 1 : 0, app && supported ? 'the app offers its update' : app ? 'an app that cannot update itself offers no update button' : 'no app: no update button');
      if (app && supported) {
        await button.click();
        await banner.getByText('Restarting AgentNet with the update…').waitFor();
        assert.equal(updates.length, 1, 'one update request');
      }
      assert.equal(await p.evaluate(() => document.documentElement.scrollWidth > innerWidth + 1), false, 'no page overflow');
      if (process.env.AGENTNET_SCREENSHOTS) { fs.mkdirSync(process.env.AGENTNET_SCREENSHOTS, {recursive: true}); await p.screenshot({path: `${process.env.AGENTNET_SCREENSHOTS}/update-required-${mode}-${width}.png`}); }
      assert.deepEqual(errors, []);
      console.log(`${mode} ${width} PASS`);
      await p.unrouteAll({behavior: 'wait'}); await ctx.close();
    }
  } finally { await browser.close(); }
  console.log('Update required rendered PASS');
})().catch((e) => { console.error(e); process.exit(1); });
