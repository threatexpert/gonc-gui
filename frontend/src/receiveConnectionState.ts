export function isPeerWaitTimeout(status: string) {
  return status.trim().toLowerCase() === 'error:hello-timeout';
}

// A connected path owns the receiver until that same path disconnects.
// Run IDs are checked by the caller before reports reach this reducer.
export function acceptReceiveReport<T extends {topic: string; status: string}>(current: T | null, next: T): T {
  if (current?.status === 'connected' && current.topic !== next.topic) return current;
  return next;
}
