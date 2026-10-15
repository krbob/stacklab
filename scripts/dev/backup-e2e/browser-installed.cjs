const fs = require('fs');
const path = require('path');
const { createHash } = require('node:crypto');
const { chromium, expect, request } = require(path.resolve(__dirname, '../../../frontend/node_modules/@playwright/test'));
const { execFileSync } = require('node:child_process');
const usage = 'node scripts/dev/backup-e2e/browser-installed.cjs --base-url ORIGIN --auth-file PRIVATE_STORAGE_STATE --output-dir NEW_DIRECTORY --expected-version VERSION [--destination-id ID] [--integrity-destination-id ID]';
const args = {};
for (let i = 2; i < process.argv.length; i += 2) {
  const key = process.argv[i];
  if (!['--base-url', '--auth-file', '--output-dir', '--expected-version', '--destination-id', '--integrity-destination-id'].includes(key) || !process.argv[i + 1] || args[key]) throw new Error(usage);
  args[key] = process.argv[i + 1];
}
for (const key of ['--base-url', '--auth-file', '--output-dir', '--expected-version']) if (!args[key]) throw new Error(usage);
const address = new URL(args['--base-url']);
if (!['http:', 'https:'].includes(address.protocol) || address.username || address.password || address.pathname !== '/' || address.search || address.hash) throw new Error('Base URL must be an HTTP(S) origin without credentials or a path');
const base = address.origin;
const authPath = path.resolve(args['--auth-file']);
const artifact = path.resolve(args['--output-dir']);
const authStat = fs.lstatSync(authPath);
if (!authStat.isFile() || (authStat.mode & 0o077) !== 0 || authStat.uid !== process.getuid()) throw new Error('Authentication state must be a private regular file owned by the current user (mode 0600)');
if (fs.existsSync(artifact)) throw new Error('Output directory already exists; choose a new private artifact directory');
fs.mkdirSync(artifact, { mode: 0o700 });
const summary = { base, harnessSHA256: createHash('sha256').update(fs.readFileSync(__filename)).digest('hex'), harnessCommit: execFileSync('git', ['rev-parse', 'HEAD'], { cwd: path.resolve(__dirname, '../../..'), encoding: 'utf8' }).trim(), time: new Date().toISOString(), node: process.version, assertions: [], skipped: [], screenshots: [], pageErrors: [], failedAssets: [] };
const record = (name) => summary.assertions.push(name);
(async () => {
  let browser;
  let context;
  let authentication;
  try {
    authentication = await request.newContext({ storageState: authPath });
    browser = await chromium.launch({ headless: true });
    const unauthenticated = await browser.newContext({ serviceWorkers: 'block' });
    const login = await unauthenticated.newPage();
    await login.goto(`${base}/backups`);
    await expect(login.getByTestId('login-password')).toBeVisible();
    record('Unauthenticated navigation presents the real login form');
    await unauthenticated.close();
    context = await browser.newContext({ viewport: { width: 1440, height: 1000 }, timezoneId: 'Europe/Warsaw', locale: 'en-US', serviceWorkers: 'block', storageState: authPath });
    await context.route('**/api/**', async route => {
      const req = route.request();
      if (req.method() !== 'GET' && req.method() !== 'HEAD') {
        summary.unexpectedMutation = { method: req.method(), path: new URL(req.url()).pathname };
        await route.abort();
        return;
      }
      await route.continue();
    });
    const page = await context.newPage();
    page.setDefaultTimeout(15000);
    page.on('pageerror', error => summary.pageErrors.push(error.message));
    page.on('response', response => {
      if (response.url().includes('/assets/') && response.status() >= 400) summary.failedAssets.push({ path: new URL(response.url()).pathname, status: response.status() });
    });
    const read = async pathname => {
      const response = await context.request.get(`${base}${pathname}`);
      if (!response.ok()) throw new Error(`${pathname} returned ${response.status()}`);
      return response.json();
    };
    const meta = await read('/api/meta');
    const overview = await read('/api/backups');
    const policy = await read('/api/backups/policy');
    summary.version = meta.app?.version;
    expect(summary.version).toBe(args['--expected-version']);
    summary.overview = { destinations: overview.destinations.length, plans: overview.plans.length, runs: overview.runs.length, helper_ready: overview.helper_ready, helper_error: overview.helper_error || '', activity: overview.protection.map(item => item.activity) };
    summary.policy = { paused: policy.paused, effective_paused: policy.effective_paused, reasons: policy.pause_reasons.map(reason => reason.code) };
    expect(typeof overview.helper_ready).toBe('boolean');
    expect(typeof overview.helper_configured).toBe('boolean');
    for (const destination of overview.destinations) {
      for (const evidence of [destination.metadata_check, destination.data_check]) {
        expect(['unknown', 'verified', 'sampled', 'failed']).toContain(evidence.status);
        expect(['unknown', 'metadata', 'subset', 'all']).toContain(evidence.scope);
      }
    }
    record('Installed API returns separate typed metadata and stored-data evidence on every destination');
    expect(policy.effective_paused).toBe(policy.pause_reasons.length > 0);
    record('Authenticated real overview/policy contract exposes helper readiness, activity and effective pause reasons');
    await page.goto(`${base}/backups?view=protection`);
    await expect(page.getByRole('heading', { name: 'Protection status', exact: true })).toBeVisible();
    if (policy.effective_paused) {
      await expect(page.getByText('Automatic backups, integrity checks and retention cleanup are paused.', { exact: false })).toBeVisible();
      record('Effective automatic pause is shown in the installed UI');
    } else summary.skipped.push('Effective pause banner: automatic work is not paused');
    if (!overview.helper_ready) {
      await expect(page.getByText(overview.helper_configured ? 'In-place recovery is unavailable:' : 'In-place recovery is not configured.', { exact: false }).first()).toBeVisible();
      record('Unavailable in-place recovery helper has an explicit warning');
    } else summary.skipped.push('Unavailable helper warning: installed helper is ready');
    const screenshot = async name => {
      await page.screenshot({ path: path.join(artifact, name), fullPage: true });
      summary.screenshots.push(name);
      expect(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth)).toBe(true);
    };
    await screenshot('protection-desktop.png');
    await page.getByRole('button', { name: 'Backup settings', exact: true }).click();
    let dialog = page.getByRole('dialog', { name: 'Backup settings', exact: true });
    await expect(dialog.getByLabel('Pause automatic backups and repository maintenance')).toHaveJSProperty('checked', policy.paused);
    await expect(dialog.getByLabel('External success heartbeat URL')).toHaveValue('');
    await screenshot('settings-desktop.png');
    await dialog.getByRole('button', { name: 'Cancel', exact: true }).click();
    record('Settings show persisted operator pause separately and never reveal heartbeat credentials');
    const journals = await read('/api/backups/recovery');
    summary.journalCount = journals.items.length;
    await page.getByRole('button', { name: 'Service recovery', exact: true }).click();
    dialog = page.getByRole('dialog', { name: 'Service recovery', exact: true });
    if (!journals.items.length) await expect(dialog.getByText('No unresolved service recovery records.')).toBeVisible();
    else for (const journal of journals.items) await expect(dialog.getByRole('heading', { name: journal.run_id, exact: true })).toBeVisible();
    await screenshot('service-recovery-desktop.png');
    await dialog.getByRole('button', { name: 'Close', exact: true }).click();
    record('Service recovery dialog agrees with live journal inventory');
    if (policy.paused && policy.pause_reasons.every(reason => reason.code === 'user_paused') && !overview.recovery_error && overview.restic_available) {
      await page.getByRole('tab', { name: 'Plans', exact: true }).click();
      const actions = page.getByRole('button', { name: 'Back up now', exact: true });
      if (await actions.count()) {
        for (const action of await actions.all()) await expect(action).toBeEnabled();
        record('Persisted user pause leaves manual capture available in the installed UI');
      } else summary.skipped.push('Manual capture during operator pause: no plan action exists');
    } else summary.skipped.push('Manual capture during operator pause: no isolated user-pause fixture with available backup tools');
    if (policy.pause_reasons.some(reason => reason.code !== 'user_paused')) {
      await page.getByRole('tab', { name: 'Plans', exact: true }).click();
      const actions = page.getByRole('button', { name: 'Back up now', exact: true });
      if (await actions.count()) {
        for (const action of await actions.all()) await expect(action).toBeDisabled();
        record('System pause blocks new captures in the installed UI');
      } else summary.skipped.push('System pause blocks new captures: no plan action exists');
    } else summary.skipped.push('System pause blocks new captures: no active system-pause fixture');
    await page.getByRole('tab', { name: 'Destinations', exact: true }).click();
    if (overview.destinations.length) await expect(page.getByText('Snapshot inventory confirmed', { exact: true }).first()).toBeVisible();
    const destinationPanel = page.locator('#backup-panel-destinations');
    for (const destination of overview.destinations.filter(item => item.status === 'available')) {
      const article = destinationPanel.locator('article').filter({ has: page.getByRole('heading', { name: destination.name, exact: true }) });
      await expect(article.getByRole('button', { name: 'Initialize', exact: true })).toBeDisabled();
    }
    if (overview.destinations.some(item => item.status === 'available')) record('Available repositories cannot be initialized in the installed UI');
    else summary.skipped.push('Initialized repository protection: no available destination fixture');
    await expect(page.getByText('Repository metadata evidence', { exact: true }).first()).toBeVisible();
    await expect(page.getByText('Stored data evidence', { exact: true }).first()).toBeVisible();
    await screenshot('destinations-desktop.png');
    if (overview.destinations.length) {
      const stableDestination = args['--destination-id'] ? overview.destinations.find(item => item.id === args['--destination-id']) : overview.destinations[0];
      if (!stableDestination) throw new Error('Requested destination is missing');
      const stableArticle = destinationPanel.locator('article').filter({ has: page.getByRole('heading', { name: stableDestination.name, exact: true }) });
      await stableArticle.getByRole('button', { name: 'Edit', exact: true }).click();
      const editor = page.getByRole('dialog', { name: 'Edit destination', exact: true });
      await expect(editor.getByLabel('Storage type', { exact: true })).toBeDisabled();
      await expect(editor.getByLabel('Repository', { exact: true })).toBeDisabled();
      await screenshot('immutable-destination-desktop.png');
      await editor.getByRole('button', { name: 'Cancel', exact: true }).click();
      record('Installed destination editor keeps type and repository address immutable');
    } else summary.skipped.push('Immutable destination editor: no configured destination');
    const integrityFailure = item => !!item.check_hold || [item.metadata_check.status, item.data_check.status].includes('failed');
    const failedDestination = args['--integrity-destination-id']
      ? overview.destinations.find(item => item.id === args['--integrity-destination-id'])
      : overview.destinations.find(item => item.data_check.status === 'failed') || overview.destinations.find(integrityFailure);
    if (args['--integrity-destination-id'] && (!failedDestination || !integrityFailure(failedDestination))) throw new Error('Requested integrity fixture is missing or has no recorded failure/hold');
    const inspectionDestination = failedDestination || overview.destinations.find(item => item.id === args['--destination-id']) || overview.destinations[0];
    if (inspectionDestination && overview.restic_available && (inspectionDestination.kind !== 'rclone' || overview.rclone_available)) {
      summary.repositoryReview = { destination_id: inspectionDestination.id, metadata_status: inspectionDestination.metadata_check.status, data_status: inspectionDestination.data_check.status };
      const article = destinationPanel.locator('article').filter({ has: page.getByRole('heading', { name: inspectionDestination.name, exact: true }) });
      await article.getByRole('button', { name: 'Check repository', exact: true }).click();
      const checkDialog = page.getByRole('dialog');
      if (inspectionDestination.data_check.status === 'failed') {
        await expect(checkDialog.getByLabel('Data-read policy')).toHaveValue('all');
        await expect(checkDialog).toContainText('Only a successful full data read can clear it');
        await checkDialog.getByLabel('Data-read policy').selectOption('metadata');
        await expect(checkDialog).toContainText('data-failure evidence unchanged');
        await checkDialog.getByLabel('Data-read policy').selectOption('all');
        record('Known stored-data failure defaults to a full read and explains why metadata checks cannot clear it');
      } else summary.skipped.push('Full-check failure default: no preexisting stored-data failure fixture');
      if (inspectionDestination.check_hold) {
        await expect(checkDialog).toContainText('Saved verification evidence is historical');
        await expect(checkDialog).toContainText(inspectionDestination.check_hold.error);
        record('Inconclusive check hold is visible separately from historical verification');
      } else summary.skipped.push('Inconclusive check hold: selected destination has no hold fixture');
      await expect(checkDialog.getByText('A full data read downloads', { exact: false })).toBeVisible();
      await expect(checkDialog).toContainText('provider download charges');
      await screenshot('check-review-desktop.png');
      await checkDialog.getByRole('button', { name: 'Cancel', exact: true }).click();
      await article.getByRole('button', { name: 'Remove stale locks', exact: true }).click();
      await expect(page.getByRole('dialog')).toContainText('refuses removal when a local lock owner is alive');
      await expect(page.getByRole('dialog')).toContainText('reserves this destination');
      await expect(page.getByRole('dialog')).toContainText('does not check or repair repository data');
      await screenshot('unlock-review-desktop.png');
      await page.getByRole('dialog').getByRole('button', { name: 'Cancel', exact: true }).click();
      record('Explicit stale-lock review explains the owner guard and unchanged integrity evidence, and is cancelled without mutation');
      await article.getByRole('button', { name: 'Run retention cleanup', exact: true }).click();
      const cleanupDialog = page.getByRole('dialog');
      if (integrityFailure(inspectionDestination)) {
        const submit = cleanupDialog.getByRole('button', { name: 'Start cleanup', exact: true });
        await expect(submit).toBeDisabled();
        await expect(cleanupDialog).toContainText('unresolved integrity failure');
        await expect(cleanupDialog).toContainText('remove recovery material needed for repair');
        expect(inspectionDestination.maintenance_review_token).toMatch(/^[a-f0-9]{64}$/);
        for (const evidence of [inspectionDestination.metadata_check, inspectionDestination.data_check].filter(item => item.status === 'failed')) {
          if (evidence.error) await expect(cleanupDialog.getByRole('alert').first()).toContainText(evidence.error);
        }
        await cleanupDialog.getByRole('textbox').fill(`${inspectionDestination.name} incorrect`);
        await expect(submit).toBeDisabled();
        await cleanupDialog.getByRole('textbox').fill(inspectionDestination.name);
        await expect(submit).toBeEnabled();
        record('Known integrity failure requires exact destination-name risk acknowledgment before cleanup');
      } else summary.skipped.push('Integrity cleanup acknowledgment: no preexisting integrity failure fixture');
      await screenshot('cleanup-review-desktop.png');
      await cleanupDialog.getByRole('button', { name: 'Cancel', exact: true }).click();
      record('Repository check and cleanup cost reviews are cancelled without starting operations');
    } else {
      if (args['--integrity-destination-id']) throw new Error('Required integrity review fixture lacks repository tools');
      summary.skipped.push('Repository decision dialogs: no destination with available backup tools');
    }
    record('Destination inventory and check-attempt details render');
    await page.getByRole('tab', { name: 'Recovery', exact: true }).click();
    const preferred = overview.destinations.filter(item => args['--destination-id'] ? item.id === args['--destination-id'] : item.status === 'available').sort((a, b) => Number(b.kind === 'local') - Number(a.kind === 'local'));
    for (const destination of preferred) {
      const response = await context.request.get(`${base}/api/backups/destinations/${encodeURIComponent(destination.id)}/snapshots`);
      if (!response.ok()) continue;
      const snapshots = await response.json();
      if (!snapshots.items.length) continue;
      for (const snapshot of snapshots.items) expect(['unknown', 'complete', 'incomplete', 'legacy', 'unsupported', 'invalid']).toContain(snapshot.recovery_status);
      await page.getByLabel('Recovery destination', { exact: true }).selectOption(destination.id);
      await page.getByRole('combobox', { name: 'Recovery set', exact: true }).selectOption(snapshots.items[0].id);
      const snapshotSource = page.getByRole('combobox', { name: 'Snapshot source', exact: true });
      await expect(snapshotSource).toBeVisible();
      await expect(page.getByText('Recovery objects:', { exact: false })).toBeVisible();
      await page.getByRole('button', { name: 'Refresh', exact: true }).click();
      await expect(page.getByRole('combobox', { name: 'Recovery set', exact: true })).toHaveValue(snapshots.items[0].id);
      await page.getByRole('tab', { name: 'History', exact: true }).click();
      await page.getByLabel('Filter by result').selectOption('failed');
      await page.getByRole('tab', { name: 'Recovery', exact: true }).click();
      await expect(page.getByRole('combobox', { name: 'Recovery set', exact: true })).toHaveValue(snapshots.items[0].id);
      await expect(page.getByRole('button', { name: 'Refresh snapshots', exact: true })).toBeEnabled();
      await screenshot('recovery-desktop.png');
      summary.recovery = { destination: destination.name, destination_id: destination.id, snapshot: snapshots.items[0].id };
      const alternate = preferred.find(item => item.id !== destination.id);
      if (alternate) {
        await page.getByLabel('Recovery destination', { exact: true }).selectOption(alternate.id);
        await expect(page.getByLabel('Recovery destination', { exact: true })).toHaveValue(alternate.id);
        await page.getByLabel('Recovery destination', { exact: true }).selectOption(destination.id);
        await expect(page.getByRole('combobox', { name: 'Recovery set', exact: true })).toHaveValue(snapshots.items[0].id);
        record('Selected recovery snapshot survives switching to another real destination and back');
      } else summary.skipped.push('Recovery selection across destinations: no alternate destination in the selected fixture set');
      record('Live recovery snapshot loads, survives explicit Refresh and tab navigation');
      break;
    }
    if (!summary.recovery) throw new Error('No available repository supplied a snapshot for live recovery inspection');
    const restoreLibrary = await read('/api/backups/restores');
    const knownRestores = restoreLibrary.items.filter(item => overview.destinations.some(destination => destination.id === item.destination_id));
    const restored = knownRestores.find(item => item.available !== false && !item.selected_files?.length) || knownRestores[0];
    if (restored) {
      expect(['unknown', 'complete', 'incomplete', 'legacy', 'unsupported', 'invalid']).toContain(restored.recovery_status);
      await page.getByLabel('Recovery destination', { exact: true }).selectOption(restored.destination_id);
      await page.getByRole('combobox', { name: 'Completed isolated restore', exact: true }).selectOption(restored.id);
      const provenance = page.getByText('Snapshot evidence at retrieval:', { exact: false }).filter({ visible: true });
      await expect(provenance).toContainText(restored.recovery_status);
      await screenshot('restore-provenance-desktop.png');
      summary.restoreProvenance = { restore_id: restored.id, recovery_status: restored.recovery_status, classified_at: restored.classified_at || null };
      record('Completed restore displays its persisted snapshot provenance independently of the selected snapshot');
      if (restored.available !== false && !restored.selected_files?.length) {
        const applyReview = page.getByRole('button', { name: 'Review applying recovered data', exact: true });
        if (await applyReview.isEnabled()) {
          await applyReview.click();
          await expect(page.getByRole('dialog').getByText('Snapshot evidence at retrieval:', { exact: false })).toContainText(restored.recovery_status);
          await screenshot('apply-provenance-desktop.png');
          await page.getByRole('dialog').getByRole('button', { name: 'Cancel', exact: true }).click();
          record('Apply mapping dialog repeats persisted restore provenance before any preview or mutation');
        } else summary.skipped.push('Apply provenance dialog: in-place recovery is currently blocked');
      } else summary.skipped.push('Apply provenance dialog: chosen restore is unavailable or contains selected files only');
      await page.getByLabel('Recovery destination', { exact: true }).selectOption(summary.recovery.destination_id);
    } else {
      summary.skipped.push('Restore provenance: no retained restore result for an existing destination');
      summary.skipped.push('Apply provenance dialog: no retained restore result for an existing destination');
    }
    await page.setViewportSize({ width: 390, height: 844 });
    for (const [tab, name] of [['Protection', 'protection-mobile.png'], ['Destinations', 'destinations-mobile.png'], ['Recovery', 'recovery-mobile.png']]) {
      const mobileTab = page.getByRole('tab', { name: tab, exact: true });
      await mobileTab.click();
      await expect(mobileTab).toHaveAttribute('aria-selected', 'true');
      await expect(page.locator('#backup-panel-' + tab.toLowerCase())).toBeVisible();
      if (tab === 'Recovery') {
        await expect(page.getByRole('combobox', { name: 'Recovery set', exact: true })).toHaveValue(summary.recovery.snapshot);
        await expect(page.getByRole('button', { name: 'Refresh snapshots', exact: true })).toBeEnabled();
      }
      await screenshot(name);
      if (tab === 'Recovery') {
        await page.getByRole('combobox', { name: 'Snapshot source', exact: true }).scrollIntoViewIfNeeded();
        await screenshot('recovery-source-mobile.png');
      }
    }
    await page.getByRole('button', { name: 'Backup settings', exact: true }).click();
    await screenshot('settings-mobile.png');
    await page.getByRole('dialog').getByRole('button', { name: 'Cancel', exact: true }).click();
    record('Mobile390px protection, destinations, recovery and settings have no horizontal overflow');
    expect(summary.pageErrors).toEqual([]);
    expect(summary.failedAssets).toEqual([]);
    record('Packaged built assets load without HTTP errors or browser page errors');
    const finalPolicy = await read('/api/backups/policy');
    expect(finalPolicy.paused).toBe(policy.paused);
    expect(finalPolicy.effective_paused).toBe(policy.effective_paused);
    record('Read-only inspection preserves existing operator pause and fixtures');
    expect(summary.unexpectedMutation).toBeUndefined();
    summary.succeeded = true;
  } catch (error) {
    summary.succeeded = false;
    summary.failure = error.message;
    if (context?.pages().length) await context.pages()[0].screenshot({ path: path.join(artifact, 'failure.png'), fullPage: true }).catch(() => {});
    throw error;
  } finally {
    if (authentication) {
      try {
        const response = await authentication.post(`${base}/api/auth/logout`, { headers: { Origin: base } });
        const verifier = await request.newContext({ storageState: authPath });
        try {
          const probe = await verifier.get(`${base}/api/backups`);
          summary.sessionRevoked = response.ok() && probe.status() === 401;
        } finally { await verifier.dispose(); }
      } catch { summary.sessionRevoked = false; }
      await authentication.dispose();
    }
    if (!summary.sessionRevoked) {
      summary.succeeded = false;
      summary.cleanupFailure = 'Could not confirm session revocation';
      process.exitCode = 1;
    }
    try { fs.unlinkSync(authPath); } catch (error) {
      summary.succeeded = false;
      summary.cleanupFailure = `Cannot remove private authentication file: ${error.message}`;
      process.exitCode = 1;
    }
    await browser?.close();
    fs.writeFileSync(path.join(artifact, 'results.json'), JSON.stringify(summary, null, 2), { mode: 0o600 });
  }
})().then(() => console.log(JSON.stringify(summary, null, 2))).catch(error => { console.error(error.message); process.exitCode = 1; });
