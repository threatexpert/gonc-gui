type Language = 'zh' | 'en';

function dateFormat(locale: string) {
  return new Intl.DateTimeFormat(locale, {
    year: 'numeric', month: '2-digit', day: '2-digit', hour: '2-digit',
    minute: '2-digit', second: '2-digit', hourCycle: 'h23',
  });
}

const dates = {zh: dateFormat('zh-CN'), en: dateFormat('en-GB')};

function timestamp(raw: string) {
  const ms = Date.parse(raw);
  if (!Number.isFinite(ms)) return null;
  // Date omits precision below milliseconds. Go supplies up to nine digits.
  const fraction = /\.(\d+)(?:Z|[+-]\d{2}:\d{2})$/.exec(raw)?.[1] ?? '';
  return {second: Math.floor(ms / 1000), fraction: fraction.padEnd(9, '0')};
}

export function comparisonTimes(local: string, remote: string, lang: Language) {
  const a = timestamp(local), b = timestamp(remote);
  let digits = 0;
  const sameSecond = a != null && b != null && a.second === b.second;
  const equal = sameSecond && a?.fraction === b?.fraction;
  if (a && b && sameSecond && !equal) {
    digits = [3, 6, 9].find(n => a.fraction.slice(0, n) !== b.fraction.slice(0, n))!;
  }
  function format(t: ReturnType<typeof timestamp>) {
    if (!t) return '';
    const seconds = dates[lang].format(t.second * 1000);
    return digits ? seconds + '.' + t.fraction.slice(0, digits) : seconds;
  }
  const localText = format(a);
  return {local: localText, remote: equal ? localText : format(b), equal};
}
