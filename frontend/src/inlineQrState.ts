export type FileTransferMode = 'send' | 'receive';

export async function receivePassphraseForStart(
  current: string,
  generate: () => Promise<string>,
  onGenerated: (passphrase: string) => void,
) {
  if (current.trim()) return current.trim();
  const passphrase = (await generate()).trim();
  if (!passphrase) throw new Error('enter a passphrase');
  onGenerated(passphrase);
  return passphrase;
}

export function normalizedQrPassphrase(value: string) {
  return value.trim();
}

export function maskAfterPassphraseUpdate(
  masked: boolean,
  running: boolean,
  previousPassphrase: string,
  nextPassphrase: string,
) {
  if (!masked || running) {
    return masked;
  }
  return normalizedQrPassphrase(previousPassphrase)
    === normalizedQrPassphrase(nextPassphrase);
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

export type QrGenerationRequest = {
  requestId: number;
  passphrase: string;
};

export function qrGenerationRequest(requestId: number, passphrase: string): QrGenerationRequest {
  return {
    requestId,
    passphrase: normalizedQrPassphrase(passphrase),
  };
}

export function isCurrentQrRequest(
  request: QrGenerationRequest,
  currentId: number,
  currentPassphrase: string,
) {
  return isCurrentQrGeneration(
    request.requestId,
    currentId,
    request.passphrase,
    currentPassphrase,
  );
}
