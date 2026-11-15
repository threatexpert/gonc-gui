import type {PassphraseMode} from './passphraseVisibility';

export const lastModeStorageKey = 'gonc.lastMode';
type ModeStorage = Pick<Storage, 'getItem' | 'setItem'>;

export function readLastMode(storage: Pick<ModeStorage, 'getItem'>): PassphraseMode {
  try {
    const mode = storage.getItem(lastModeStorageKey);
    if (mode === 'send' || mode === 'receive' || mode === 'vpnServer' || mode === 'vpnClient') return mode;
  } catch { /* Unavailable storage must not prevent startup. */ }
  return 'send';
}

export function saveLastMode(storage: Pick<ModeStorage, 'setItem'>, mode: PassphraseMode): void {
  try { storage.setItem(lastModeStorageKey, mode); } catch { /* Best-effort UI preference. */ }
}

export function startupFileMode(current: PassphraseMode, hasFiles: boolean, manuallySelected: boolean): PassphraseMode {
  return hasFiles && !manuallySelected ? 'send' : current;
}
