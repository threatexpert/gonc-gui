export type AddPickerState = 'closed' | 'choose' | 'text';

export const appendUniquePaths = (current: string[], added: string[]) =>
  Array.from(new Set([...current, ...added]));

export const removePath = (current: string[], removed: string) =>
  current.filter((path) => path !== removed);

export const textCanSubmit = (text: string) => text.length > 0;

export const dropHintMode = (paths: string[]) => paths.length === 0 ? 'empty' : 'compact';
