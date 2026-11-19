import {useEffect, useRef, useState} from 'react';
import {CancelReceivedComparison, DeleteReceivedComparison, ReceiveReceivedComparison, ReceivedComparisonPage, SelectReceivedComparison, StartReceivedComparison} from '../wailsjs/go/main/App';
import {ProfileDeleteDialog} from './ProfileDeleteDialog';
import './ReceiveComparison.css';
import {comparisonTime} from './receiveComparisonFormat';

type Purpose = 'localOnly' | 'localMissing' | 'differences';
type Item = {id: number; path: string; size: number; localSize?: number | null; remoteSize?: number | null; modified: string; localModified: string; remoteModified: string; selected: boolean; outcome: string; error: string};
type Snapshot = {id: string; root: string; scope: string; kind: string; phase: string; error: string; remote: number; scanned: number; candidates: number; bytes: number; unchecked: number; warnings: string[]; matched: number; selected: number; selectedBytes: number; selectedLocalOnly: number; selectedLocalOnlyBytes: number; selectedReceive: number; selectedReceiveBytes: number; deleted: number; received: number; skipped: number; failed: number; deleteMode: string; page: number; items: Item[]};
type Props = {saveDir: string; lang: 'zh' | 'en'; onClose: () => void; onChanged: () => void};

function bytes(n: number) {
  if (n < 1024) return `${n} B`;
  const i = Math.min(Math.floor(Math.log(n) / Math.log(1024)), 4);
  return `${(n / 1024 ** i).toFixed(1)} ${['B', 'KB', 'MB', 'GB', 'TB'][i]}`;
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
  const hints: Record<Purpose, string> = zh ? {
    localOnly: '找出对方当前列表中没有的本地文件，按需清理。也可能包含你自行添加的文件。',
    localMissing: '找出对方有、本地没有的文件，勾选后接收。',
    differences: '找出本地多出、缺少，以及大小或修改时间不同的文件。修改时间按获取到的完整精度比较，包含秒以下差异。不比较文件内容；大小和时间相同的文件，内容仍可能不同。',
  } : {
    localOnly: 'Find local files absent from the sender list for optional cleanup. This can include files you added yourself.',
    localMissing: 'Find files the sender has that are missing locally, then select files to receive.',
    differences: 'Find extra, missing, or changed files by existence, size and modification time, using all available timestamp precision including subsecond differences. File contents are not compared. Files with the same size and time may still have different contents.',
  };

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
  const fileSize = (size: number | null | undefined) => size == null ? (zh ? '无' : 'Absent') : <>{bytes(size)}{size >= 1024 && <small>{size.toLocaleString()} B</small>}</>;
  const fileTime = (size: number | null | undefined, modified: string) => size == null ? (zh ? '无' : 'Absent') : modified ? comparisonTime(modified, lang) : (zh ? '未知' : 'Unknown');
  const selectable = (item: Item) => !['deleted', 'received', 'skipped'].includes(item.outcome);

  return <>
    <div className="qr-backdrop receive-compare-backdrop">
      <section ref={panel} className="receive-compare" role="dialog" aria-modal="true" aria-labelledby="receive-compare-title" tabIndex={-1} onKeyDown={event => {
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
          <button type="button" disabled={busy} onClick={onClose}>{zh ? '关闭' : 'Close'}</button>
        </header>
        <div className="receive-compare-root" title={saveDir}>{zh ? '接收目录：' : 'Receive folder: '}{saveDir}</div>
        <div className="receive-compare-controls">
          <label>{zh ? '比对范围' : 'Subfolder'}<input value={scope} disabled={busy} onChange={event => setScope(event.target.value)} placeholder="/" aria-label={zh ? '相对目录，/ 表示整个接收目录' : 'Relative folder; / means the whole receive folder'} /></label>
        </div>
        <div className="receive-compare-purposes" role="group" aria-label={zh ? '筛查内容' : 'What to find'}>
          {(Object.keys(purposes) as Purpose[]).map(value => <button key={value} type="button" aria-pressed={purpose === value} disabled={busy || !saveDir} onClick={() => start(value)}>{purposes[value]}</button>)}
        </div>
        <p className="receive-compare-hint">{purpose ? hints[purpose] : (zh ? '点击上方任一按钮，立即开始对应筛查。' : 'Click any option above to start that search.')}</p>
        <div className="receive-compare-summary" role="status" aria-live="polite">
          <strong>{pending && !id ? (zh ? '正在启动…' : 'Starting…') : phaseText[phase]}</strong>
          {data && <span>{zh ? '已检查' : 'Checked'} {data.scanned.toLocaleString()} · {zh ? '找到' : 'Found'} {data.candidates.toLocaleString()} ({bytes(data.bytes)})</span>}
          {data?.scope && <span>{zh ? '本次范围：' : 'Compared scope: '}{data.scope}</span>}
          {active && <button type="button" disabled={pending} onClick={async () => {
            setPending(true);
            try {await CancelReceivedComparison(id, false);} catch (err) {setError(String(err));}
            finally {setPending(false); setRevision(value => value + 1);}
          }}>{zh ? '取消任务' : 'Cancel task'}</button>}
        </div>
        {(error || data?.error) && <div className="remote-error" role="alert">{explain(error || data?.error || '')}</div>}
        {!!data?.unchecked && <details className="receive-compare-warning"><summary>{zh ? `有 ${data.unchecked} 项未完整检查（链接、权限、类型冲突或时间缺失），结果可能不完整` : `${data.unchecked} entries not fully checked (links, permissions, type conflicts or missing timestamps); results may be incomplete`}</summary>{data.warnings.map((warning, index) => <div key={index}>{warning.replace('sender modification time unavailable; time comparison skipped', zh ? '对方未提供修改时间，已跳过时间比较' : 'sender modification time unavailable; time comparison skipped')}</div>)}</details>}
        {data && (ready || phase === 'deleting' || phase === 'receiving') && <>
          <div className="receive-compare-controls">
            <input className="receive-compare-search" value={input} disabled={busy} onChange={event => setInput(event.target.value)} placeholder={zh ? '按相对路径筛选，例如 old/ 或 .zip' : 'Filter path, e.g. old/ or .zip'} aria-label={zh ? '筛选相对路径' : 'Filter relative path'} />
            <button type="button" disabled={locked || !data.matched} onClick={() => select([], true, true)}>{zh ? '选择全部筛选结果' : 'Select all matches'}</button>
            <button type="button" disabled={locked || !data.selected} onClick={() => select([], true, false, '')}>{zh ? '清空选择' : 'Clear selection'}</button>
          </div>
          <div className="receive-compare-table-wrap">
            <table className="receive-compare-table"><thead><tr>
              <th><input type="checkbox" aria-label={zh ? '选择当前页' : 'Select this page'} disabled={locked || !data.items.some(selectable)} checked={data.items.some(selectable) && data.items.filter(selectable).every(item => item.selected)} onChange={event => select(data.items.map(item => item.id), false, event.target.checked)} /></th>
              <th>{zh ? '相对路径' : 'Relative path'}</th>
              {purpose !== 'localMissing' && <th className="receive-compare-size-col">{zh ? '本地大小' : 'Local size'}</th>}
              {purpose !== 'localOnly' && <th className="receive-compare-size-col">{zh ? '对方大小' : 'Sender size'}</th>}
              <th className="receive-compare-detail-col">{zh ? '修改时间 / 结果' : 'Modified / result'}</th>
            </tr></thead><tbody>{data.items.map(item => <tr key={item.id}>
              <td><input type="checkbox" checked={item.selected} disabled={locked || !selectable(item)} aria-label={item.path} onChange={event => select([item.id], false, event.target.checked)} /></td>
              <td title={item.path}>{item.path.replace(/^\//, '')}</td>
              {purpose !== 'localMissing' && <td>{fileSize(item.localSize)}</td>}
              {purpose !== 'localOnly' && <td>{fileSize(item.remoteSize)}</td>}
              <td>{purpose !== 'localMissing' && <div>{zh ? '本地：' : 'Local: '}{fileTime(item.localSize, item.localModified)}</div>}{purpose !== 'localOnly' && <div>{zh ? '对方：' : 'Sender: '}{fileTime(item.remoteSize, item.remoteModified)}</div>}{item.outcome && <><strong>{outcomes[item.outcome]}</strong>{item.error && <small>{explain(item.error)}</small>}</>}</td>
            </tr>)}</tbody></table>
            {!data.items.length && <p className="receive-compare-empty">{zh ? '没有符合条件的文件。' : 'No matching files.'}</p>}
          </div>
          <div className="receive-compare-pagination">
            <span>{zh ? '筛选结果' : 'Matches'} {data.matched.toLocaleString()} · {data.page + 1} / {Math.max(1, Math.ceil(data.matched / 200))}</span>
            <button type="button" disabled={locked || data.page === 0} onClick={() => setPage(data.page - 1)}>{zh ? '上一页' : 'Previous'}</button>
            <button type="button" disabled={locked || (data.page + 1) * 200 >= data.matched} onClick={() => setPage(data.page + 1)}>{zh ? '下一页' : 'Next'}</button>
          </div>
          <footer className="receive-compare-footer">
            <span>{zh ? '已选' : 'Selected'} {data.selected.toLocaleString()} · {bytes(data.selectedBytes)}</span>
            {canDelete && <select aria-label={zh ? '删除方式' : 'Deletion method'} value={mode} disabled={locked} onChange={event => setMode(event.target.value)}>
              {data.deleteMode === 'trash' && <option value="trash">{zh ? '移入系统回收站（推荐）' : 'System Trash (recommended)'}</option>}
              <option value="permanent">{zh ? '永久删除（不可恢复）' : 'Permanent deletion (cannot undo)'}</option>
            </select>}
            {canDelete && <button ref={deleteButton} type="button" className="danger" disabled={locked || !data.selectedLocalOnly} onClick={() => setConfirm('delete')}>{deleteAction}{purpose === 'differences' && ` (${data.selectedLocalOnly})`}</button>}
            {purpose !== 'localOnly' && <button ref={receiveButton} type="button" className="primary" disabled={locked || !data.selectedReceive} onClick={() => setConfirm('receive')}>{receiveAction}{purpose === 'differences' && ` (${data.selectedReceive})`}</button>}
          </footer>
          <p className="receive-compare-hint">{zh ? '选择跨页保留；修改筛选不会清空已选项。' : 'Selections persist across pages and filter changes.'}</p>
          {purpose === 'differences' && <p className="receive-compare-hint">{zh ? '删除仅处理已选的本地多出文件；接收 / 修复仅处理已选的本地缺少、大小或时间不同的文件。修复会以对方为准，可能覆盖本地修改。' : 'Delete only handles selected extra local files. Receive / repair only handles selected missing files or files with different sizes or times. Repair uses the sender version and may overwrite local edits.'}</p>}
          {canDelete && mode === 'permanent' && <p className="receive-compare-warning">{data.deleteMode !== 'trash' ? (zh ? '当前系统未提供可用的回收站接口。' : 'A system Trash interface is unavailable. ') : ''}{zh ? '当前选择永久删除，不经过回收站。' : 'Permanent deletion is selected; files will not go to Trash.'}</p>}
          {(phase === 'receiving' || data.deleted + data.received + data.skipped + data.failed > 0) && <p role="status">{zh ? `已删除 ${data.deleted} · 已接收 ${data.received} · 已跳过 ${data.skipped} · 失败 ${data.failed}。失败项可重新勾选重试。` : `Deleted ${data.deleted} · Received ${data.received} · Skipped ${data.skipped} · Failed ${data.failed}. Failed entries can be selected for retry.`}</p>}
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
