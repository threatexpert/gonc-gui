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
