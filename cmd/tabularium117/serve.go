package main

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"strconv"

	"github.com/Dealwatch/Tabularium117/internal/server"
	"github.com/Dealwatch/Tabularium117/internal/state"
	"github.com/Dealwatch/Tabularium117/internal/store"
)

// loopbackHost is the address Tabularium 117 always binds: the PC it runs on. LAN
// mode adds a second listener on one private address next to it, never
// instead of it (KONZEPT.md section 6).
const loopbackHost = "127.0.0.1"

// httpServer is the running HTTP server: what it serves, where, and how to
// stop it.
type httpServer struct {
	server *server.Server
	url    string
	stop   context.CancelFunc
	err    chan error
}

// startServer binds the port and starts serving in the background.
//
// It returns only once the listener is open, so a port that is already taken
// is a clear failure here rather than a UI that never appears, and the caller
// can hand out a URL that really works - including the real port behind
// --port 0.
func startServer(cfg config, st *state.State, hist *history, engine server.AlertSource, log *slog.Logger) (*httpServer, error) {
	var db *store.Store
	if hist != nil {
		db = hist.store
	}

	ready := make(chan net.Addr, 1)
	srv := server.New(server.Options{
		State:   st,
		Store:   db,
		Alerts:  engine,
		Log:     log,
		Version: version,
		OnReady: func(addr net.Addr) { ready <- addr },
		LAN: server.LANOptions{
			IP:            cfg.lanIP,
			AllowLoopback: cfg.lanAllowLoopback,
		},
	})

	// The server outlives the run context on purpose: with
	// --serve-after-replay it has to stay up after the replay has ended, so
	// only Close stops it.
	ctx, cancel := context.WithCancel(context.Background())
	errc := make(chan error, 1)
	addr := net.JoinHostPort(loopbackHost, strconv.Itoa(cfg.port))
	go func() { errc <- srv.ListenAndServe(ctx, addr) }()

	select {
	case bound := <-ready:
		web := &httpServer{
			server: srv,
			url:    "http://" + bound.String() + "/",
			stop:   cancel,
			err:    errc,
		}
		if cfg.lan {
			// The LAN listener binds now, so that --lan either works or says
			// why on the console. It is not fatal either way: the loopback UI
			// is up, shows the reason, and has the switch to try again.
			if err := srv.EnableLAN(); err != nil {
				log.Error("cannot switch LAN mode on; serving this PC only", "err", err)
			}
		}
		return web, nil
	case err := <-errc:
		cancel()
		return nil, fmt.Errorf("cannot serve on %s: %w; is another Tabularium 117 (or another program) "+
			"using that port? Choose a different one with --port", addr, err)
	}
}

// Close stops the server and reports what serving ended with.
func (h *httpServer) Close() error {
	h.stop()
	return <-h.err
}
