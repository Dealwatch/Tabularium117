package pipe

import (
	"bufio"
	"context"
	"errors"
	"io"
	"sync"
	"time"

	"github.com/Dealwatch/Tabularium117/internal/protocol"
	"github.com/Dealwatch/Tabularium117/internal/source"
)

// Retry timing.
//
// waitInterval is the poll interval while the pipe does not exist; the
// reference reader polls once per second too (docs/protocol.md, "Transport").
// backoffMin and backoffMax bound the reconnect backoff after a connection
// failed or was lost (KONZEPT.md section 8: maximum 10 s).
const (
	waitInterval = 1 * time.Second
	backoffMin   = 500 * time.Millisecond
	backoffMax   = 10 * time.Second
)

// Options configures a Client. Only Dialer is required.
type Options struct {
	// Dialer opens connections. Use NewDialer(DefaultPipeName) outside tests.
	Dialer Dialer
	// OnStatus receives connection status events. It is called from Run's
	// goroutine and must not block.
	OnStatus StatusFunc
	// Now stamps received frames. Defaults to time.Now; tests override it.
	Now func() time.Time
	// Sleep waits for d or until ctx is done, returning ctx.Err() in the
	// latter case. Defaults to a real, context-aware sleep; tests override it
	// so that backoff can be asserted without waiting.
	Sleep func(ctx context.Context, d time.Duration) error
}

// Client reads frames from the named pipe and reconnects on its own. It
// implements source.Source.
type Client struct {
	dialer   Dialer
	onStatus StatusFunc
	now      func() time.Time
	sleep    func(context.Context, time.Duration) error

	// last is the most recently emitted state; it is only touched by Run.
	last     State
	haveLast bool
}

// New returns a Client for the given options.
func New(opts Options) *Client {
	c := &Client{
		dialer:   opts.Dialer,
		onStatus: opts.OnStatus,
		now:      opts.Now,
		sleep:    opts.Sleep,
	}
	if c.now == nil {
		c.now = time.Now
	}
	if c.sleep == nil {
		c.sleep = sleepContext
	}
	return c
}

// Run connects, reads frames into out, and reconnects until ctx is done.
//
// It returns ctx.Err() on cancellation. The only other way it returns is a
// failure that retrying cannot fix, currently ErrUnsupportedPlatform; every
// other failure - missing pipe, broken connection, framing error - is reported
// as a status event and retried. Run never closes out and never writes to the
// pipe.
func (c *Client) Run(ctx context.Context, out chan<- source.Frame) error {
	if c.dialer == nil {
		return errors.New("pipe: no dialer configured")
	}
	backoff := backoffMin
	for {
		if err := ctx.Err(); err != nil {
			return err
		}

		conn, err := c.dialer.Dial(ctx)
		if err != nil {
			if ctxErr := ctx.Err(); ctxErr != nil {
				return ctxErr
			}
			if errors.Is(err, ErrUnsupportedPlatform) {
				c.emit(StateDisconnected, err)
				return err
			}
			if errors.Is(err, ErrPipeNotFound) {
				// The expected idle case. Emitted once per transition, not
				// once per poll, so the UI does not flicker.
				c.emit(StateWaiting, nil)
				if err := c.sleep(ctx, waitInterval); err != nil {
					return err
				}
				continue
			}
			c.emit(StateDisconnected, err)
			if err := c.sleep(ctx, backoff); err != nil {
				return err
			}
			backoff = nextBackoff(backoff)
			continue
		}

		c.emit(StateConnected, nil)
		backoff = backoffMin

		safe := &closeOnce{ReadCloser: conn}
		readErr := c.readFrames(ctx, safe, out)
		safe.Close()
		if ctxErr := ctx.Err(); ctxErr != nil {
			return ctxErr
		}

		// Any read failure ends the connection: a clean io.EOF (the game
		// closed the pipe) as well as a framing error, after which the byte
		// stream is out of sync and only a fresh connection can recover.
		c.emit(StateDisconnected, readErr)
		if err := c.sleep(ctx, backoff); err != nil {
			return err
		}
		backoff = nextBackoff(backoff)
	}
}

// readFrames reads frames from one connection until it fails or ctx is done.
//
// A read blocked in Windows ReadFile cannot be interrupted by a context, so a
// watcher goroutine closes the connection when ctx is done; the pending read
// then fails and this loop returns. See the package documentation.
func (c *Client) readFrames(ctx context.Context, conn io.ReadCloser, out chan<- source.Frame) error {
	stop := make(chan struct{})
	defer close(stop)
	go func() {
		select {
		case <-ctx.Done():
			conn.Close()
		case <-stop:
		}
	}()

	r := bufio.NewReader(conn)
	for {
		payload, err := protocol.ReadFrame(r)
		if err != nil {
			return err
		}
		// The receiver's clock is the time base of every sample; the game's
		// own timeStamp stays opaque (docs/protocol.md).
		frame := source.Frame{ReceivedAt: c.now(), Payload: payload}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case out <- frame:
		}
	}
}

// emit reports a status change. Consecutive StateWaiting events are collapsed:
// waiting is polled once per second and only the transition is news.
func (c *Client) emit(state State, err error) {
	if c.haveLast && c.last == state && state == StateWaiting {
		return
	}
	c.last, c.haveLast = state, true
	if c.onStatus != nil {
		c.onStatus(Status{State: state, Err: err, At: c.now()})
	}
}

// nextBackoff doubles d up to backoffMax.
func nextBackoff(d time.Duration) time.Duration {
	if d <= 0 {
		return backoffMin
	}
	if d >= backoffMax/2 {
		return backoffMax
	}
	return d * 2
}

// sleepContext waits for d or until ctx is done.
func sleepContext(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return ctx.Err()
	}
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

// closeOnce makes Close idempotent, so the cancellation watcher and the read
// loop can both close the connection.
type closeOnce struct {
	io.ReadCloser
	once sync.Once
	err  error
}

func (c *closeOnce) Close() error {
	c.once.Do(func() { c.err = c.ReadCloser.Close() })
	return c.err
}
