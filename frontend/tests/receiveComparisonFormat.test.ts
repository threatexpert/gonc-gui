import test from 'node:test';
import assert from 'node:assert/strict';
import {comparisonTime} from '../src/receiveComparisonFormat.js';

for (const lang of ['zh', 'en'] as const) {
  test(`preserves sub-millisecond differences in ${lang}`, () => {
    const first = comparisonTime('2026-11-19T01:02:03.123456700Z', lang);
    const second = comparisonTime('2026-11-19T01:02:03.123456701Z', lang);
    assert.notEqual(first, second);
    assert.ok(first.endsWith('.123456700'));
    assert.ok(second.endsWith('.123456701'));
    assert.equal(first, comparisonTime('2026-11-19T09:02:03.1234567+08:00', lang));
    assert.ok(comparisonTime('2026-11-19T01:02:03Z', lang).endsWith('.000000000'));
  });
}
