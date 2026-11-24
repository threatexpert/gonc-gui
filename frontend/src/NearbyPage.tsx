import {useCallback, useEffect, useMemo, useRef, useState, type ReactNode} from 'react';
import {EventsOff, EventsOn} from '../wailsjs/runtime/runtime';
import {
  NearbyAccept,
  NearbyCancel,
  NearbyChooseSaveDir,
  NearbyClearFinished,
  NearbyConfirmPairing,
  NearbyDecline,
  NearbyForget,
  NearbyPair,
  NearbyQuickSend,
  NearbyRefresh,
  NearbySend,
  NearbySetNickname,
  NearbySetQuickReceive,
  NearbySetVisible,
  NearbyState,
  NearbyTransfers,
} from '../wailsjs/go/main/App';
import type {nearby, nearbyapp} from '../wailsjs/go/models';
import {Icon, type IconName} from './Icon';
import './Nearby.css';

type Lang = 'zh' | 'en';

const strings = {
  en: {
    title: 'Nearby',
    subtitle: 'Send and receive files with devices on the same network. No passphrase needed.',
    readyTitle: (name: string) => `Ready to receive as ${name}`,
    readyBody: 'Keep this page open. Nearby devices can send you files, and you are asked before anything is saved.',
    hiddenTitle: 'Hidden from nearby devices',
    hiddenBody: 'Other devices cannot find this one. You can still send.',
    showMe: 'Show this device',
    rename: 'Rename',
    devices: 'Nearby devices',
    searching: 'Looking for devices…',
    locale: 'en',
    lastSeenAgo: (ago: string) => `Last seen ${ago}`,
    neverSeen: 'Not seen since pairing',
    showMore: (n: number) => `Show ${n} more`,
    showLess: 'Show less',
    searchingMore: 'Looking for more devices…',
    searchingMoreHint: 'Open Nearby in Gonc on another device to see it here.',
    lastSeen: (seconds: number) => `Last seen ${seconds} s ago`,
    notNearby: 'Not nearby right now',
    pairedAway: 'Paired, not nearby',
    searchingHint: 'Open Nearby in Gonc on the other device and keep both on the same network.',
    paired: 'Paired',
    notPaired: 'Not paired',
    send: 'Send',
    pair: 'Pair',
    pairAndSend: 'Pair & send',
    quickSendHint: 'Send without comparing numbers. Use it for files that are fine for anyone on this network to see.',
    pairAndSendHint: 'Compare numbers first, so only this device can receive the files.',
    quickReceive: 'Accept files from unpaired devices',
    quickReceiveHint: 'You are always asked first. Turn this off to receive only from paired devices.',
    unverifiedTitle: 'Not paired',
    unverifiedBody: (name: string) => `This device is not paired with ${name}, so anyone on your network could be using that name. Accept only files you are expecting.`,
    unpaired: 'not paired',
    forget: 'Forget',
    forgetConfirm: (name: string) => `Forget ${name}? You will need to compare numbers again to send to it.`,
    forgetSure: 'Click again to forget',
    addFirst: 'Choose what to send first, then pick a device.',
    sendHintTitle: 'Want to send something?',
    sendHintBody: 'Choose files, folders or text, or drop files here. Then pick a device below.',
    chooseFiles: 'Choose files',
    fallbackHint: 'Other device not listed? Send with a passphrase instead. It also works between different networks.',
    useSend: 'Send Files',
    useReceive: 'Receive Files',
    transfers: 'Transfers',
    clearFinished: 'Clear finished',
    noTransfers: 'Files you send or receive appear here.',
    cancel: 'Cancel',
    reveal: 'Show in folder',
    thisDevice: 'This device',
    deviceName: 'Device name',
    save: 'Save',
    visible: 'Visible to nearby devices',
    visibleHint: 'Others can see this name while this page is open.',
    saveTo: 'Save received files to',
    choose: 'Choose',
    chooseTitle: 'Choose where to save received files',
    storageWarning: 'Pairing keys on this system are stored in a file only you can read, not in the system keychain.',
    discoveryWarning: 'Automatic discovery is unavailable on this network. Use Send Files and Receive Files instead.',
    pairTitleOut: (name: string) => `Pair with ${name}`,
    pairTitleIn: (name: string) => `${name} wants to pair`,
    pairBody: (name: string) => `Check that ${name} shows the same number, then confirm on both devices.`,
    numbersMatch: 'Numbers match',
    waitingOther: (name: string) => `Waiting for ${name} to confirm…`,
    pairedNotice: (name: string) => `Paired with ${name}.`,
    pairRejected: (name: string) => `Pairing with ${name} was cancelled.`,
    pairFailed: (name: string, msg: string) => `Could not pair with ${name}: ${msg}`,
    offerTitle: (name: string) => `${name} wants to send you files`,
    offerSummary: (count: number, size: string) => `${count} ${count === 1 ? 'file' : 'files'} · ${size}`,
    more: (n: number) => `and ${n} more`,
    savingTo: 'Saving to',
    decline: 'Decline',
    accept: 'Accept',
    status: {
      connecting: 'Connecting…',
      waiting: 'Waiting for the other device to accept…',
      offered: 'Waiting for your answer',
      transferring: 'Transferring',
      completed: 'Done',
      declined: 'Declined',
      cancelled: 'Cancelled',
      failed: 'Failed',
    } as Record<string, string>,
    to: 'To',
    from: 'From',
    connectingTo: 'Connecting…',
    cancelling: 'Cancelling…',
    change: 'Change',
    noOverwrite: 'Nothing is overwritten: if a file with the same name exists, the new one is saved with a number added.',
    activeSend: (status: string) => `Sending · ${status}`,
    cardStatus: {connecting: 'connecting…', waiting: 'waiting for answer'} as Record<string, string>,
    sendFailed: 'Could not send',
    errors: {
      'not-paired': (name: string) => `${name} no longer recognizes this pairing, maybe because it was forgotten there. Forget it here too, then pair again.`,
      'quick-refused': (name: string) => `${name} only accepts files from paired devices. Pair with it first.`,
      busy: (name: string) => `${name} is busy. Try again in a moment.`,
      timeout: (name: string) => `${name} did not respond. Check that it is on the same network with Nearby open.`,
    } as Record<string, (name: string) => string>,
    forgetPairing: 'Forget pairing',
    dismiss: 'Dismiss',
    broadcasting: 'Visible to nearby devices now.',
  },
  zh: {
    title: '附近设备',
    subtitle: '与同一网络中的设备互传文件，无需口令。',
    readyTitle: (name: string) => `已准备好接收，设备名“${name}”`,
    readyBody: '保持此页面打开，附近设备即可向你发送文件，保存前都会先询问你。',
    hiddenTitle: '附近设备看不到本机',
    hiddenBody: '其他设备无法发现本机，但你仍可以发送。',
    showMe: '让本机可见',
    rename: '改名',
    devices: '附近的设备',
    searching: '正在查找设备…',
    locale: 'zh-CN',
    lastSeenAgo: (ago: string) => `${ago}在线`,
    neverSeen: '配对后未再出现',
    showMore: (n: number) => `显示其余 ${n} 台`,
    showLess: '收起',
    searchingMore: '正在继续查找设备…',
    searchingMoreHint: '在另一台设备的 Gonc 中打开“附近设备”，它就会出现在这里。',
    lastSeen: (seconds: number) => `${seconds} 秒前在线`,
    notNearby: '暂时不在附近',
    pairedAway: '已配对，暂不在附近',
    searchingHint: '请在另一台设备的 Gonc 中打开“附近设备”，并保持两台设备在同一网络。',
    paired: '已配对',
    notPaired: '未配对',
    send: '发送',
    pair: '配对',
    pairAndSend: '配对并发送',
    quickSendHint: '无需核对数字直接发送。适合发送可以让本网络中任何人看到的文件。',
    pairAndSendHint: '先核对数字，确保只有这台设备能收到文件。',
    quickReceive: '接收未配对设备发送的文件',
    quickReceiveHint: '每次都会先询问你。关闭后只接收已配对设备发送的文件。',
    unverifiedTitle: '未配对',
    unverifiedBody: (name: string) => `本机未与 ${name} 配对，网络中的任何人都可能冒用这个名称。请只接收你预期的文件。`,
    unpaired: '未配对',
    forget: '忘记',
    forgetConfirm: (name: string) => `忘记 ${name}？之后需要重新核对数字才能向它发送。`,
    forgetSure: '再次点击以忘记',
    addFirst: '请先选择要发送的内容，再选择设备。',
    sendHintTitle: '要发送内容？',
    sendHintBody: '选择文件、文件夹或文字，也可以把文件拖放到这里，然后在下方选择设备。',
    chooseFiles: '选择文件',
    fallbackHint: '对方设备没有出现？可以改用口令方式传输，不同网络之间也能使用。',
    useSend: '发送文件',
    useReceive: '接收文件',
    transfers: '传输',
    clearFinished: '清除已结束',
    noTransfers: '发送或接收的文件会显示在这里。',
    cancel: '取消',
    reveal: '在文件夹中显示',
    thisDevice: '本机',
    deviceName: '设备名称',
    save: '保存',
    visible: '允许附近设备发现本机',
    visibleHint: '此页面打开期间，其他设备可以看到此名称。',
    saveTo: '接收文件保存到',
    choose: '选择',
    chooseTitle: '选择接收文件的保存位置',
    storageWarning: '此系统上的配对密钥保存在仅当前用户可读的文件中，而不是系统钥匙串。',
    discoveryWarning: '当前网络无法自动发现设备，请改用“发送文件”和“接收文件”。',
    pairTitleOut: (name: string) => `与 ${name} 配对`,
    pairTitleIn: (name: string) => `${name} 请求配对`,
    pairBody: (name: string) => `请确认 ${name} 显示相同的数字，然后在两台设备上确认。`,
    numbersMatch: '数字一致',
    waitingOther: (name: string) => `等待 ${name} 确认…`,
    pairedNotice: (name: string) => `已与 ${name} 配对。`,
    pairRejected: (name: string) => `与 ${name} 的配对已取消。`,
    pairFailed: (name: string, msg: string) => `无法与 ${name} 配对：${msg}`,
    offerTitle: (name: string) => `${name} 想向你发送文件`,
    offerSummary: (count: number, size: string) => `${count} 个文件 · ${size}`,
    more: (n: number) => `还有 ${n} 项`,
    savingTo: '保存到',
    decline: '拒绝',
    accept: '接收',
    status: {
      connecting: '正在连接…',
      waiting: '等待对方接收…',
      offered: '等待你的确认',
      transferring: '传输中',
      completed: '完成',
      declined: '已拒绝',
      cancelled: '已取消',
      failed: '失败',
    } as Record<string, string>,
    to: '发送给',
    from: '来自',
    connectingTo: '正在连接…',
    cancelling: '正在取消…',
    change: '更改',
    noOverwrite: '不会覆盖已有文件：如有同名文件，新文件会在名称后加上编号保存。',
    activeSend: (status: string) => `发送中 · ${status}`,
    cardStatus: {connecting: '正在连接…', waiting: '等待对方接收'} as Record<string, string>,
    sendFailed: '发送失败',
    errors: {
      'not-paired': (name: string) => `${name} 已不再识别此配对，可能已在那边忘记了本机。请在这里也忘记该配对，然后重新配对。`,
      'quick-refused': (name: string) => `${name} 只接收已配对设备的文件，请先配对。`,
      busy: (name: string) => `${name} 正忙，请稍后再试。`,
      timeout: (name: string) => `${name} 没有响应。请确认它在同一网络中，并已打开附近设备。`,
    } as Record<string, (name: string) => string>,
    forgetPairing: '忘记配对',
    dismiss: '关闭',
    broadcasting: '附近设备现在可以发现本机。',
  },
};

type Strings = typeof strings.en;

const activeStatuses = new Set(['connecting', 'waiting', 'offered', 'transferring']);

function errorText(reason: unknown) {
  return reason instanceof Error ? reason.message : String(reason);
}

export function formatSize(bytes: number) {
  if (!bytes || bytes < 0) return '0 B';
  const units = ['B', 'KB', 'MB', 'GB', 'TB'];
  let value = bytes;
  let unit = 0;
  while (value >= 1024 && unit < units.length - 1) {
    value /= 1024;
    unit++;
  }
  return `${value >= 10 || unit === 0 ? value.toFixed(0) : value.toFixed(1)} ${units[unit]}`;
}

/** A transfer's failure in the app's language, when the Go side named it. */
function failureText(item: nearbyapp.TransferInfo, t: Strings) {
  const word = item.errorCode ? t.errors[item.errorCode] : undefined;
  return word ? word(item.peerName) : item.message || '';
}

function formatCode(code: string) {
  return code.length === 6 ? `${code.slice(0, 3)} ${code.slice(3)}` : code;
}

/** Paired devices that are not nearby, listed before "Show more". */
const AWAY_SHOWN = 3;

/** A time in the past as "3 days ago", "yesterday" or "3 天前" in the app's language. */
function agoText(ms: number, now: number, locale: string) {
  const rtf = new Intl.RelativeTimeFormat(locale, {numeric: 'auto'});
  const minutes = Math.max(1, Math.round((now - ms) / 60000));
  if (minutes < 60) return rtf.format(-minutes, 'minute');
  const hours = Math.round(minutes / 60);
  if (hours < 24) return rtf.format(-hours, 'hour');
  const days = Math.round(hours / 24);
  if (days < 30) return rtf.format(-days, 'day');
  const months = Math.round(days / 30);
  if (months < 12) return rtf.format(-months, 'month');
  return rtf.format(-Math.round(days / 365), 'year');
}

/** A Go time as epoch milliseconds, or 0 for Go's zero time or bad input. */
function timeMs(value: unknown) {
  const ms = Date.parse(String(value ?? ''));
  return Number.isFinite(ms) && ms > 0 ? ms : 0;
}

function deviceIcon(type: string): IconName {
  return type === 'phone' || type === 'tablet' ? 'phone' : 'monitor';
}

/** State and actions shared by the Nearby page and the app-wide dialogs. */
/**
 * How present a device looks, from 1 down to 0.5. A device stays at 1 while
 * it announces itself normally; once the service marks it fading, it eases
 * toward 0.5 at 90% of the time after which it leaves the list.
 */
export function deviceVitality(device: nearby.Device, now: number, fadeMs: number, expiryMs: number) {
  if (device.status !== 'fading') return 1;
  const age = now - Date.parse(String(device.lastSeen));
  if (!Number.isFinite(age) || age <= fadeMs) return 1;
  const end = Math.max(fadeMs + 1, expiryMs * 0.9);
  return 1 - 0.5 * Math.min(1, (age - fadeMs) / (end - fadeMs));
}

/** The current time, updated every second while active is true. */
function useNow(active: boolean) {
  const [now, setNow] = useState(() => Date.now());
  useEffect(() => {
    if (!active) return;
    setNow(Date.now());
    const timer = window.setInterval(() => setNow(Date.now()), 1000);
    return () => window.clearInterval(timer);
  }, [active]);
  return now;
}

export function useNearbyModel(lang: Lang) {
  const t = strings[lang];
  const [state, setState] = useState<nearbyapp.State | null>(null);
  const [transfers, setTransfers] = useState<Record<string, nearbyapp.TransferInfo>>({});
  const [pairing, setPairing] = useState<nearbyapp.PairingInfo | null>(null);
  const [pairingConfirmed, setPairingConfirmed] = useState(false);
  const [notice, setNotice] = useState('');
  const [error, setError] = useState('');
  // Devices with a pair or send request still being set up, so their
  // buttons show progress and cannot start a second attempt.
  const [busyDevices, setBusyDevices] = useState<Record<string, true>>({});
  // Transfers the user asked to cancel; the button shows progress at once.
  const [cancelling, setCancelling] = useState<Record<string, true>>({});
  // A transfer the user just accepted; the Nearby page scrolls it into view.
  const [revealTransfer, setRevealTransfer] = useState<string | null>(null);
  // A device whose send just failed; the Nearby page scrolls its card,
  // which shows the reason, into view.
  const [revealDevice, setRevealDevice] = useState<string | null>(null);
  // Failed sends the user closed on the device card.
  const [dismissedFailures, setDismissedFailures] = useState<Record<string, true>>({});
  // When the user picks an unpaired device to send to, the send starts
  // automatically once pairing succeeds.
  const sendAfterPairing = useRef<{pairingId: string; paths: string[]} | null>(null);
  // Every device seen since the page opened, so a card with a send in
  // progress or a failure stays on screen after the device leaves the list.
  const lastKnown = useRef<Record<string, nearby.Device>>({});
  const remember = useCallback((next: nearbyapp.State) => {
    for (const device of next.devices || []) lastKnown.current[device.id] = device;
    setState(next);
  }, []);

  useEffect(() => {
    NearbyState().then(remember).catch(() => undefined);
    NearbyTransfers()
      .then((list) => setTransfers(Object.fromEntries((list || []).map((item) => [item.id, item]))))
      .catch(() => undefined);
    EventsOn('nearby:state', (next: nearbyapp.State) => remember(next));
    EventsOn('nearby:transfer', (item: nearbyapp.TransferInfo) => {
      setTransfers((current) => {
        if (item.direction === 'send' && item.status === 'failed' && item.deviceId && current[item.id]?.status !== 'failed') {
          setRevealDevice(item.deviceId);
        }
        return {...current, [item.id]: item};
      });
    });
    EventsOn('nearby:pairing', (info: nearbyapp.PairingInfo) => {
      if (info.status === 'waiting') {
        setPairing(info);
        setPairingConfirmed(false);
        return;
      }
      setPairing((current) => (current && current.id === info.id ? null : current));
      const name = info.peerName || '';
      if (info.status === 'paired') {
        setNotice(t.pairedNotice(name));
        const pending = sendAfterPairing.current;
        if (pending && pending.pairingId === info.id && info.peerId) {
          sendAfterPairing.current = null;
          NearbySend(`p-${info.peerId}`, pending.paths).catch((reason) => setError(errorText(reason)));
        }
      } else if (info.status === 'rejected') {
        setNotice(t.pairRejected(name));
      } else {
        setError(t.pairFailed(name, info.error || ''));
      }
      if (sendAfterPairing.current?.pairingId === info.id) sendAfterPairing.current = null;
    });
    return () => {
      EventsOff('nearby:state');
      EventsOff('nearby:transfer');
      EventsOff('nearby:pairing');
    };
    // The strings only change with the language, which is fixed per session.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  useEffect(() => {
    if (!notice) return;
    const timer = window.setTimeout(() => setNotice(''), 5000);
    return () => window.clearTimeout(timer);
  }, [notice]);

  const run = useCallback(async (action: () => Promise<unknown>) => {
    setError('');
    try {
      await action();
    } catch (reason) {
      setError(errorText(reason));
    }
  }, []);

  const withBusy = useCallback(async (deviceId: string, action: () => Promise<unknown>) => {
    setBusyDevices((current) => ({...current, [deviceId]: true}));
    try {
      await run(action);
    } finally {
      setBusyDevices((current) => {
        const next = {...current};
        delete next[deviceId];
        return next;
      });
    }
  }, [run]);

  const sendTo = useCallback(async (device: nearby.Device, paths: string[]) => {
    if (paths.length === 0) {
      setError(t.addFirst);
      return;
    }
    await withBusy(device.id, async () => {
      if (device.paired) {
        await NearbySend(device.id, paths);
        return;
      }
      const info = await NearbyPair(device.id);
      sendAfterPairing.current = {pairingId: info.id, paths};
      setPairing(info);
      setPairingConfirmed(false);
    });
  }, [withBusy, t]);

  const quickSend = useCallback(async (device: nearby.Device, paths: string[]) => {
    if (paths.length === 0) {
      setError(t.addFirst);
      return;
    }
    await withBusy(device.id, () => NearbyQuickSend(device.id, paths));
  }, [withBusy, t]);

  const pairOnly = useCallback(async (device: nearby.Device) => {
    await withBusy(device.id, async () => {
      const info = await NearbyPair(device.id);
      setPairing(info);
      setPairingConfirmed(false);
    });
  }, [withBusy]);

  const cancelTransfer = useCallback(async (id: string) => {
    setCancelling((current) => ({...current, [id]: true}));
    await run(() => NearbyCancel(id));
  }, [run]);

  const answerPairing = useCallback(async (accept: boolean) => {
    if (!pairing) return;
    const id = pairing.id;
    if (!accept) {
      setPairing(null);
      if (sendAfterPairing.current?.pairingId === id) sendAfterPairing.current = null;
    } else {
      setPairingConfirmed(true);
    }
    await run(() => NearbyConfirmPairing(id, accept));
  }, [pairing, run]);

  const transferList = useMemo(
    () => Object.values(transfers).sort((a, b) => String(b.updatedAt).localeCompare(String(a.updatedAt))),
    [transfers],
  );
  const offer = transferList.find((item) => item.direction === 'receive' && item.status === 'offered') || null;
  // The newest unfinished send to each device, shown on its card.
  const activeSends = useMemo(() => {
    const out: Record<string, nearbyapp.TransferInfo> = {};
    for (const item of transferList) {
      if (item.direction === 'send' && item.deviceId && activeStatuses.has(item.status) && !out[item.deviceId]) out[item.deviceId] = item;
    }
    return out;
  }, [transferList]);
  // A device's newest send when it failed and the user has not closed it.
  // A new send to the device replaces it.
  const failedSends = useMemo(() => {
    const newest: Record<string, nearbyapp.TransferInfo> = {};
    for (const item of transferList) {
      if (item.direction === 'send' && item.deviceId && !newest[item.deviceId]) newest[item.deviceId] = item;
    }
    const out: Record<string, nearbyapp.TransferInfo> = {};
    for (const [deviceId, item] of Object.entries(newest)) {
      if (item.status === 'failed' && !dismissedFailures[item.id]) out[deviceId] = item;
    }
    return out;
  }, [transferList, dismissedFailures]);
  const dismissFailure = useCallback((id: string) => {
    setDismissedFailures((current) => ({...current, [id]: true}));
  }, []);

  return {
    t, state, transfers: transferList, pairing, pairingConfirmed, offer, notice, error,
    busyDevices, cancelling, activeSends, revealTransfer, setRevealTransfer,
    failedSends, dismissFailure, revealDevice, setRevealDevice, lastKnown: lastKnown.current,
    setError, setNotice, run, sendTo, quickSend, pairOnly, answerPairing, cancelTransfer,
    clearFinished: async () => {
      await NearbyClearFinished();
      setTransfers((current) => Object.fromEntries(Object.entries(current).filter(([, item]) => activeStatuses.has(item.status))));
    },
  };
}

export type NearbyModel = ReturnType<typeof useNearbyModel>;

/** Pairing and incoming-file dialogs, shown on every page. */
/** onAccepted opens the Nearby page, so the accepted transfer can be shown. */
export function NearbyDialogs({model, onAccepted}: {model: NearbyModel; onAccepted?: () => void}) {
  const {t, pairing, pairingConfirmed, offer, state} = model;
  const primaryRef = useRef<HTMLButtonElement>(null);

  useEffect(() => {
    primaryRef.current?.focus();
  }, [pairing?.id, offer?.id]);

  if (pairing) {
    const name = pairing.peerName || '…';
    return (
      <div className="qr-backdrop nearby-backdrop">
        <section className="nearby-dialog" role="dialog" aria-modal="true" aria-labelledby="nearby-pair-title">
          <h2 id="nearby-pair-title">{pairing.incoming ? t.pairTitleIn(name) : t.pairTitleOut(name)}</h2>
          <div className="nearby-code" aria-label={pairing.code}>{formatCode(pairing.code)}</div>
          <p>{pairingConfirmed ? t.waitingOther(name) : t.pairBody(name)}</p>
          <div className="nearby-dialog-actions">
            <button type="button" className="ghost" onClick={() => model.answerPairing(false)}>{t.cancel}</button>
            <button ref={primaryRef} type="button" className="primary" disabled={pairingConfirmed} onClick={() => model.answerPairing(true)}>
              <Icon name="check" />{t.numbersMatch}
            </button>
          </div>
        </section>
      </div>
    );
  }

  if (offer) {
    const items = offer.items || [];
    const shown = items.slice(0, 6);
    return (
      <div className="qr-backdrop nearby-backdrop">
        <section className="nearby-dialog" role="dialog" aria-modal="true" aria-labelledby="nearby-offer-title">
          <h2 id="nearby-offer-title">{t.offerTitle(offer.peerName)}</h2>
          {offer.quick && (
            <div className="nearby-unverified" role="note">
              <Icon name="info" />
              <span><strong>{t.unverifiedTitle}.</strong> {t.unverifiedBody(offer.peerName)}</span>
            </div>
          )}
          <p className="nearby-offer-summary">{t.offerSummary(offer.fileCount, formatSize(offer.totalBytes))}</p>
          <ul className="nearby-offer-items">
            {shown.map((item) => (
              <li key={item.name}>
                <Icon name={item.isDir ? 'folder' : 'file'} />
                <span title={item.name}>{item.name}</span>
                <em>{formatSize(item.size)}</em>
              </li>
            ))}
            {items.length > shown.length && <li className="more">{t.more(items.length - shown.length)}</li>}
          </ul>
          <div className="nearby-save-to">
            <span>{t.savingTo}</span>
            <code title={state?.saveDir || offer.saveDir}>{state?.saveDir || offer.saveDir}</code>
            <button type="button" className="ghost" onClick={() => model.run(() => NearbyChooseSaveDir(t.chooseTitle))}><Icon name="folder" />{t.change}</button>
          </div>
          <p className="nearby-hint">{t.noOverwrite}</p>
          <div className="nearby-dialog-actions">
            <button type="button" className="ghost" onClick={() => model.run(() => NearbyDecline(offer.id))}>{t.decline}</button>
            <button ref={primaryRef} type="button" className="primary" onClick={() => {
              model.setRevealTransfer(offer.id);
              onAccepted?.();
              model.run(() => NearbyAccept(offer.id));
            }}>
              <Icon name="receive" />{t.accept}
            </button>
          </div>
        </section>
      </div>
    );
  }
  return null;
}

type PageProps = {
  model: NearbyModel;
  /** Opens the picker for files, folders, text or clipboard content. */
  onAddContent: () => void;
  /** Opens the passphrase Send Files or Receive Files page. */
  onOpenMode: (mode: 'send' | 'receive') => void;
  shareCard: ReactNode;
  sharePaths: string[];
  onReveal: (path: string) => void;
};

export function NearbyPage({model, onAddContent, onOpenMode, shareCard, sharePaths, onReveal}: PageProps) {
  const {t, state, transfers, notice, error} = model;
  const [nameDraft, setNameDraft] = useState<string | null>(null);
  // Forgetting asks for a second click; WebView hosts do not all support window.confirm.
  const [confirmForget, setConfirmForget] = useState<string | null>(null);
  const devices = useMemo(() => state?.devices || [], [state]);
  const hasFinished = transfers.some((item) => !activeStatuses.has(item.status));
  const {revealTransfer, setRevealTransfer} = model;
  const discovering = !!state?.discovering;
  const fadeMs = state?.deviceFadeMs || 7000;
  const expiryMs = state?.deviceExpiryMs || 16000;

  // Cards with a send, a connection attempt or a failure stay on screen even
  // when their device leaves the list; they show it is not nearby.
  const presentIds = useMemo(() => new Set(devices.map((device) => device.id)), [devices]);
  const pinnedIds = useMemo(
    () => new Set([...Object.keys(model.activeSends), ...Object.keys(model.busyDevices), ...Object.keys(model.failedSends)]),
    [model.activeSends, model.busyDevices, model.failedSends],
  );
  const awayCards = [...pinnedIds]
    .filter((id) => !presentIds.has(id))
    .map((id) => model.lastKnown[id])
    .filter((device): device is nearby.Device => !!device);

  // An unpaired device that leaves fades out instead of vanishing. A paired
  // one moves to "Paired, not nearby" below.
  const [leaving, setLeaving] = useState<Record<string, nearby.Device>>({});
  const previousDevices = useRef<nearby.Device[]>([]);
  useEffect(() => {
    // A device that rotated its tag is listed again under a new ID with the
    // same name; it has not left.
    const names = new Set(devices.map((device) => device.nickname));
    const gone = previousDevices.current.filter((device) => !presentIds.has(device.id) && !device.paired
      && !pinnedIds.has(device.id) && !names.has(device.nickname));
    previousDevices.current = devices;
    if (gone.length === 0) return;
    setLeaving((current) => ({...current, ...Object.fromEntries(gone.map((device) => [device.id, device]))}));
    window.setTimeout(() => setLeaving((current) => {
      const next = {...current};
      for (const device of gone) delete next[device.id];
      return next;
    }), 400);
  }, [devices, presentIds, pinnedIds]);
  const leavingCards = Object.values(leaving).filter((device) => !presentIds.has(device.id) && !pinnedIds.has(device.id));

  const cards = [
    ...devices.map((device) => ({device, away: false, leaving: false})),
    ...awayCards.map((device) => ({device, away: true, leaving: false})),
    ...leavingCards.map((device) => ({device, away: false, leaving: true})),
  ];
  // Paired devices that are not nearby, most recently seen first. Pairings
  // never seen since are ordered by when they were paired.
  const [showAllAway, setShowAllAway] = useState(false);
  const awayPeers = (state?.peers || [])
    .filter((peer) => {
      const id = `p-${peer.peerId}`;
      return !presentIds.has(id) && !pinnedIds.has(id) && !devices.some((device) => device.peerId === peer.peerId);
    })
    .map((peer) => ({peer, seen: timeMs(peer.lastSeen)}))
    .sort((a, b) => (b.seen || timeMs(b.peer.pairedAt)) - (a.seen || timeMs(a.peer.pairedAt)));
  const awayShown = showAllAway ? awayPeers : awayPeers.slice(0, AWAY_SHOWN);
  const now = useNow(devices.some((device) => device.status === 'fading'));

  // Coming back to the window is when a refresh helps: it announces this
  // device at once and joins the network again if it changed.
  useEffect(() => {
    let last = 0;
    const refresh = () => {
      if (document.visibilityState !== 'visible' || Date.now() - last < 3000) return;
      last = Date.now();
      NearbyRefresh().catch(() => undefined);
    };
    window.addEventListener('focus', refresh);
    document.addEventListener('visibilitychange', refresh);
    return () => {
      window.removeEventListener('focus', refresh);
      document.removeEventListener('visibilitychange', refresh);
    };
  }, []);

  // After the user accepts files, make sure the transfer is on screen. The
  // row may render a moment later, so fall back to the Transfers card.
  useEffect(() => {
    if (!revealTransfer) return;
    const frame = window.requestAnimationFrame(() => {
      const target = document.getElementById(`nearby-transfer-${revealTransfer}`)
        || document.querySelector('.nearby-transfers-card');
      target?.scrollIntoView({behavior: 'smooth', block: 'nearest'});
      setRevealTransfer(null);
    });
    return () => window.cancelAnimationFrame(frame);
  }, [revealTransfer, setRevealTransfer]);

  // A failed send shows its reason on the device's card; bring it on screen.
  const {revealDevice, setRevealDevice} = model;
  useEffect(() => {
    if (!revealDevice) return;
    const frame = window.requestAnimationFrame(() => {
      document.getElementById(`nearby-device-${revealDevice}`)?.scrollIntoView({behavior: 'smooth', block: 'nearest'});
      setRevealDevice(null);
    });
    return () => window.cancelAnimationFrame(frame);
  }, [revealDevice, setRevealDevice]);

  async function forgetBroken(device: nearby.Device, failure: nearbyapp.TransferInfo) {
    if (!device.peerId) return;
    await model.run(() => NearbyForget(device.peerId!));
    model.dismissFailure(failure.id);
  }

  function focusName() {
    const input = document.getElementById('nearby-name') as HTMLInputElement | null;
    if (!input) return;
    input.scrollIntoView({behavior: 'smooth', block: 'center'});
    input.focus({preventScroll: true});
    input.select();
  }

  async function forget(device: nearby.Device) {
    if (!device.peerId) return;
    await forgetPeer(device.id, device.peerId);
  }

  async function forgetPeer(key: string, peerId: string) {
    if (confirmForget !== key) {
      setConfirmForget(key);
      return;
    }
    setConfirmForget(null);
    await model.run(() => NearbyForget(peerId));
  }

  return (
    <>
      {state?.error && <div className="alert" role="alert">{state.error}</div>}
      {error && <div className="alert" role="alert">{error}</div>}
      {notice && <div className="nearby-notice" role="status"><Icon name="check" />{notice}</div>}

      {state && !state.error && (
        <section className={`nearby-ready${state.visible ? '' : ' hidden'}${state.visible && discovering ? ' broadcasting' : ''}`} aria-live="polite">
          <span className="nearby-ready-icon" title={state.visible && discovering ? t.broadcasting : undefined}>
            <Icon name={state.visible ? 'receive' : 'eyeOff'} />
          </span>
          <div className="nearby-ready-text">
            <strong>{state.visible ? t.readyTitle(state.nickname) : t.hiddenTitle}</strong>
            <small>{state.visible ? t.readyBody : t.hiddenBody}</small>
          </div>
          {state.visible ? (
            <button type="button" className="ghost" onClick={focusName}>{t.rename}</button>
          ) : (
            <button type="button" className="secondary" onClick={() => model.run(() => NearbySetVisible(true))}>{t.showMe}</button>
          )}
        </section>
      )}

      {/* The content card appears once something is added; until then a
          short prompt keeps the page about devices, not about sending. */}
      {sharePaths.length > 0 ? shareCard : (
        <section className="card nearby-send-hint">
          <span className="nearby-send-hint-icon"><Icon name="send" /></span>
          <div className="nearby-send-hint-text">
            <strong>{t.sendHintTitle}</strong>
            <small>{t.sendHintBody}</small>
          </div>
          <button type="button" className="secondary" onClick={onAddContent}><Icon name="plus" />{t.chooseFiles}</button>
        </section>
      )}

      <section className="card nearby-devices-card">
        <div className="card-head">
          <h2>{t.devices}</h2>
        </div>
        <div className="card-body">
          {state?.discoveryError && <p className="nearby-warning">{t.discoveryWarning}</p>}
          {cards.length === 0 ? (
            <div className="empty-state compact nearby-searching">
              <span className="nearby-pulse" aria-hidden="true"><Icon name="nearby" /></span>
              <strong>{t.searching}</strong>
              <p>{t.searchingHint}</p>
            </div>
          ) : (
            <div className="nearby-device-grid">
              {cards.map(({device, away, leaving: isLeaving}) => {
                const busy = !!model.busyDevices[device.id];
                const sending = model.activeSends[device.id];
                const sendPercent = sending && sending.totalBytes > 0 ? Math.min(100, Math.round((sending.doneBytes / sending.totalBytes) * 100)) : 0;
                const failure = sending ? undefined : model.failedSends[device.id];
                const fading = !away && !isLeaving && device.status === 'fading';
                // A card the user is working with stays fully visible.
                const vitality = sending || busy || failure ? 1 : deviceVitality(device, now, fadeMs, expiryMs);
                const silentFor = fading ? Math.max(1, Math.round((now - Date.parse(String(device.lastSeen))) / 1000)) : 0;
                const presence = away || isLeaving ? 'away' : fading ? 'fading' : 'online';
                return (
                <div
                  key={device.id}
                  id={`nearby-device-${device.id}`}
                  className={`nearby-device${device.paired ? ' paired' : ''}${sending ? ' sending' : ''}${failure ? ' failed' : ''}${away ? ' away' : ''}${isLeaving ? ' leaving' : ''}`}
                  style={vitality < 1 ? {opacity: vitality} : undefined}
                >
                  <span className="nearby-device-icon"><Icon name={deviceIcon(device.deviceType)} /></span>
                  <div className="nearby-device-text">
                    <strong title={device.nickname}>
                      <span className={`nearby-presence ${presence}`} aria-hidden="true" />
                      {device.nickname || device.addresses?.[0]}
                    </strong>
                    {sending ? (
                      <span className="active" role="status">
                        {t.activeSend(sending.status === 'transferring' ? `${sendPercent}%` : (t.cardStatus[sending.status] || t.status[sending.status] || sending.status))}
                      </span>
                    ) : away ? (
                      <span>{t.notNearby}</span>
                    ) : fading ? (
                      <span>{t.lastSeen(silentFor)}</span>
                    ) : (
                      <span className={device.paired ? 'ok' : ''}>{device.paired ? t.paired : t.notPaired}</span>
                    )}
                    {sending?.status === 'transferring' && <div className="nearby-progress" aria-hidden="true"><span style={{width: `${sendPercent}%`}} /></div>}
                  </div>
                  <div className="nearby-device-actions">
                    {device.paired && !sending && !away && (
                      <button
                        type="button"
                        className={`ghost nearby-forget${confirmForget === device.id ? ' confirming' : ''}`}
                        onClick={() => forget(device)}
                        onBlur={() => setConfirmForget((current) => (current === device.id ? null : current))}
                        title={confirmForget === device.id ? t.forgetConfirm(device.nickname) : t.forget}
                      >
                        {confirmForget === device.id ? t.forgetSure : t.forget}
                      </button>
                    )}
                    {sending ? (
                      <button type="button" className="ghost" disabled={!!model.cancelling[sending.id]} onClick={() => model.cancelTransfer(sending.id)}>
                        {model.cancelling[sending.id] ? t.cancelling : t.cancel}
                      </button>
                    ) : away || isLeaving ? null : busy ? (
                      <button type="button" className="secondary" disabled aria-busy="true">
                        <span className="spinner-dot" aria-hidden="true" />{t.connectingTo}
                      </button>
                    ) : !device.paired && sharePaths.length === 0 ? (
                      <button type="button" className="secondary" onClick={() => model.pairOnly(device)}>
                        <Icon name="link" />{t.pair}
                      </button>
                    ) : !device.paired && device.quickReceive ? (
                      <>
                        <button type="button" className="ghost" title={t.pairAndSendHint} onClick={() => model.sendTo(device, sharePaths)}>
                          <Icon name="link" />{t.pairAndSend}
                        </button>
                        <button type="button" className="primary" title={t.quickSendHint} onClick={() => model.quickSend(device, sharePaths)}>
                          <Icon name="send" />{t.send}
                        </button>
                      </>
                    ) : (
                      <button type="button" className="primary" disabled={sharePaths.length === 0} title={sharePaths.length === 0 ? t.addFirst : undefined} onClick={() => model.sendTo(device, sharePaths)}>
                        <Icon name="send" />{device.paired ? t.send : t.pairAndSend}
                      </button>
                    )}
                  </div>
                  {failure && (
                    <div className="nearby-device-failure" role="alert">
                      <Icon name="info" />
                      <p><strong>{t.sendFailed}</strong> {failureText(failure, t)}</p>
                      <div className="nearby-device-failure-actions">
                        {failure.errorCode === 'not-paired' && device.paired && device.peerId && (
                          <button type="button" className="secondary" onClick={() => forgetBroken(device, failure)}>{t.forgetPairing}</button>
                        )}
                        <button type="button" className="ghost" onClick={() => model.dismissFailure(failure.id)}>{t.dismiss}</button>
                      </div>
                    </div>
                  )}
                </div>
                );
              })}
              {discovering && (
                <div className="nearby-device nearby-device-placeholder" aria-live="off">
                  <span className="nearby-pulse small" aria-hidden="true"><Icon name="nearby" /></span>
                  <div className="nearby-device-text">
                    <strong>{t.searchingMore}</strong>
                    <span>{t.searchingMoreHint}</span>
                  </div>
                </div>
              )}
            </div>
          )}
          {awayPeers.length > 0 && (
            <div className="nearby-away">
              <h3>{t.pairedAway}</h3>
              <ul>
                {awayShown.map(({peer, seen}) => {
                  const key = `peer-${peer.peerId}`;
                  return (
                    <li key={peer.peerId}>
                      <span className="nearby-device-icon"><Icon name={deviceIcon(peer.deviceType)} /></span>
                      <span className="nearby-away-name" title={peer.nickname}>{peer.nickname}</span>
                      <span className="nearby-away-status" title={seen ? new Date(seen).toLocaleString(t.locale) : undefined}>
                        {seen ? t.lastSeenAgo(agoText(seen, Date.now(), t.locale)) : t.neverSeen}
                      </span>
                      <button
                        type="button"
                        className={`ghost nearby-forget${confirmForget === key ? ' confirming' : ''}`}
                        onClick={() => forgetPeer(key, peer.peerId)}
                        onBlur={() => setConfirmForget((current) => (current === key ? null : current))}
                        title={confirmForget === key ? t.forgetConfirm(peer.nickname) : t.forget}
                      >
                        {confirmForget === key ? t.forgetSure : t.forget}
                      </button>
                    </li>
                  );
                })}
              </ul>
              {awayPeers.length > AWAY_SHOWN && (
                <button type="button" className="quiet-action nearby-away-more" aria-expanded={showAllAway} onClick={() => setShowAllAway((v) => !v)}>
                  {showAllAway ? t.showLess : t.showMore(awayPeers.length - AWAY_SHOWN)}
                </button>
              )}
            </div>
          )}
          <div className="nearby-fallback">
            <p>{t.fallbackHint}</p>
            <div className="nearby-fallback-actions">
              <button type="button" className="quiet-action" onClick={() => onOpenMode('send')}><Icon name="send" />{t.useSend}</button>
              <button type="button" className="quiet-action" onClick={() => onOpenMode('receive')}><Icon name="receive" />{t.useReceive}</button>
            </div>
          </div>
        </div>
      </section>

      <section className="card nearby-transfers-card">
        <div className="card-head">
          <h2>{t.transfers}</h2>
          <div className="card-actions">
            {hasFinished && <button type="button" className="ghost" onClick={model.clearFinished}><Icon name="trash" />{t.clearFinished}</button>}
          </div>
        </div>
        <div className="card-body">
          {transfers.length === 0 ? (
            <p className="nearby-muted">{t.noTransfers}</p>
          ) : (
            <ul className="nearby-transfer-list">
              {transfers.map((item) => <TransferRow key={item.id} item={item} t={t} cancelling={!!model.cancelling[item.id]} onCancel={() => model.cancelTransfer(item.id)} onReveal={onReveal} />)}
            </ul>
          )}
        </div>
      </section>

      <section className="card nearby-self-card">
        <div className="card-head"><h2>{t.thisDevice}</h2></div>
        <div className="card-body nearby-settings">
          <form className="field" onSubmit={(event) => {
            event.preventDefault();
            if (nameDraft !== null) model.run(async () => { await NearbySetNickname(nameDraft); setNameDraft(null); });
          }}>
            <label htmlFor="nearby-name">{t.deviceName}</label>
            <div className="input-group">
              <input id="nearby-name" value={nameDraft ?? state?.nickname ?? ''} onChange={(event) => setNameDraft(event.target.value)} maxLength={64} />
              {nameDraft !== null && nameDraft !== state?.nickname && <button type="submit" className="secondary">{t.save}</button>}
            </div>
          </form>
          <label className="nearby-toggle">
            <input type="checkbox" checked={!!state?.visible} onChange={(event) => model.run(() => NearbySetVisible(event.target.checked))} />
            <span>
              <strong>{t.visible}</strong>
              <small>{t.visibleHint}</small>
            </span>
          </label>
          <label className="nearby-toggle">
            <input type="checkbox" checked={!!state?.quickReceive} onChange={(event) => model.run(() => NearbySetQuickReceive(event.target.checked))} />
            <span>
              <strong>{t.quickReceive}</strong>
              <small>{t.quickReceiveHint}</small>
            </span>
          </label>
          <div className="field">
            <label>{t.saveTo}</label>
            <div className="input-group">
              <input value={state?.saveDir || ''} readOnly title={state?.saveDir} />
              <button type="button" className="icon-button" onClick={() => model.run(() => NearbyChooseSaveDir(t.chooseTitle))} aria-label={t.choose} title={t.choose}><Icon name="folder" /></button>
              <button type="button" className="icon-button" onClick={() => state?.saveDir && onReveal(state.saveDir)} aria-label={t.reveal} title={t.reveal}><Icon name="folderOpen" /></button>
            </div>
          </div>
          {state && !state.storageProtected && <p className="nearby-warning">{t.storageWarning}</p>}
        </div>
      </section>
    </>
  );
}

function TransferRow({item, t, cancelling, onCancel, onReveal}: {item: nearbyapp.TransferInfo; t: Strings; cancelling: boolean; onCancel: () => void; onReveal: (path: string) => void}) {
  const active = activeStatuses.has(item.status);
  const percent = item.totalBytes > 0 ? Math.min(100, Math.round((item.doneBytes / item.totalBytes) * 100)) : (item.status === 'completed' ? 100 : 0);
  const names = (item.items || []).map((entry) => entry.name);
  const label = names.length > 2 ? `${names.slice(0, 2).join(', ')} ${t.more(names.length - 2)}` : names.join(', ');
  const tone = item.status === 'completed' ? 'ok' : item.status === 'failed' ? 'error' : item.status === 'declined' || item.status === 'cancelled' ? 'muted' : 'active';
  return (
    <li id={`nearby-transfer-${item.id}`} className={`nearby-transfer ${tone}`}>
      <span className="nearby-transfer-icon"><Icon name={item.direction === 'send' ? 'send' : 'receive'} /></span>
      <div className="nearby-transfer-main">
        <div className="nearby-transfer-top">
          <strong>{item.direction === 'send' ? t.to : t.from} {item.peerName}{item.quick && <span className="nearby-unpaired-tag"> · {t.unpaired}</span>}</strong>
          <span className="nearby-transfer-status">
            {t.status[item.status] || item.status}
            {item.status === 'transferring' && ` · ${percent}%`}
            {item.status === 'transferring' && item.bytesPerSecond > 0 && ` · ${formatSize(item.bytesPerSecond)}/s`}
          </span>
        </div>
        <div className="nearby-transfer-detail" title={names.join('\n')}>
          {label} · {formatSize(item.totalBytes)}
          {item.message && <em> — {item.status === 'failed' ? failureText(item, t) : item.message}</em>}
        </div>
        {(item.status === 'transferring' || item.status === 'completed') && (
          <div className="nearby-progress" aria-hidden="true"><span style={{width: `${percent}%`}} /></div>
        )}
      </div>
      <div className="nearby-transfer-actions">
        {active && item.status !== 'offered' && <button type="button" className="ghost" disabled={cancelling} onClick={onCancel}>{cancelling ? t.cancelling : t.cancel}</button>}
        {item.status === 'completed' && item.direction === 'receive' && item.savedPaths && item.savedPaths.length > 0 && (
          <button type="button" className="icon-button" onClick={() => onReveal(item.savedPaths![0])} aria-label={t.reveal} title={t.reveal}><Icon name="folderOpen" /></button>
        )}
      </div>
    </li>
  );
}

export function nearbyText(lang: Lang) {
  return strings[lang];
}
