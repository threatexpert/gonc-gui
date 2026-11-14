import {useEffect, useLayoutEffect, useRef, useState, type KeyboardEvent as ReactKeyboardEvent, type PointerEvent as ReactPointerEvent} from 'react';
import {Icon} from './Icon';

export type ProfileStripItem = {
  name: string;
  detail: string;
  initials: string;
};

type ProfileStripProps = {
  items: ProfileStripItem[];
  selected: number;
  locked: boolean;
  showAll: boolean;
  pageLabel: string;
  reorderLabel: string;
  onSelect: (index: number) => void;
  onMove: (from: number, to: number) => void;
};

type Press = {
  index: number | null;
  grip: boolean;
  pointerId: number;
  pointerType: string;
  startX: number;
  startY: number;
  startScroll: number;
  scrolling: boolean;
  offsetX: number;
  offsetY: number;
  width: number;
  height: number;
};

type Drag = {
  from: number;
  to: number;
  x: number;
  y: number;
  offsetX: number;
  offsetY: number;
  width: number;
  height: number;
};

const minItemWidth = 180;
const itemGap = 8;
const pagePadding = 3;
const mouseDragThreshold = 5;
const touchSlop = 8;
const edgeZone = 36;
const edgeDelayMs = 550;
const wheelGestureGapMs = 250;
const snapRestoreMs = 450;
const springBackMs = 420;

// Order after moving one entry, as a list of original indices.
function movedOrder(count: number, from: number, to: number) {
  const order = Array.from({length: count}, (_value, index) => index);
  const [moved] = order.splice(from, 1);
  order.splice(to, 0, moved);
  return order;
}

// iOS-style rubber band: the further past the edge, the stronger the resistance.
function rubberBand(overshoot: number, dimension: number) {
  const distance = Math.abs(overshoot);
  return Math.sign(overshoot) * (1 - 1 / ((distance * 0.55) / Math.max(1, dimension) + 1)) * dimension;
}

// Profiles in one row, paged like a home screen, or all of them in a wrapping grid.
// In the row, dragging scrolls between pages; dragging a profile by its grip reorders it.
export function ProfileStrip({items, selected, locked, showAll, pageLabel, reorderLabel, onSelect, onMove}: ProfileStripProps) {
  const stripRef = useRef<HTMLDivElement>(null);
  const [width, setWidth] = useState(0);
  const [page, setPage] = useState(0);
  const [drag, setDrag] = useState<Drag | null>(null);
  const [freeScroll, setFreeScroll] = useState(false);
  const pressRef = useRef<Press | null>(null);
  const dragRef = useRef<Drag | null>(null);
  const edgeRef = useRef({direction: 0, timer: 0});
  const snapTimerRef = useRef(0);
  const suppressClickRef = useRef(false);
  const focusAfterMoveRef = useRef<number | null>(null);
  const skipSelectedScrollRef = useRef<number | null>(null);
  const initialScrollDoneRef = useRef(false);
  const itemRefs = useRef(new Map<number, HTMLElement>());
  const positionsRef = useRef({width: 0, items: new Map<number, {left: number; top: number}>()});

  const innerWidth = Math.max(0, width - pagePadding * 2);
  const perPage = Math.max(1, Math.floor((innerWidth + itemGap) / (minItemWidth + itemGap)));
  const pageCount = showAll ? 1 : Math.max(1, Math.ceil(items.length / perPage));
  const itemWidth = (innerWidth - itemGap * (perPage - 1)) / perPage;
  const canReorder = !locked && items.length > 1;
  const layoutRef = useRef({perPage, pageCount, itemWidth, count: items.length, canReorder, showAll});
  layoutRef.current = {perPage, pageCount, itemWidth, count: items.length, canReorder, showAll};
  const onMoveRef = useRef(onMove);
  onMoveRef.current = onMove;

  const order = drag ? movedOrder(items.length, drag.from, drag.to) : items.map((_item, index) => index);
  const pages: number[][] = [];
  const pageSize = showAll ? Math.max(1, order.length) : perPage;
  for (let start = 0; start < order.length; start += pageSize) {
    pages.push(order.slice(start, start + pageSize));
  }

  function currentPage() {
    const strip = stripRef.current;
    return strip ? Math.round(strip.scrollLeft / Math.max(1, strip.clientWidth)) : 0;
  }

  function scrollToPage(target: number, smooth = true) {
    const strip = stripRef.current;
    if (!strip) return;
    const next = Math.min(Math.max(target, 0), layoutRef.current.pageCount - 1);
    strip.scrollTo({left: next * strip.clientWidth, behavior: smooth ? 'smooth' : 'auto'});
    setPage(next);
  }

  // Keep the stored positions keyed by the new indices, so the next layout pass
  // animates moved items from where they were drawn.
  function remapPositions(from: number, to: number) {
    const moved = movedOrder(items.length, from, to);
    const previous = positionsRef.current.items;
    const next = new Map<number, {left: number; top: number}>();
    moved.forEach((oldIndex, newIndex) => {
      const position = previous.get(oldIndex);
      if (position) next.set(newIndex, position);
    });
    positionsRef.current.items = next;
    const nextSelected = moved.indexOf(selected);
    skipSelectedScrollRef.current = nextSelected !== selected ? nextSelected : null;
  }

  function startDrag(press: Press, x: number, y: number) {
    if (press.index === null) return;
    const next: Drag = {from: press.index, to: press.index, x, y, offsetX: press.offsetX, offsetY: press.offsetY, width: press.width, height: press.height};
    dragRef.current = next;
    suppressClickRef.current = true;
    document.documentElement.classList.add('profile-dragging');
    setDrag(next);
  }

  function updateDrag(x: number, y: number) {
    const strip = stripRef.current;
    const active = dragRef.current;
    if (!strip || !active) return;
    const layout = layoutRef.current;
    const rect = strip.getBoundingClientRect();
    const column = Math.min(layout.perPage - 1, Math.max(0, Math.floor((x - rect.left - pagePadding) / (layout.itemWidth + itemGap))));
    const row = Math.max(0, Math.floor((y - rect.top - pagePadding) / (active.height + itemGap)));
    const to = Math.min(layout.count - 1, layout.showAll ? row * layout.perPage + column : currentPage() * layout.perPage + column);
    const next = {...active, x, y, to};
    dragRef.current = next;
    setDrag(next);
    if (layout.showAll) return;

    const direction = x < rect.left + edgeZone ? -1 : x > rect.right - edgeZone ? 1 : 0;
    if (direction !== edgeRef.current.direction) {
      window.clearTimeout(edgeRef.current.timer);
      edgeRef.current = {direction, timer: direction ? window.setTimeout(() => flipWhileDragging(direction), edgeDelayMs) : 0};
    }
  }

  // Holding a dragged profile at the strip's edge turns the page.
  function flipWhileDragging(direction: number) {
    if (!dragRef.current) return;
    const target = currentPage() + direction;
    if (target >= 0 && target < layoutRef.current.pageCount) {
      scrollToPage(target);
      window.setTimeout(() => {
        const active = dragRef.current;
        if (active) updateDrag(active.x, active.y);
      }, 320);
    }
    edgeRef.current = {direction, timer: window.setTimeout(() => flipWhileDragging(direction), edgeDelayMs + 300)};
  }

  function releaseClickSoon() {
    window.setTimeout(() => {
      suppressClickRef.current = false;
    }, 0);
  }

  function finishDrag(commit: boolean) {
    window.clearTimeout(edgeRef.current.timer);
    edgeRef.current = {direction: 0, timer: 0};
    const active = dragRef.current;
    dragRef.current = null;
    if (!active) return;
    document.documentElement.classList.remove('profile-dragging');
    setDrag(null);
    if (commit && active.from !== active.to) {
      remapPositions(active.from, active.to);
      onMoveRef.current(active.from, active.to);
    }
    releaseClickSoon();
  }

  function setOverscroll(offset: number, durationMs: number) {
    const strip = stripRef.current;
    if (!strip) return;
    strip.style.setProperty('--overscroll-duration', `${durationMs}ms`);
    strip.style.setProperty('--overscroll', `${offset}px`);
  }

  function startScroll(press: Press, x: number) {
    press.scrolling = true;
    suppressClickRef.current = true;
    window.clearTimeout(snapTimerRef.current);
    setFreeScroll(true);
    if (press.pointerType === 'mouse') document.documentElement.classList.add('profile-dragging');
    moveScroll(press, x);
  }

  // Past the first or last page the strip stops scrolling and the pages stretch instead.
  function moveScroll(press: Press, x: number) {
    const strip = stripRef.current;
    if (!strip) return;
    const desired = press.startScroll - (x - press.startX);
    const clamped = Math.min(Math.max(desired, 0), Math.max(0, strip.scrollWidth - strip.clientWidth));
    strip.scrollLeft = clamped;
    setOverscroll(-rubberBand(desired - clamped, strip.clientWidth), 0);
  }

  // Settle on a page: a pull past the threshold turns it, a shorter one springs back.
  function finishScroll(press: Press, x: number) {
    const strip = stripRef.current;
    document.documentElement.classList.remove('profile-dragging');
    setOverscroll(0, springBackMs);
    if (strip) {
      const pageWidth = Math.max(1, strip.clientWidth);
      const startPage = Math.round(press.startScroll / pageWidth);
      const threshold = Math.min(80, pageWidth * 0.2);
      const pulled = x - press.startX;
      scrollToPage(pulled < -threshold ? startPage + 1 : pulled > threshold ? startPage - 1 : startPage);
    }
    snapTimerRef.current = window.setTimeout(() => setFreeScroll(false), snapRestoreMs);
    releaseClickSoon();
  }

  function endPress(commit: boolean, x: number) {
    const press = pressRef.current;
    pressRef.current = null;
    if (!press) return;
    if (dragRef.current) {
      finishDrag(commit);
    } else if (press.scrolling) {
      finishScroll(press, commit ? x : press.startX);
    }
  }

  const handlersRef = useRef({startDrag, updateDrag, startScroll, moveScroll, endPress});
  handlersRef.current = {startDrag, updateDrag, startScroll, moveScroll, endPress};

  useLayoutEffect(() => {
    const strip = stripRef.current;
    if (!strip) return;
    const measure = () => setWidth(strip.clientWidth);
    measure();
    const observer = new ResizeObserver(measure);
    observer.observe(strip);
    return () => observer.disconnect();
  }, []);

  useEffect(() => {
    const handlePointerMove = (event: PointerEvent) => {
      const press = pressRef.current;
      if (!press || event.pointerId !== press.pointerId) return;
      const handlers = handlersRef.current;
      if (dragRef.current) {
        handlers.updateDrag(event.clientX, event.clientY);
        return;
      }
      if (press.scrolling) {
        handlers.moveScroll(press, event.clientX);
        return;
      }
      const dx = event.clientX - press.startX;
      const dy = event.clientY - press.startY;
      if (Math.hypot(dx, dy) <= (press.pointerType === 'mouse' ? mouseDragThreshold : touchSlop)) return;
      if (press.grip && layoutRef.current.canReorder) {
        handlers.startDrag(press, event.clientX, event.clientY);
        handlers.updateDrag(event.clientX, event.clientY);
      } else if (layoutRef.current.showAll) {
        // Nothing to scroll when every profile is shown.
        pressRef.current = null;
      } else if (press.pointerType === 'mouse' || Math.abs(dx) > Math.abs(dy)) {
        handlers.startScroll(press, event.clientX);
      } else {
        // A vertical touch swipe: leave it to the page.
        pressRef.current = null;
      }
    };
    const handlePointerUp = (event: PointerEvent) => {
      if (pressRef.current?.pointerId === event.pointerId) handlersRef.current.endPress(true, event.clientX);
    };
    const handlePointerCancel = (event: PointerEvent) => {
      if (pressRef.current?.pointerId === event.pointerId) handlersRef.current.endPress(false, event.clientX);
    };
    const holdTouchScroll = (event: TouchEvent) => {
      if (dragRef.current) event.preventDefault();
    };
    const cancelOnEscape = (event: KeyboardEvent) => {
      if (event.key === 'Escape' && dragRef.current) handlersRef.current.endPress(false, 0);
    };
    window.addEventListener('pointermove', handlePointerMove);
    window.addEventListener('pointerup', handlePointerUp);
    window.addEventListener('pointercancel', handlePointerCancel);
    window.addEventListener('touchmove', holdTouchScroll, {passive: false});
    window.addEventListener('keydown', cancelOnEscape);
    return () => {
      window.removeEventListener('pointermove', handlePointerMove);
      window.removeEventListener('pointerup', handlePointerUp);
      window.removeEventListener('pointercancel', handlePointerCancel);
      window.removeEventListener('touchmove', holdTouchScroll);
      window.removeEventListener('keydown', cancelOnEscape);
      handlersRef.current.endPress(false, 0);
      window.clearTimeout(snapTimerRef.current);
    };
  }, []);

  // A vertical mouse wheel turns one page per gesture.
  useEffect(() => {
    const strip = stripRef.current;
    if (!strip) return;
    let lastWheel = 0;
    const handleWheel = (event: WheelEvent) => {
      if (layoutRef.current.showAll || Math.abs(event.deltaY) <= Math.abs(event.deltaX)) return;
      const target = Math.round(strip.scrollLeft / Math.max(1, strip.clientWidth)) + (event.deltaY > 0 ? 1 : -1);
      if (target < 0 || target >= layoutRef.current.pageCount) return;
      event.preventDefault();
      const now = performance.now();
      if (now - lastWheel > wheelGestureGapMs) {
        strip.scrollTo({left: target * strip.clientWidth, behavior: 'smooth'});
      }
      lastWheel = now;
    };
    strip.addEventListener('wheel', handleWheel, {passive: false});
    return () => strip.removeEventListener('wheel', handleWheel);
  }, []);

  // Bring the selected profile's page into view when the selection changes (add, import, delete).
  useEffect(() => {
    if (width === 0 || dragRef.current) return;
    if (skipSelectedScrollRef.current === selected) {
      skipSelectedScrollRef.current = null;
      return;
    }
    if (showAll) return;
    scrollToPage(Math.floor(selected / perPage), initialScrollDoneRef.current);
    initialScrollDoneRef.current = true;
  }, [selected, perPage, width, showAll]);

  useEffect(() => {
    if (page > pageCount - 1) scrollToPage(pageCount - 1);
  }, [page, pageCount]);

  // Slide items that changed place instead of letting them jump.
  useLayoutEffect(() => {
    const strip = stripRef.current;
    if (!strip) return;
    const animate = positionsRef.current.width === width && !window.matchMedia('(prefers-reduced-motion: reduce)').matches;
    const next = new Map<number, {left: number; top: number}>();
    itemRefs.current.forEach((element, index) => {
      // Layout offsets inside the strip ignore scrolling, the edge stretch and running animations.
      const position = {left: element.offsetLeft, top: element.offsetTop};
      const previous = positionsRef.current.items.get(index);
      if (animate && previous && !element.classList.contains('profile-placeholder')) {
        const dx = previous.left - position.left;
        const dy = previous.top - position.top;
        if ((Math.abs(dx) > 1 || Math.abs(dy) > 1) && Math.abs(dx) < strip.clientWidth) {
          element.animate([{translate: `${dx}px ${dy}px`}, {translate: '0 0'}], {duration: 180, easing: 'cubic-bezier(0.2, 0, 0, 1)'});
        }
      }
      next.set(index, position);
    });
    positionsRef.current = {width, items: next};

    const focusIndex = focusAfterMoveRef.current;
    if (focusIndex !== null) {
      focusAfterMoveRef.current = null;
      itemRefs.current.get(focusIndex)?.focus();
      if (!showAll) scrollToPage(Math.floor(focusIndex / perPage));
    }
  });

  function registerItem(index: number) {
    return (element: HTMLElement | null) => {
      if (element) {
        itemRefs.current.set(index, element);
      } else {
        itemRefs.current.delete(index);
      }
    };
  }

  function handlePointerDown(event: ReactPointerEvent<HTMLDivElement>) {
    if (event.button !== 0 || dragRef.current) return;
    const target = event.target instanceof Element ? event.target : null;
    const itemElement = target?.closest<HTMLButtonElement>('button[data-profile-index]') || null;
    const index = itemElement && !itemElement.disabled ? Number(itemElement.dataset.profileIndex) : null;
    const rect = itemElement?.getBoundingClientRect();
    pressRef.current = {
      index,
      grip: index !== null && Boolean(target?.closest('.profile-grip')),
      pointerId: event.pointerId,
      pointerType: event.pointerType,
      startX: event.clientX,
      startY: event.clientY,
      startScroll: event.currentTarget.scrollLeft,
      scrolling: false,
      offsetX: rect ? event.clientX - rect.left : 0,
      offsetY: rect ? event.clientY - rect.top : 0,
      width: rect?.width || 0,
      height: rect?.height || 0,
    };
  }

  function handleKeyDown(event: ReactKeyboardEvent<HTMLButtonElement>, index: number) {
    if (!event.altKey || !canReorder) return;
    const delta = event.key === 'ArrowLeft' ? -1 : event.key === 'ArrowRight' ? 1 : 0;
    const to = index + delta;
    if (!delta || to < 0 || to >= items.length) return;
    event.preventDefault();
    remapPositions(index, to);
    focusAfterMoveRef.current = to;
    onMove(index, to);
  }

  function itemContent(item: ProfileStripItem) {
    return (
      <>
        <span className="profile-avatar" aria-hidden="true">{item.initials}</span>
        <span className="profile-text">
          <strong title={item.name}>{item.name}</strong>
          <small title={item.detail}>{item.detail}</small>
        </span>
        {canReorder && <span className="profile-grip" title={reorderLabel} aria-hidden="true"><Icon name="grip" /></span>}
      </>
    );
  }

  const dragged = drag ? items[drag.from] : undefined;

  return (
    <div className="profile-strip-wrap">
      <div
        ref={stripRef}
        className={`profile-strip${showAll ? ' show-all' : ''}${freeScroll ? ' free-scroll' : ''}`}
        onPointerDown={handlePointerDown}
        onScroll={(event) => {
          const strip = event.currentTarget;
          setPage(Math.round(strip.scrollLeft / Math.max(1, strip.clientWidth)));
        }}
      >
        {pages.map((pageItems, pageIndex) => (
          <div className="profile-page" key={pageIndex} style={{gridTemplateColumns: `repeat(${perPage}, minmax(0, 1fr))`}}>
            {pageItems.map((index) => drag?.from === index ? (
              <div key={index} ref={registerItem(index)} className="profile-placeholder" aria-hidden="true" />
            ) : (
              <button
                type="button"
                key={index}
                ref={registerItem(index)}
                data-profile-index={index}
                className={`profile-item${index === selected ? ' active' : ''}`}
                aria-pressed={index === selected}
                aria-keyshortcuts={canReorder ? 'Alt+ArrowLeft Alt+ArrowRight' : undefined}
                disabled={locked && index !== selected}
                onClick={() => {
                  if (!suppressClickRef.current) onSelect(index);
                }}
                onKeyDown={(event) => handleKeyDown(event, index)}
              >
                {itemContent(items[index])}
              </button>
            ))}
          </div>
        ))}
      </div>
      {pageCount > 1 && (
        <div className="profile-dots">
          {Array.from({length: pageCount}, (_value, index) => (
            <button
              type="button"
              key={index}
              className={index === page ? 'active' : ''}
              aria-label={pageLabel.replace('{n}', String(index + 1))}
              aria-current={index === page ? 'true' : undefined}
              onClick={() => scrollToPage(index)}
            >
              <span />
            </button>
          ))}
        </div>
      )}
      {drag && dragged && (
        <div
          className={`profile-item profile-ghost${drag.from === selected ? ' active' : ''}`}
          style={{left: drag.x - drag.offsetX, top: drag.y - drag.offsetY, width: drag.width, height: drag.height}}
          aria-hidden="true"
        >
          {itemContent(dragged)}
        </div>
      )}
    </div>
  );
}
