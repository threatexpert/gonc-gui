import test from 'node:test';
import assert from 'node:assert/strict';
import {comparisonTimes} from '../src/receiveComparisonFormat.js';

for (const lang of ['zh', 'en'] as const) {
  test(`adaptive timestamp precision in ${lang}`, () => {
    const base = '2026-11-19T01:02:03';
    const pair = (a: string, b: string) => comparisonTimes(base + a + 'Z', base + b + 'Z', lang);
    const equal = comparisonTimes(base + '.1234Z', '2026-11-19T09:02:03.123400000+08:00', lang);
    assert.equal(equal.equal, true);
    assert.equal(equal.local, equal.remote);
    assert.doesNotMatch(equal.local, /\.\d+$/);
    assert.match(pair('', '.001').local, /\.000$/);
    assert.match(pair('.123', '.1249').remote, /\.124$/);
    assert.match(pair('.1234', '.1235').local, /\.123400$/);
    assert.match(pair('.123456700', '.123456701').remote, /\.123456701$/);
    const different = comparisonTimes(base + '.999Z', '2026-11-19T01:02:04.888Z', lang);
    assert.equal(different.equal, false);
    assert.doesNotMatch(different.remote, /\.\d+$/);
    assert.notEqual(different.local, different.remote);
    assert.equal(comparisonTimes('', base + 'Z', lang).local, '');
    assert.equal(comparisonTimes('', '', lang).equal, false);
  });
}
