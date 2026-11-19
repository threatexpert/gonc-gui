package goncrunner

import (
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/threatexpert/gonc/v2/goncembed"
)

type runnerSession interface {
	Stop()
	Done() <-chan struct{}
}

type reconnectSession struct {
	stop chan struct{}
	done chan struct{}
	lost chan struct{}
	once sync.Once
}

func (s *reconnectSession) Stop()                 { s.once.Do(func() { close(s.stop) }) }
func (s *reconnectSession) Done() <-chan struct{} { return s.done }

// Every attempt has its own callback gate. Retired attempts cannot publish a
// late Ready or loss into the next attempt's route state.
type attemptCallback struct {
	goncembed.Callback
	traffic goncembed.TrafficCallback
	mu      sync.Mutex
	active  bool
	lost    func()
}

func (c *attemptCallback) invoke(f func()) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.active {
		f()
	}
}
func (c *attemptCallback) retire()           { c.mu.Lock(); c.active = false; c.mu.Unlock() }
func (c *attemptCallback) Event(l, m string) { c.invoke(func() { c.Callback.Event(l, m) }) }
func (c *attemptCallback) Error(m string)    { c.invoke(func() { c.Callback.Error(m) }) }
func (c *attemptCallback) Ready(e string)    { c.invoke(func() { c.Callback.Ready(e) }) }
func (c *attemptCallback) Stopped(code int)  { c.invoke(func() { c.Callback.Stopped(code); c.lost() }) }
func (c *attemptCallback) P2PReport(t, s, status, n, m, p string, at int64, pid int) {
	c.invoke(func() { c.Callback.P2PReport(t, s, status, n, m, p, at, pid) })
}
func (c *attemptCallback) Traffic(s string, in, out int64, ib, ob float64, elapsed int64, count int, final bool) {
	c.invoke(func() {
		if c.traffic != nil {
			c.traffic.Traffic(s, in, out, ib, ob, elapsed, count, final)
		}
	})
}

func tunnelDisconnected(status string) bool {
	s := strings.ToLower(strings.TrimSpace(status))
	switch s {
	case "disconnected", "disconnect", "closed", "stopped":
		return true
	}
	return strings.Contains(s, "fail") || strings.Contains(s, "error") || strings.Contains(s, "lost") || strings.Contains(s, "timeout")
}

type reconnectActions struct {
	refresh      func() (string, error)
	start        func(string, goncembed.Callback) (runnerSession, error)
	disconnected func()
	report       func(error)
	callback     goncembed.Callback
	retry        time.Duration
}

func startReconnectSession(local string, a reconnectActions) (*reconnectSession, error) {
	s := &reconnectSession{stop: make(chan struct{}), done: make(chan struct{}), lost: make(chan struct{}, 1)}
	signal := func() {
		select {
		case s.lost <- struct{}{}:
		default:
		}
	}
	gate := func() *attemptCallback {
		c := &attemptCallback{Callback: a.callback, active: true, lost: signal}
		c.traffic, _ = a.callback.(goncembed.TrafficCallback)
		return c
	}
	if cb, ok := a.callback.(*callback); ok {
		cb.onTunnelLost = signal
	}
	currentGate := gate()
	current, err := a.start(local, currentGate)
	if err != nil {
		currentGate.retire()
		return nil, err
	}
	go func() {
		defer close(s.done)
		for {
			// No timer and no network inspection while a tunnel attempt is running.
			select {
			case <-s.stop:
				currentGate.retire()
				current.Stop()
				<-current.Done()
				return
			case <-s.lost:
			case <-current.Done():
			}
			currentGate.retire()
			a.disconnected() // Invalidates Ready while preserving the route worker/TUN.
			current.Stop()
			// Never overlap gonc instances (their -dns setting is process-wide).
			<-current.Done()
			for {
				select {
				case <-s.lost:
					continue
				default:
				}
				break
			}
			for {
				select {
				case <-s.stop:
					return
				default:
				}
				local, err = a.refresh()
				if err == nil {
					select {
					case <-s.stop:
						return
					default:
					}
					currentGate = gate()
					current, err = a.start(local, currentGate)
					if err == nil {
						break
					}
					currentGate.retire()
				}
				a.report(fmt.Errorf("reconnect preparation failed: %w", err))
				timer := time.NewTimer(a.retry)
				select {
				case <-s.stop:
					timer.Stop()
					return
				case <-timer.C:
				}
			}
		}
	}()
	return s, nil
}
