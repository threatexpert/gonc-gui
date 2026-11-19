// Date only retains milliseconds; keep the original RFC3339 fractional part
// separately so nanosecond differences remain visible after localization.
export function comparisonTime(modified: string, lang: 'zh' | 'en'): string {
  const fraction = /\.(\d+)(?:Z|[+-]\d{2}:\d{2})$/.exec(modified)?.[1] ?? '';
  const seconds = new Date(modified).toLocaleString(lang === 'zh' ? 'zh-CN' : 'en-GB', {
    year: 'numeric', month: '2-digit', day: '2-digit', hour: '2-digit',
    minute: '2-digit', second: '2-digit', hour12: false,
  });
  return `${seconds}.${fraction.padEnd(9, '0')}`;
}
