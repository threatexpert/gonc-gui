import {useEffect, useId, useRef, useState} from 'react';
import {Icon} from './Icon';

type ConnectionPeer = {
  id: string;
  address: string;
  description: string;
};

type ConnectionDetailsProps = {
  title: string;
  connectedLabel: string;
  closeLabel: string;
  emptyLabel: string;
  onlineLabel: string;
  metrics: {label: string; value: string}[];
  peers: ConnectionPeer[];
};

export function ConnectionDetails({title, connectedLabel, closeLabel, emptyLabel, onlineLabel, metrics, peers}: ConnectionDetailsProps) {
  const [open, setOpen] = useState(false);
  const id = useId();
  const buttonRef = useRef<HTMLButtonElement>(null);
  const panelRef = useRef<HTMLElement>(null);
  const closeRef = useRef<HTMLButtonElement>(null);

  useEffect(() => {
    if (!open) return;
    closeRef.current?.focus();
    const dismissOutside = (event: Event) => {
      const target = event.target;
      if (target instanceof Node && !panelRef.current?.contains(target) && !buttonRef.current?.contains(target)) {
        setOpen(false);
      }
    };
    const dismissOnEscape = (event: KeyboardEvent) => {
      if (event.key === 'Escape') {
        event.preventDefault();
        setOpen(false);
        buttonRef.current?.focus();
      }
    };
    document.addEventListener('pointerdown', dismissOutside);
    document.addEventListener('focusin', dismissOutside);
    document.addEventListener('keydown', dismissOnEscape);
    return () => {
      document.removeEventListener('pointerdown', dismissOutside);
      document.removeEventListener('focusin', dismissOutside);
      document.removeEventListener('keydown', dismissOnEscape);
    };
  }, [open]);

  return (
    <div className="connections-control">
      <button
        ref={buttonRef}
        type="button"
        className={`statusbar-button connections-trigger${open ? ' active' : ''}`}
        aria-expanded={open}
        aria-haspopup="dialog"
        aria-controls={open ? id : undefined}
        title={title}
        onClick={() => setOpen((value) => !value)}
      >
        <Icon name="users" />{connectedLabel} {peers.length}
        <Icon name="chevron" className="connections-chevron" />
      </button>
      {open && (
        <section ref={panelRef} id={id} className="connections-popover" role="dialog" aria-modal="false" aria-labelledby={`${id}-title`}>
          <div className="connections-heading">
            <h2 id={`${id}-title`}>{title} <span>· {peers.length}</span></h2>
            <button ref={closeRef} type="button" className="icon-button" aria-label={closeLabel} title={closeLabel} onClick={() => {
              setOpen(false);
              buttonRef.current?.focus();
            }}><Icon name="close" /></button>
          </div>
          <div className="connections-metrics">
            {metrics.map((metric) => <div key={metric.label}><span>{metric.label}</span><strong>{metric.value}</strong></div>)}
          </div>
          <div className="connections-list">
            {peers.length === 0 ? <p className="connections-empty">{emptyLabel}</p> : peers.map((peer) => (
              <div className="connection-peer" key={peer.id}>
                <span className="dot connected" aria-hidden="true" />
                <div className="connection-peer-info">
                  <code>{peer.address}</code>
                  {peer.description && <small>{peer.description}</small>}
                </div>
                <span className="connection-peer-state">{connectedLabel}</span>
              </div>
            ))}
          </div>
          <div className="connections-foot">{onlineLabel}</div>
        </section>
      )}
    </div>
  );
}
