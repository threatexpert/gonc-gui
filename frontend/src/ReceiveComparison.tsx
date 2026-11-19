import {useEffect, useRef, useState} from 'react';
import {CancelReceivedComparison, DeleteReceivedComparison, ReceiveReceivedComparison, ReceivedComparisonPage, SelectReceivedComparison, StartReceivedComparison} from '../wailsjs/go/main/App';
import {ProfileDeleteDialog} from './ProfileDeleteDialog';
import './ReceiveComparison.css';
import {comparisonTimes} from './receiveComparisonFormat';

type Purpose = 'localOnly' | 'localMissing' | 'differences';
type Snapshot = Omit<Awaited<ReturnType<typeof ReceivedComparisonPage>>, 'convertValues'>;
type Item = Snapshot['items'][number];
type ActiveReceive = Snapshot['activeReceives'][number];
type Props = {saveDir: string; lang: 'zh' | 'en'; onClose: () => void; onChanged: () => void};

function bytes(n: number) {
  if (n < 1024) return `${n} B`;
  const i = Math.min(Math.floor(Math.log(n) / Math.log(1024)), 4);
  return `${(n / 1024 ** i).toFixed(1)} ${['B', 'KB', 'MB', 'GB', 'TB'][i]}`;
}

function ComparisonValue({local, remote, equal, different, zh}: {
  local?: string; remote?: string; equal: boolean; different: boolean; zh: boolean;
}) {
  if (equal) return <span className="compare-equal" title={zh ? '本地与对方相同' : 'Local and sender match'}>✓ {local}</span>;
  return <>
    {local != null && <div className={different ? 'compare-different' : undefined}><span className="compare-side">{zh ? '本地' : 'Local'}</span>{local}</div>}
    {remote != null && <div><span className="compare-side">{zh ? '对方' : 'Sender'}</span>{remote}</div>}
  </>;
}

function transferStatus(file: ActiveReceive, zh: boolean) {
  if (file.checking) return zh ? '复核中' : 'Checking';
  const label = zh ? '接收中' : 'Receiving';
  if (file.totalBytes <= 0) return label;
  return `${label} ${Math.min(100, Math.floor(file.doneBytes * 100 / file.totalBytes))}%`;
}

export function ReceiveComparison({saveDir, lang, onClose, onChanged}: Props) {
  const zh = lang === 'zh';
  function explain(message: string) {
    if (!zh) return message;
    const translations: Record<string, string> = {
      'comparison index too large; compare a smaller subfolder': '文件数量超过本次比对的内存预算，请填写较小的子目录分批比对。',
      'deletion cancelled; completed actions are preserved': '删除已取消；已完成的操作保留，未处理的文件仍保持选中。',
      'receiving cancelled; completed actions are preserved': '接收已取消；已完成的文件保留，失败项可重新勾选后续传。',
      'local file appeared since comparison': '保存位置已有该文件，已保留，未覆盖。',
      'remote file size is missing or invalid': '对方清单缺少有效文件大小，无法完成本次筛查。',
      'path exists on the sender': '对端仍存在该路径，已跳过。',
      'local file changed since comparison': '本地文件在比对后发生变化，已跳过。',
      'save directory changed': '接收目录在比对后发生变化。',
      'linked path skipped': '路径已变为链接，已跳过。',
      'comparison expired; compare again': '比对结果已失效，请重新比对。',
      'sender connection changed; compare again': '对端连接已变化，请重新比对。',
      'connect to the sender first': '请先连接对端。',
      'wait for the current download or comparison task': '请等待当前下载或比对任务结束。',
      'comparison path must stay within the save directory': '比对范围必须是接收目录中的相对目录。',
    };
    if (message.endsWith(': link target unavailable')) return message.slice(0, -'link target unavailable'.length) + '链接目标不可用';
    return translations[message] || message;
  }
  const [id, setId] = useState('');
  const [purpose, setPurpose] = useState<Purpose | null>(null);
  const [scope, setScope] = useState('/');
  const [data, setData] = useState<Snapshot | null>(null);
  const [input, setInput] = useState('');
  const [query, setQuery] = useState('');
  const [page, setPage] = useState(0);
  const [viewQuery, setViewQuery] = useState('');
  const [revision, setRevision] = useState(0);
  const [pending, setPending] = useState(false);
  const [error, setError] = useState('');
  const [mode, setMode] = useState('trash');
  const [confirm, setConfirm] = useState<'delete' | 'receive' | null>(null);
  const panel = useRef<HTMLElement>(null);
  const deleteButton = useRef<HTMLButtonElement>(null);
  const receiveButton = useRef<HTMLButtonElement>(null);
  const idRef = useRef('');
  const mounted = useRef(true);
  const request = useRef(0);
  const actionPending = useRef(false);
  const lastChange = useRef('');
  const changed = useRef(onChanged);
  changed.current = onChanged;
  const active = !!data && ['remote', 'local', 'deleting', 'receiving'].includes(data.phase);
  const busy = pending || active;
  const ready = !!data && ['ready', 'done'].includes(data.phase);
  const locked = busy || input !== query || viewQuery !== query || (!!data && data.page !== page);

  useEffect(() => {
    mounted.current = true;
    const previous = document.activeElement as HTMLElement | null;
    panel.current?.focus();
    return () => {
      mounted.current = false;
      request.current++;
      if (idRef.current) CancelReceivedComparison(idRef.current, true).catch(() => undefined);
      if (previous?.isConnected) previous.focus();
    };
  }, []);

  useEffect(() => {
    const timer = window.setTimeout(() => {setQuery(input); setPage(0);}, 200);
    return () => clearTimeout(timer);
  }, [input]);

  useEffect(() => {
    if (!id) return;
    let stopped = false;
    let timer: number | undefined;
    const version = ++request.current;
    async function poll() {
      try {
        const value: Snapshot = await ReceivedComparisonPage(id, query, page);
        if (stopped || version !== request.current) return;
        setData(value);
        setViewQuery(query);
        setPage(value.page);
        if (['remote', 'local', 'deleting', 'receiving'].includes(value.phase)) timer = window.setTimeout(poll, 500);
        if (value.phase === 'done') {
          const signature = `${id}:${value.deleted}:${value.received}:${value.skipped}:${value.failed}`;
          if (lastChange.current !== signature) {lastChange.current = signature; changed.current();}
        }
      } catch (err) {
        if (!stopped && version === request.current) setError(String(err));
      }
    }
    void poll();
    return () => {stopped = true; clearTimeout(timer);};
  }, [id, query, page, revision]);

  async function start(nextPurpose: Purpose) {
    if (busy || actionPending.current || !saveDir) return;
    actionPending.current = true;
    request.current++;
    const oldID = idRef.current; idRef.current = '';
    setPending(true); setPurpose(nextPurpose); setId(''); setData(null); setConfirm(null);
    setError(''); setInput(''); setQuery(''); setViewQuery(''); setPage(0);
    try {
      if (oldID) {
        try {await CancelReceivedComparison(oldID, true);}
        catch { /* A new connection may already have invalidated the old result. */ }
      }
      if (!mounted.current) return;
      const next = await StartReceivedComparison(saveDir, scope, nextPurpose);
      idRef.current = next;
      if (!mounted.current) {await CancelReceivedComparison(next, true); return;}
      const value: Snapshot = await ReceivedComparisonPage(next, '', 0);
      if (!mounted.current) return;
      setData(value); setViewQuery(''); setMode(value.deleteMode); setId(next);
    } catch (err) {if (mounted.current) setError(String(err));}
    finally {actionPending.current = false; if (mounted.current) setPending(false);}
  }

  async function select(ids: number[], all: boolean, selected: boolean, filter = query) {
    if (locked || actionPending.current) return;
    actionPending.current = true;
    setPending(true); setError('');
    try {
      await SelectReceivedComparison(id, filter, ids, all, selected);
      const value: Snapshot = await ReceivedComparisonPage(id, query, page);
      if (mounted.current) setData(value);
    } catch (err) {if (mounted.current) setError(String(err));}
    finally {actionPending.current = false; if (mounted.current) setPending(false);}
  }

  async function remove() {
    if (confirm === 'delete') await DeleteReceivedComparison(id, mode);
    else await ReceiveReceivedComparison(id);
    if (!mounted.current) return;
    setConfirm(null);
    setData(current => current ? {...current, phase: confirm === 'delete' ? 'deleting' : 'receiving', error: ''} : current);
    setRevision(value => value + 1);
  }

  const purposes: Record<Purpose, string> = zh
    ? {localOnly: '本地多出', localMissing: '本地缺少', differences: '有差异'}
    : {localOnly: 'Extra on local', localMissing: 'Missing on local', differences: 'Differences'};
  const hints: Record<Purpose, string> = zh
    ? {localOnly: '仅本地存在，可勾选清理。', localMissing: '仅对方存在，可勾选接收。', differences: '对比文件有无、大小、修改时间和链接目标。'}
    : {localOnly: 'Find local files you can remove.', localMissing: 'Find missing files to receive.', differences: 'Compare presence, size, modification time and link targets.'};
  const phase = data?.phase ?? '';
  const phaseText: Record<string, string> = zh
    ? {remote: '正在读取对方列表…', local: '正在筛查本地文件…', ready: '筛查完成', done: '处理结束', error: '筛查未完成，无法操作', cancelled: '筛查已取消', deleting: '正在复核并删除…', receiving: '正在接收所选文件…'}
    : {remote: 'Reading sender list…', local: 'Checking local files…', ready: 'Search complete', done: 'Processing finished', error: 'Search incomplete; actions disabled', cancelled: 'Search cancelled', deleting: 'Verifying and deleting…', receiving: 'Receiving selected files…'};
  const outcomes: Record<string, string> = zh ? {deleted: '已删除', received: '已接收', skipped: '已跳过', failed: '失败'} : {deleted: 'Deleted', received: 'Received', skipped: 'Skipped', failed: 'Failed'};
  const deleteAction = mode === 'trash' ? (zh ? '移入回收站' : 'Move to Trash') : (zh ? '永久删除' : 'Permanently delete');
  const receiveAction = purpose === 'localMissing' ? (zh ? '接收所选' : 'Receive selected') : (zh ? '接收 / 修复所选' : 'Receive / repair selected');
  const deleting = confirm === 'delete';
  const action = deleting ? deleteAction : receiveAction;
  const actionCount = deleting ? data?.selectedLocalOnly ?? 0 : data?.selectedReceive ?? 0;
  const actionBytes = deleting ? data?.selectedLocalOnlyBytes ?? 0 : data?.selectedReceiveBytes ?? 0;
  const canDelete = purpose === 'localOnly' || purpose === 'differences';
  const transfers = data?.activeReceives ?? [];
  const hasList = ready || phase === 'deleting' || phase === 'receiving';
  const unknown = zh ? '未知' : 'Unknown';
  const selectable = (item: Item) => !['deleted', 'received', 'skipped'].includes(item.outcome);

  return <>
    <div className="qr-backdrop receive-compare-backdrop">
      <section ref={panel} className={`receive-compare${hasList ? ' has-list' : ''}`} role="dialog" aria-modal="true" aria-labelledby="receive-compare-title" tabIndex={-1} onKeyDown={event => {
        if (confirm) return;
        if (event.key === 'Escape' && !busy) {event.preventDefault(); onClose();}
        if (event.key === 'Tab') {
          const elements = Array.from(event.currentTarget.querySelectorAll<HTMLElement>('button:not(:disabled), input:not(:disabled), select:not(:disabled), summary'));
          const first = elements[0], last = elements[elements.length - 1];
          if (event.shiftKey && (document.activeElement === first || document.activeElement === panel.current)) {event.preventDefault(); last?.focus();}
          else if (!event.shiftKey && document.activeElement === last) {event.preventDefault(); first?.focus();}
        }
      }}>
        <header className="receive-compare-head">
          <h2 id="receive-compare-title">{zh ? '比对目录' : 'Compare folder'}</h2>
          <span className="receive-compare-root" title={saveDir}>{saveDir}</span>
          <button type="button" disabled={busy} onClick={onClose}>{zh ? '关闭' : 'Close'}</button>
        </header>
        <div className="receive-compare-controls">
          <label>{zh ? '比对范围' : 'Subfolder'}<input value={scope} disabled={busy} onChange={event => setScope(event.target.value)} placeholder="/" title={zh ? '/ 表示整个接收目录' : '/ covers the entire receive folder'} /></label>
          <div className="receive-compare-purposes" role="group" aria-label={zh ? '开始比对' : 'Start comparison'}>
            {(Object.keys(purposes) as Purpose[]).map(value => <button key={value} type="button" title={hints[value]} aria-pressed={purpose === value} disabled={busy || !saveDir} onClick={() => start(value)}>{purposes[value]}</button>)}
          </div>
        </div>
        <div className="receive-compare-summary" role="status">
          {pending && !id || phase ? <strong>{pending && !id ? (zh ? '正在启动…' : 'Starting…') : phaseText[phase]}</strong> : <span>{zh ? '选择要查找的差异即可开始。/ 表示整个接收目录。' : 'Choose a comparison to start. / covers the entire receive folder.'}</span>}
          {data && (phase === 'receiving'
            ? <><progress value={data.batchFinished} max={data.batchTotal || 1} aria-label={zh ? '批次进度' : 'Batch progress'} /><span>{zh ? '已处理' : 'Processed'} {data.batchFinished} / {data.batchTotal}</span></>
            : <span>{zh ? '已检查' : 'Checked'} {data.scanned.toLocaleString()} · {zh ? '找到' : 'Found'} {data.candidates.toLocaleString()} ({bytes(data.bytes)})</span>)}
          {data && data.scope !== scope && <span>{zh ? '本次范围' : 'Compared scope'} {data.scope || '/'}</span>}
          {phase === 'done' && data && <span>{zh ? `已接收 ${data.received} · 已删除 ${data.deleted} · 已跳过 ${data.skipped} · 失败 ${data.failed}` : `Received ${data.received} · Deleted ${data.deleted} · Skipped ${data.skipped} · Failed ${data.failed}`}</span>}
          {active && <button type="button" disabled={pending} onClick={async () => {
            setPending(true);
            try {await CancelReceivedComparison(id, false);} catch (err) {setError(String(err));}
            finally {setPending(false); setRevision(value => value + 1);}
          }}>{zh ? '取消任务' : 'Cancel task'}</button>}
        </div>
        {phase === 'receiving' && transfers.length > 0 && <ul className="receive-compare-transfers" aria-label={zh ? '正在处理的文件' : 'Active files'}>
          {transfers.map(file => <li key={file.id}><span title={file.path}>{file.path}</span><strong>{transferStatus(file, zh)}</strong></li>)}
        </ul>}
        {(error || data?.error) && <div className="remote-error" role="alert">{explain(error || data?.error || '')}</div>}
        {!!data?.unchecked && <details className="receive-compare-warning"><summary>{zh ? `有 ${data.unchecked} 项未完整检查（链接、权限、类型冲突或时间缺失），结果可能不完整` : `${data.unchecked} entries not fully checked (links, permissions, type conflicts or missing timestamps); results may be incomplete`}</summary>{data.warnings.map((warning, index) => <div key={index}>{explain(warning.replace('sender modification time unavailable; time comparison skipped', zh ? '对方未提供修改时间，已跳过时间比较' : 'sender modification time unavailable; time comparison skipped'))}</div>)}</details>}
        {data && hasList && <>
          <div className="receive-compare-controls">
            <input className="receive-compare-search" value={input} disabled={busy} onChange={event => setInput(event.target.value)} placeholder={zh ? '筛选相对路径' : 'Filter paths'} aria-label={zh ? '筛选相对路径' : 'Filter relative path'} />
            <button type="button" disabled={locked || !data.matched} onClick={() => select([], true, true)}>{zh ? '全选匹配项' : 'Select all matches'}</button>
            <button type="button" disabled={locked || !data.selected} onClick={() => select([], true, false, '')}>{zh ? '清空选择' : 'Clear selection'}</button>
            <div className="receive-compare-pagination">
              <span>{zh ? '筛选结果' : 'Matches'} {data.matched.toLocaleString()} · {data.page + 1} / {Math.max(1, Math.ceil(data.matched / 200))}</span>
              <button type="button" disabled={locked || data.page === 0} onClick={() => setPage(data.page - 1)}>{zh ? '上一页' : 'Previous'}</button>
              <button type="button" disabled={locked || (data.page + 1) * 200 >= data.matched} onClick={() => setPage(data.page + 1)}>{zh ? '下一页' : 'Next'}</button>
            </div>
          </div>
          <div className="receive-compare-table-wrap">
            <table className="receive-compare-table"><thead><tr>
              <th><input type="checkbox" aria-label={zh ? '选择当前页' : 'Select this page'} disabled={locked || !data.items.some(selectable)} checked={data.items.some(selectable) && data.items.filter(selectable).every(item => item.selected)} onChange={event => select(data.items.map(item => item.id), false, event.target.checked)} /></th>
              <th>{zh ? '相对路径' : 'Relative path'}</th>
              <th className="receive-compare-size-col">{zh ? '大小' : 'Size'}</th>
              <th className="receive-compare-time-col">{zh ? '修改时间' : 'Modified'}</th>
              <th className="receive-compare-status-col">{zh ? '操作状态' : 'Action status'}</th>
            </tr></thead><tbody>{data.items.map(item => {
              const {localSize, remoteSize} = item;
              const both = localSize != null && remoteSize != null;
              const times = comparisonTimes(item.localModified, item.remoteModified, lang);
              let localSizeText = localSize == null ? undefined : bytes(localSize);
              const remoteSizeText = remoteSize == null ? undefined : bytes(remoteSize);
              if (both && localSize !== remoteSize && localSizeText === remoteSizeText) {
                localSizeText += ` (${localSize > remoteSize ? '+' : '−'}${bytes(Math.abs(localSize - remoteSize))})`;
              }
              const transfer = transfers.find(file => file.id === item.id);
              const queued = phase === 'receiving' && item.selected && remoteSize != null;
              return <tr key={item.id}>
                <td><input type="checkbox" checked={item.selected} disabled={locked || !selectable(item)} aria-label={item.path} onChange={event => select([item.id], false, event.target.checked)} /></td>
                <td title={item.path}>{item.path.replace(/^\//, '')}{item.linkTarget && <small title={item.linkTarget}>{item.preserveLink ? (zh ? '接收为链接' : 'Receive as link') : (zh ? '接收目标内容' : 'Receive target contents')} → {item.linkTarget}</small>}</td>
                <td><ComparisonValue local={localSizeText} remote={remoteSizeText} equal={both && localSize === remoteSize} different={both && localSize !== remoteSize} zh={zh} /></td>
                <td><ComparisonValue local={localSize == null ? undefined : times.local || unknown} remote={remoteSize == null ? undefined : times.remote || unknown} equal={both && times.equal} different={both && !!times.local && !!times.remote && !times.equal} zh={zh} /></td>
                <td>{transfer ? <span className="compare-active">{transferStatus(transfer, zh)}{!transfer.checking && transfer.totalBytes > 0 && <small>{bytes(transfer.doneBytes)} / {bytes(transfer.totalBytes)}</small>}</span>
                  : queued ? (zh ? '等待接收' : 'Queued')
                  : item.outcome ? <span className={`compare-${item.outcome}`}>{outcomes[item.outcome]}{item.error && <small>{explain(item.error)}</small>}</span>
                  : <span className="compare-idle">{zh ? '未操作' : 'No action'}</span>}</td>
              </tr>;
            })}</tbody></table>
            {!data.items.length && <p className="receive-compare-empty">{zh ? '没有符合条件的文件。' : 'No matching files.'}</p>}
          </div>
          <footer className="receive-compare-footer">
            <span title={zh ? '跨页和筛选保留选择' : 'Selections persist across pages and filters'}>{zh ? '已选' : 'Selected'} {data.selected.toLocaleString()} · {bytes(data.selectedBytes)}</span>
            {canDelete && <select aria-label={zh ? '删除方式' : 'Deletion method'} value={mode} disabled={locked} onChange={event => setMode(event.target.value)}>
              {data.deleteMode === 'trash' && <option value="trash">{zh ? '移入系统回收站（推荐）' : 'System Trash (recommended)'}</option>}
              <option value="permanent">{zh ? '永久删除（不可恢复）' : 'Permanent deletion (cannot undo)'}</option>
            </select>}
            {canDelete && <button ref={deleteButton} type="button" className="danger" disabled={locked || !data.selectedLocalOnly} onClick={() => setConfirm('delete')}>{deleteAction}{purpose === 'differences' && ` (${data.selectedLocalOnly})`}</button>}
            {purpose !== 'localOnly' && <button ref={receiveButton} type="button" className="primary" disabled={locked || !data.selectedReceive} onClick={() => setConfirm('receive')}>{receiveAction}{purpose === 'differences' && ` (${data.selectedReceive})`}</button>}
          </footer>
        </>}
      </section>
    </div>
    {confirm && data && <ProfileDeleteDialog title={zh ? '确认处理所选文件' : 'Confirm selected files'} message={!deleting ? (zh
      ? `将${purpose === 'localMissing' ? '接收' : '接收 / 修复'} ${actionCount.toLocaleString()} 个文件（${bytes(actionBytes)}），保存到 ${data.root}。${purpose === 'differences' ? '修复会以对方内容和修改时间为准，可能覆盖本地修改；本地多出的文件不参与此操作。' : '接收前会再检查保存位置，避免覆盖你已经放入的文件。'}`
      : `${action}: ${actionCount.toLocaleString()} files (${bytes(actionBytes)}) to ${data.root}. ${purpose === 'differences' ? 'Repair uses sender contents and modification times and may overwrite local edits. Extra local files are excluded.' : 'The save location is checked again before receiving to avoid overwriting files you have already placed there.'}`) : zh
      ? `将${action} ${actionCount.toLocaleString()} 个本地多出文件（${bytes(actionBytes)}），目录：${data.root}。${mode === 'permanent' ? '此操作不可撤销。' : '回收站操作失败不会自动改为永久删除。'} 删除前会复核对端是否仍然不存在，并跳过本地已变化的文件。`
      : `${action}: ${actionCount.toLocaleString()} extra local files (${bytes(actionBytes)}) in ${data.root}. ${mode === 'permanent' ? 'This cannot be undone.' : 'Trash failures will not automatically fall back to permanent deletion.'} Remote presence and local changes are checked again before deletion.`}
      cancelLabel={zh ? '取消' : 'Cancel'} deleteLabel={action} pendingLabel={zh ? '正在开始…' : 'Starting…'} triggerRef={deleting ? deleteButton : receiveButton} onCancel={() => setConfirm(null)} onDelete={remove} />}
  </>;
}
