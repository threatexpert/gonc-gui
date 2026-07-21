export type AddPickerState = 'closed' | 'choose' | 'text';
export type AddPickerAction = 'open' | 'text' | 'back' | 'close';

export function nextAddPickerState(
  state: AddPickerState,
  action: AddPickerAction,
): AddPickerState {
  if (action === 'close') {
    return 'closed';
  }
  if (state === 'closed' && action === 'open') {
    return 'choose';
  }
  if (state === 'choose' && action === 'text') {
    return 'text';
  }
  if (state === 'text' && action === 'back') {
    return 'choose';
  }
  return state;
}

export const appendUniquePaths = (current: string[], added: string[]) =>
  Array.from(new Set([...current, ...added]));

export const removePath = (current: string[], removed: string) =>
  current.filter((path) => path !== removed);

export const textCanSubmit = (text: string) => text.length > 0;

export const dropHintMode = (paths: string[]) => paths.length === 0 ? 'empty' : 'compact';

export type SharePathMutation = {
  propose: (current: string[]) => string[];
  generatedOnFailure?: string[];
  releaseAfterSuccess?: (current: string[], proposed: string[]) => string[];
};

export type SharePathMutationResult =
  | {ok: true; changed: boolean}
  | {ok: false; changed: false; error: string};

export type SharePathTransactionDependencies = {
  getPaths: () => string[];
  isRunning: () => boolean;
  update: (proposed: string[]) => Promise<void>;
  commit: (proposed: string[]) => void;
  release: (paths: string[]) => Promise<void>;
  clearError: () => void;
  showError: (message: string) => void;
  formatError: (error: unknown) => string;
  onPendingChange: (pending: boolean) => void;
};

const samePaths = (left: string[], right: string[]) =>
  left.length === right.length && left.every((path, index) => path === right[index]);

export function createSharePathTransactionCoordinator(
  dependencies: SharePathTransactionDependencies,
) {
  let tail: Promise<void> = Promise.resolve();
  let pendingCount = 0;

  async function execute(mutation: SharePathMutation): Promise<SharePathMutationResult> {
    dependencies.clearError();
    const current = dependencies.getPaths();
    const proposed = mutation.propose(current);
    const generatedOnFailure = mutation.generatedOnFailure || [];
    if (samePaths(current, proposed)) {
      if (generatedOnFailure.length > 0) {
        await dependencies.release(generatedOnFailure).catch(() => undefined);
      }
      return {ok: true, changed: false};
    }

    if (dependencies.isRunning()) {
      try {
        await dependencies.update(proposed);
      } catch (error) {
        if (generatedOnFailure.length > 0) {
          await dependencies.release(generatedOnFailure).catch(() => undefined);
        }
        const message = dependencies.formatError(error);
        dependencies.showError(message);
        return {ok: false, changed: false, error: message};
      }
    }

    dependencies.commit(proposed);
    if (mutation.releaseAfterSuccess) {
      const released = mutation.releaseAfterSuccess(current, proposed);
      if (released.length > 0) {
        await dependencies.release(released).catch(() => undefined);
      }
    }
    return {ok: true, changed: true};
  }

  function enqueue(mutation: SharePathMutation): Promise<SharePathMutationResult> {
    pendingCount += 1;
    if (pendingCount === 1) {
      dependencies.onPendingChange(true);
    }
    const operation = tail.catch(() => undefined).then(() => execute(mutation));
    tail = operation.then(() => undefined, () => undefined);
    return operation.finally(() => {
      pendingCount -= 1;
      if (pendingCount === 0) {
        dependencies.onPendingChange(false);
      }
    });
  }

  return {enqueue};
}
