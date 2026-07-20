# Desktop Passphrase Visibility Isolation Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Give desktop send, receive, VPN client, and VPN server independent five-second passphrase visibility without mode switches cancelling or leaking reveal state.

**Architecture:** Add a small pure visibility-policy module that owns the four-mode state shape and per-mode reveal generations. `App.tsx` keeps one timeout handle per mode, passes the initiating mode explicitly through every reveal action, and removes password timeout cleanup from the mode-dependent event subscription effect.

**Tech Stack:** React 18, TypeScript 4.6, Node test runner, Vite 3, Wails v2.

## Global Constraints

- Desktop only; Android visibility behavior is unchanged.
- `send`, `receive`, `vpnClient`, and `vpnServer` each own independent visibility state and an independent timeout.
- All four modes begin hidden.
- A reveal schedules only its target mode to hide after `5000ms`; another reveal in that mode restarts only that mode's interval.
- Changing modes does not cancel, restart, reveal, or hide any mode's timer or state.
- A stale timeout cannot hide a newer reveal in the same mode.
- Random generation, copy, paste, password scan, and passphrase QR activation retain the initiating mode across asynchronous work.
- Application unmount clears every outstanding passphrase visibility timeout.
- Do not change passphrase values, QR payloads, QR masking, connection/transfer state, VPN profiles, action availability, running-mode editing rules, or layout.
- Do not create a Git branch or worktree; work directly in the current checkout.
- Preserve the user's existing uncommitted `VERSION` change and do not stage, restore, overwrite, or commit it.

---

## File structure

- `frontend/src/passphraseVisibility.ts`: pure four-mode visibility and reveal-generation policy.
- `frontend/tests/inlineQrState.test.ts`: unit coverage for isolation and stale-timeout ownership plus a focused `App.tsx` source contract.
- `frontend/tsconfig.inline-qr-test.json`: compile the new pure policy into the existing Node test bundle.
- `frontend/src/App.tsx`: per-mode state/timers, explicit asynchronous action ownership, and unmount-only timer cleanup.

---

### Task 1: Implement independent desktop passphrase visibility

**Files:**
- Create: `frontend/src/passphraseVisibility.ts`
- Modify: `frontend/tests/inlineQrState.test.ts`
- Modify: `frontend/tsconfig.inline-qr-test.json`
- Modify: `frontend/src/App.tsx`

**Interfaces:**
- Produces: `PassphraseMode`, `PassphraseVisibility`, `PassphraseRevealVersions`
- Produces: `hiddenPassphraseVisibility()`, `initialPassphraseRevealVersions()`, `withPassphraseVisibility(...)`, `nextPassphraseRevealVersions(...)`, `isCurrentPassphraseReveal(...)`
- Preserves: existing `Mode` values, passphrase setters, QR dialog, scan workflow, event subscriptions, and all transfer/VPN behavior

- [ ] **Step 1: Write failing pure-policy and source-contract tests**

Add these imports to `frontend/tests/inlineQrState.test.ts`:

```ts
import {readFileSync} from 'node:fs';
import {
  hiddenPassphraseVisibility,
  initialPassphraseRevealVersions,
  isCurrentPassphraseReveal,
  nextPassphraseRevealVersions,
  withPassphraseVisibility,
} from '../src/passphraseVisibility.js';
```

Add the tests:

```ts
test('all desktop passphrase modes begin hidden and reveal independently', () => {
  const hidden = hiddenPassphraseVisibility();
  assert.deepEqual(hidden, {
    send: false,
    receive: false,
    vpnServer: false,
    vpnClient: false,
  });

  const sendVisible = withPassphraseVisibility(hidden, 'send', true);
  assert.deepEqual(sendVisible, {
    send: true,
    receive: false,
    vpnServer: false,
    vpnClient: false,
  });

  const receiveVisible = withPassphraseVisibility(sendVisible, 'receive', true);
  assert.equal(receiveVisible.send, true);
  assert.equal(receiveVisible.receive, true);
  assert.equal(withPassphraseVisibility(receiveVisible, 'send', false).receive, true);
});

test('each desktop passphrase mode owns its reveal generation', () => {
  const initial = initialPassphraseRevealVersions();
  const firstSend = nextPassphraseRevealVersions(initial, 'send');
  const receive = nextPassphraseRevealVersions(firstSend, 'receive');
  const secondSend = nextPassphraseRevealVersions(receive, 'send');

  assert.equal(isCurrentPassphraseReveal(secondSend, 'send', firstSend.send), false);
  assert.equal(isCurrentPassphraseReveal(secondSend, 'send', secondSend.send), true);
  assert.equal(isCurrentPassphraseReveal(secondSend, 'receive', receive.receive), true);
  assert.equal(secondSend.vpnServer, 0);
  assert.equal(secondSend.vpnClient, 0);
});

test('desktop mode subscription cleanup does not own passphrase timers', () => {
  const source = readFileSync('src/App.tsx', 'utf8');
  const refreshIndex = source.indexOf('    refreshStatus();');
  const effectStart = source.lastIndexOf('useEffect(() => {', refreshIndex);
  const effectEnd = source.indexOf('  }, [mode]);', effectStart);
  assert.ok(refreshIndex >= 0 && effectStart >= 0 && effectEnd > effectStart);
  const modeEffect = source.slice(effectStart, effectEnd);
  assert.doesNotMatch(modeEffect, /passwordTimers|passwordTimer|clearTimeout/);
  assert.match(source, /type=\{passwordVisibility\[mode\] \? 'text' : 'password'\}/);
  assert.match(source, /Object\.values\(passwordTimers\.current\)/);
  assert.match(source, /function revealPasswordTemporarily\(targetMode: Mode\)/);
  assert.match(source, /scanPasswordMode\.current = mode/);
});
```

- [ ] **Step 2: Include the policy module in the existing test compilation**

Change `frontend/tsconfig.inline-qr-test.json` to:

```json
{
  "compilerOptions": {
    "target": "ES2020",
    "module": "ES2020",
    "moduleResolution": "Node",
    "allowSyntheticDefaultImports": true,
    "strict": true,
    "skipLibCheck": true,
    "rootDir": ".",
    "outDir": "node_modules/.cache/gonc-inline-qr-tests"
  },
  "include": [
    "src/inlineQrState.ts",
    "src/passphraseVisibility.ts",
    "tests/inlineQrState.test.ts"
  ]
}
```

- [ ] **Step 3: Run the focused test to verify RED**

Run from `frontend/`:

```powershell
npm run test:inline-qr
```

Expected: TypeScript compilation fails because `src/passphraseVisibility.ts` and its exports do not exist.

- [ ] **Step 4: Implement the pure four-mode policy**

Create `frontend/src/passphraseVisibility.ts`:

```ts
export type PassphraseMode = 'send' | 'receive' | 'vpnServer' | 'vpnClient';
export type PassphraseVisibility = Record<PassphraseMode, boolean>;
export type PassphraseRevealVersions = Record<PassphraseMode, number>;

export function hiddenPassphraseVisibility(): PassphraseVisibility {
  return {send: false, receive: false, vpnServer: false, vpnClient: false};
}

export function initialPassphraseRevealVersions(): PassphraseRevealVersions {
  return {send: 0, receive: 0, vpnServer: 0, vpnClient: 0};
}

export function withPassphraseVisibility(
  current: PassphraseVisibility,
  mode: PassphraseMode,
  visible: boolean,
): PassphraseVisibility {
  return {...current, [mode]: visible};
}

export function nextPassphraseRevealVersions(
  current: PassphraseRevealVersions,
  mode: PassphraseMode,
): PassphraseRevealVersions {
  return {...current, [mode]: current[mode] + 1};
}

export function isCurrentPassphraseReveal(
  current: PassphraseRevealVersions,
  mode: PassphraseMode,
  version: number,
) {
  return current[mode] === version;
}
```

- [ ] **Step 5: Run the focused test to confirm the remaining App contract stays RED**

Run from `frontend/`:

```powershell
npm run test:inline-qr
```

Expected: pure policy tests pass, while `desktop mode subscription cleanup does not own passphrase timers` fails because `App.tsx` still uses the global `passwordVisible`/`passwordTimer` and clears it from the `[mode]` effect.

- [ ] **Step 6: Replace global visibility with per-mode state and ownership**

Import the policy in `frontend/src/App.tsx`:

```ts
import {
  hiddenPassphraseVisibility,
  initialPassphraseRevealVersions,
  isCurrentPassphraseReveal,
  nextPassphraseRevealVersions,
  withPassphraseVisibility,
  type PassphraseMode,
} from './passphraseVisibility';
```

Replace the local mode alias and global visibility state/ref with:

```ts
type Mode = PassphraseMode;

const [passwordVisibility, setPasswordVisibility] = useState(hiddenPassphraseVisibility);
const passwordTimers = useRef<Partial<Record<Mode, number>>>({});
const passwordRevealVersions = useRef(initialPassphraseRevealVersions());
const scanPasswordMode = useRef<Mode>('send');
```

Remove `passwordVisible` and `passwordTimer`.

- [ ] **Step 7: Implement target-specific reveal and unmount cleanup**

Replace `revealPasswordTemporarily()` with:

```ts
function revealPasswordTemporarily(targetMode: Mode) {
  const versions = nextPassphraseRevealVersions(passwordRevealVersions.current, targetMode);
  passwordRevealVersions.current = versions;
  const version = versions[targetMode];
  setPasswordVisibility((current) => withPassphraseVisibility(current, targetMode, true));

  const previousTimer = passwordTimers.current[targetMode];
  if (previousTimer !== undefined) {
    window.clearTimeout(previousTimer);
  }
  passwordTimers.current[targetMode] = window.setTimeout(() => {
    if (!isCurrentPassphraseReveal(passwordRevealVersions.current, targetMode, version)) {
      return;
    }
    setPasswordVisibility((current) => withPassphraseVisibility(current, targetMode, false));
    delete passwordTimers.current[targetMode];
  }, 5000);
}
```

Add an unmount-only effect near the refs/effects:

```ts
useEffect(() => () => {
  for (const timer of Object.values(passwordTimers.current)) {
    if (timer !== undefined) {
      window.clearTimeout(timer);
    }
  }
}, []);
```

Delete password timeout cleanup from the event-subscription effect whose dependency is `[mode]`.

- [ ] **Step 8: Bind every reveal action to its initiating mode**

Use literal targets for mode-specific generators:

```ts
revealPasswordTemporarily('send');
revealPasswordTemporarily('receive');
revealPasswordTemporarily('vpnServer');
revealPasswordTemporarily('vpnClient');
```

At the start of `copyPassword`, `pastePassword`, and `showPasswordQr`, capture:

```ts
const targetMode = mode;
```

Use `targetMode` for all branching within that invocation and call `revealPasswordTemporarily(targetMode)` after success. Add this helper for paste/scan assignment:

```ts
function setPasswordForMode(targetMode: Mode, value: string) {
  if (targetMode === 'send') {
    setSendPassword(value);
  } else if (targetMode === 'receive') {
    setReceivePassword(value);
  } else if (targetMode === 'vpnServer') {
    setVpnServerPassword(value);
  } else {
    setVpnProfileField('passphrase', value);
  }
}
```

When `startScreenScan('password')` begins, set `scanPasswordMode.current = mode`. In `decodeScanRegion`, assign and reveal using `scanPasswordMode.current` rather than the current rendered `mode`. Keep VPN profile scanning unchanged.

- [ ] **Step 9: Render visibility for only the active mode**

Change the passphrase input type to:

```tsx
type={passwordVisibility[mode] ? 'text' : 'password'}
```

No other markup or styling changes.

- [ ] **Step 10: Run focused and production frontend verification**

Run from `frontend/`:

```powershell
npm run test:inline-qr
npm run build
```

Expected: all Node tests pass; TypeScript and Vite production build exit `0`.

- [ ] **Step 11: Commit the tested fix**

```powershell
git add frontend/src/passphraseVisibility.ts frontend/tests/inlineQrState.test.ts frontend/tsconfig.inline-qr-test.json frontend/src/App.tsx
git commit -m "fix: isolate desktop passphrase visibility"
```

Do not stage or commit `VERSION`.

---

### Task 2: Final desktop verification

**Files:**
- Modify only if verification exposes a defect in the scoped visibility change.

**Interfaces:**
- Verifies the independent visibility policy and its integration without changing QR, transfer, or VPN behavior.

- [ ] **Step 1: Re-run frontend tests and production build without accepting stale output**

Run from `frontend/`:

```powershell
npm run test:inline-qr
npm run build
```

Expected: TypeScript recompiles the test bundle, every Node test passes, and Vite exits `0`.

- [ ] **Step 2: Check the scoped diff and repository state**

Run from the repository root:

```powershell
git diff --check -- frontend/src/passphraseVisibility.ts frontend/tests/inlineQrState.test.ts frontend/tsconfig.inline-qr-test.json frontend/src/App.tsx
git status --short
```

Expected: no scoped whitespace errors and no uncommitted frontend visibility files. The pre-existing user-owned `VERSION` modification remains unaltered.

- [ ] **Step 3: Perform desktop interaction smoke when available**

Verify:

- revealing send leaves receive and both VPN modes hidden;
- switching away and back before five seconds shows only the initiating mode for its remaining interval;
- switching back after five seconds shows stars;
- revealing receive while send is visible does not cancel or extend send;
- repeating an action in one mode restarts only that mode's five seconds;
- random, copy, paste, password scan, and QR activation reveal only their initiating modes.

If no interactive desktop environment is available, record interaction smoke as not executed rather than passed.
