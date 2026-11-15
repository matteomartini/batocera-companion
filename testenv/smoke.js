const {chromium} = require('playwright');
(async () => {
  const b = await chromium.launch(); const p = await b.newPage({viewport: {width: 1280, height: 900}});
  const errs = []; p.on('pageerror', e => errs.push('JS: ' + e.message));
  p.on('response', r => { if (r.url().includes('/api/') && r.status() >= 400) errs.push('HTTP ' + r.status() + ' ' + r.url()); });
  const base = process.argv[2] || 'http://localhost:9802';
  for (const v of ['dash', 'roms', 'check', 'emu', 'games', 'plancia', 'dats', 'bios', 'look', 'addons', 'backup', 'shell', 'settings']) {
    await p.goto(base + '/#' + v); await p.waitForTimeout(2500);
    const toasts = await p.$$eval('.toast.err', els => els.map(e => e.textContent));
    console.log(v, 'errtoasts:', JSON.stringify(toasts));
    await p.screenshot({path: '/tmp/shot-' + v + '.png'});
  }
  console.log('errors:', errs);
  await b.close();
})();
