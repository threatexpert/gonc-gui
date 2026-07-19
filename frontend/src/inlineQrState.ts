export type FileTransferMode = 'send' | 'receive';

export function normalizedQrPassphrase(value: string) {
  return value.trim();
}

export function isFileTransferMode(mode: string): mode is FileTransferMode {
  return mode === 'send' || mode === 'receive';
}

export function latchSuccessfulConnection(previous: boolean, status: string) {
  return previous || status.trim().toLowerCase() === 'connected';
}

export function fileTransferReportBelongsToRun(reportRunId: number, activeRunId: number) {
  return activeRunId > 0 && reportRunId === activeRunId;
}

export function inlineQrShouldMask(
  mode: string,
  sendHasConnected: boolean,
  receiveHasConnected: boolean,
) {
  return mode === 'send'
    ? sendHasConnected
    : mode === 'receive' && receiveHasConnected;
}

export function transferStartGate(pending: boolean) {
  return {
    accepted: !pending,
    pending: true,
  };
}

export function isCurrentQrGeneration(
  requestId: number,
  currentId: number,
  generatedPassphrase: string,
  currentPassphrase: string,
) {
  return requestId === currentId
    && generatedPassphrase === normalizedQrPassphrase(currentPassphrase);
}
