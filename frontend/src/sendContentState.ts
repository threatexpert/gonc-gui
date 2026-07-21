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
