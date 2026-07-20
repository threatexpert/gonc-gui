export type PassphraseMode = 'send' | 'receive' | 'vpnServer' | 'vpnClient';
export type PassphraseVisibility = Record<PassphraseMode, boolean>;
export type PassphraseRevealVersions = Record<PassphraseMode, number>;
export type PassphraseTimer = number;
export type SchedulePassphraseTimer = (callback: () => void, delay: number) => PassphraseTimer;
export type CancelPassphraseTimer = (timer: PassphraseTimer) => void;

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

export function createPassphraseRevealCoordinator(
  schedule: SchedulePassphraseTimer,
  cancel: CancelPassphraseTimer,
) {
  let versions = initialPassphraseRevealVersions();
  const timers: Partial<Record<PassphraseMode, PassphraseTimer>> = {};

  return {
    reveal(mode: PassphraseMode, hide: () => void) {
      versions = nextPassphraseRevealVersions(versions, mode);
      const version = versions[mode];
      const previousTimer = timers[mode];
      if (previousTimer !== undefined) {
        cancel(previousTimer);
      }

      const timer = schedule(() => {
        if (!isCurrentPassphraseReveal(versions, mode, version) || timers[mode] !== timer) {
          return;
        }
        delete timers[mode];
        hide();
      }, 5000);
      timers[mode] = timer;
    },

    dispose() {
      for (const mode of Object.keys(timers) as PassphraseMode[]) {
        const timer = timers[mode];
        if (timer !== undefined) {
          cancel(timer);
          delete timers[mode];
        }
      }
    },
  };
}
