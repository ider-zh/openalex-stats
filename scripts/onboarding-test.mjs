import test from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs';
import vm from 'node:vm';

const sandbox = {};
vm.runInNewContext(fs.readFileSync(new URL('../internal/web/static/onboarding.js', import.meta.url), 'utf8'), sandbox);
const { STORAGE_KEY, COOLDOWN_MS, shouldShow, createGate } = sandbox.OpenAlexOnboardingPolicy;
const now = Date.UTC(2026, 10, 12);
const record = dismissedAt => JSON.stringify({ version: 1, dismissedAt });
function memory(initial) {
  let value = initial, writes = 0;
  return { getItem(key) { assert.equal(key, STORAGE_KEY); return value; }, setItem(key, next) { assert.equal(key, STORAGE_KEY); value = next; writes++; }, get writes() { return writes; }, get value() { return value; } };
}

test('first visit and exact seven-day boundary', () => {
  assert.equal(shouldShow(null, now), true);
  assert.equal(shouldShow(record(now), now), false);
  assert.equal(shouldShow(record(now - COOLDOWN_MS + 60_000), now), false);
  assert.equal(shouldShow(record(now - COOLDOWN_MS), now), true);
  assert.equal(shouldShow(record(now - COOLDOWN_MS - 1), now), true);
});
test('malformed, wrong-version, non-numeric and future timestamps cannot suppress forever', () => {
  for (const raw of ['oops', 'null', '{}', record(now + 1), record(0), record(-1), record(String(now)), JSON.stringify({ version: 2, dismissedAt: now })]) assert.equal(shouldShow(raw, now), true, raw);
});
test('ordinary browsing is read-only and does not extend the original deadline', () => {
  const storage = memory(record(now - COOLDOWN_MS + 60_000));
  let clock = now;
  const gate = createGate(() => storage, () => clock);
  assert.equal(gate.eligible(), false);
  assert.equal(gate.eligible(), false);
  assert.equal(storage.writes, 0);
  clock += 60_000;
  assert.equal(gate.eligible(), true);
  assert.equal(storage.writes, 0);
});
test('completion, skip and manual re-entry share dismissal semantics', () => {
  const storage = memory(null);
  let clock = now;
  const gate = createGate(() => storage, () => clock);
  assert.equal(gate.eligible(), true);
  gate.started();
  assert.equal(storage.writes, 0, 'starting does not mark a tour complete');
  assert.equal(gate.eligible(), false, 'one automatic prompt in the current page');
  gate.dismiss();
  assert.equal(storage.writes, 1);
  assert.equal(JSON.parse(storage.value).dismissedAt, now);
  // Manual replay bypasses eligibility, and closing it renews the same cooldown.
  clock += 5_000; gate.started(); gate.dismiss();
  assert.equal(JSON.parse(storage.value).dismissedAt, clock);
  assert.equal(createGate(() => storage, () => clock).eligible(), false);
});
test('disabled/throwing storage degrades to a one-per-page prompt', () => {
  for (const storage of [() => { throw new Error('disabled'); }, () => ({ getItem() { throw new Error('blocked'); }, setItem() { throw new Error('blocked'); } }), () => null]) {
    const gate = createGate(storage, () => now);
    assert.equal(gate.eligible(), true);
    assert.doesNotThrow(() => { gate.started(); gate.dismiss(); });
    assert.equal(gate.eligible(), false);
  }
});
