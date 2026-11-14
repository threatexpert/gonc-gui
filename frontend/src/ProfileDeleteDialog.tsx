import {useEffect, useId, useRef, useState, type RefObject} from 'react';

type Props = {
  title: string;
  message: string;
  cancelLabel: string;
  deleteLabel: string;
  pendingLabel: string;
  triggerRef: RefObject<HTMLButtonElement>;
  onCancel: () => void;
  onDelete: () => Promise<void>;
};

// WebView hosts do not consistently implement window.confirm (notably WKWebView).
export function ProfileDeleteDialog({title, message, cancelLabel, deleteLabel, pendingLabel, triggerRef, onCancel, onDelete}: Props) {
  const id = useId();
  const dialogRef = useRef<HTMLElement>(null);
  const cancelRef = useRef<HTMLButtonElement>(null);
  const pendingRef = useRef(false);
  const [pending, setPending] = useState(false);
  const [error, setError] = useState('');

  useEffect(() => {
    const trigger = triggerRef.current;
    cancelRef.current?.focus();
    const keepFocus = (event: FocusEvent) => {
      if (event.target instanceof Node && !dialogRef.current?.contains(event.target)) {
        (pendingRef.current ? dialogRef.current : cancelRef.current)?.focus();
      }
    };
    document.addEventListener('focusin', keepFocus);
    return () => {
      document.removeEventListener('focusin', keepFocus);
      if (trigger?.isConnected) trigger.focus();
    };
  }, [triggerRef]);

  async function confirmDelete() {
    if (pendingRef.current) return;
    pendingRef.current = true;
    setPending(true);
    setError('');
    try {
      await onDelete();
    } catch (reason) {
      setError(reason instanceof Error ? reason.message : String(reason));
    } finally {
      pendingRef.current = false;
      setPending(false);
    }
  }

  return (
    <div className="qr-backdrop" onClick={(event) => {
      if (event.target === event.currentTarget && !pendingRef.current) onCancel();
    }}>
      <section
        ref={dialogRef}
        className="profile-delete-dialog"
        role="alertdialog"
        aria-modal="true"
        aria-labelledby={`${id}-title`}
        aria-describedby={`${id}-message`}
        aria-busy={pending}
        tabIndex={-1}
        onKeyDown={(event) => {
          if (event.key === 'Escape') {
            event.preventDefault();
            if (!pendingRef.current) onCancel();
          } else if (event.key === 'Tab') {
            const buttons = Array.from(event.currentTarget.querySelectorAll<HTMLButtonElement>('button:not(:disabled)'));
            const first = buttons[0];
            const last = buttons[buttons.length - 1];
            if (!first) {
              event.preventDefault();
            } else if (event.shiftKey && (document.activeElement === first || document.activeElement === event.currentTarget)) {
              event.preventDefault();
              last.focus();
            } else if (!event.shiftKey && (document.activeElement === last || document.activeElement === event.currentTarget)) {
              event.preventDefault();
              first.focus();
            }
          }
        }}
      >
        <h2 id={`${id}-title`}>{title}</h2>
        <p id={`${id}-message`}>{message}</p>
        {error && <div className="add-picker-error" role="alert">{error}</div>}
        <div className="profile-delete-actions">
          <button ref={cancelRef} type="button" className="secondary" disabled={pending} onClick={onCancel}>{cancelLabel}</button>
          <button type="button" className="danger" disabled={pending} onClick={confirmDelete}>{pending ? pendingLabel : deleteLabel}</button>
        </div>
      </section>
    </div>
  );
}
