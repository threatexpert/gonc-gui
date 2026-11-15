export type VpnDisconnectPhase = 'idle' | 'pending' | 'stopped' | 'failed';

// A synchronous gate is needed as a second click can arrive before React has
// rendered the disabled button. Keep the result until the next explicit start.
export function createVpnDisconnectCoordinator(onChange: (phase: VpnDisconnectPhase) => void) {
  let phase: VpnDisconnectPhase = 'idle';
  const update = (next: VpnDisconnectPhase) => {
    phase = next;
    onChange(next);
  };
  return {
    isPending: () => phase === 'pending',
    reset() {
      if (phase === 'pending') return false;
      update('idle');
      return true;
    },
    async run(stop: () => Promise<void>) {
      if (phase === 'pending') return false;
      update('pending');
      try {
        await stop();
        update('stopped');
        return true;
      } catch (err) {
        update('failed');
        throw err;
      }
    },
  };
}

export function vpnDisconnectView(phase: VpnDisconnectPhase, running: boolean): VpnDisconnectPhase {
  // A timeout/error can race with the backend finishing shutdown. If it has
  // already stopped, do not keep claiming that disconnection failed.
  return phase === 'failed' && !running ? 'stopped' : phase;
}
