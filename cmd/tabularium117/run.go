package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/Dealwatch/Tabularium117/internal/alerts"
	"github.com/Dealwatch/Tabularium117/internal/catalog"
	"github.com/Dealwatch/Tabularium117/internal/ingest"
	"github.com/Dealwatch/Tabularium117/internal/model"
	"github.com/Dealwatch/Tabularium117/internal/pipe"
	"github.com/Dealwatch/Tabularium117/internal/replay"
	"github.com/Dealwatch/Tabularium117/internal/source"
	"github.com/Dealwatch/Tabularium117/internal/state"
)

// startupErr holds run to its promise during start-up: once ctx is done, a
// step that failed because of it is the shutdown that was asked for, not a
// failure to report.
//
// Opening the database is the step this happens to. It is the one start-up
// step that takes a context, and on a loaded machine it takes long enough
// that a Ctrl+C right after the start lands inside it. SQLite then returns
// "context deadline exceeded", which main would otherwise report as if the
// program had broken.
func startupErr(ctx context.Context, err error) error {
	if err == nil || ctx.Err() != nil {
		return nil
	}
	return err
}

// run wires the source, the pipeline and the state together and blocks until
// the source stops or ctx is done. A cancelled context is a clean shutdown and
// returns nil - during start-up (startupErr) as well as while running;
// everything else is an error the caller reports and exits on.
func run(ctx context.Context, cfg config, stdout, stderr io.Writer) (err error) {
	level := slog.LevelInfo
	if cfg.verbose {
		level = slog.LevelDebug
	}
	logger := slog.New(slog.NewTextHandler(stderr, &slog.HandlerOptions{Level: level}))

	dataDir, err := resolveDataDir(cfg.dataDir)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		return fmt.Errorf("create data directory %s: %w", dataDir, err)
	}
	logger.Info("tabularium117 starting", "version", version, "data_dir", dataDir)

	st := state.New()
	// Unknown-GUID warnings go through the same structured log as everything else.
	catalog.Default().Logger = logger
	pipeline := &ingest.Pipeline{State: st, Log: logger}

	// The rule engine is built before the server, which reads it for the
	// active warnings. It is pure memory: no goroutine, no files, nothing to
	// close.
	engine := alerts.New(alerts.Config{
		DeficitSamples:       cfg.alertDeficitSamples,
		DropPercentagePoints: cfg.alertDropPP,
	})
	logger.Info("watching for warnings",
		"deficit_samples", engine.Config().DeficitSamples,
		"drop_percentage_points", engine.Config().DropPercentagePoints)

	if cfg.recordPath != "" {
		rec, rerr := newRecorder(cfg.recordPath)
		if rerr != nil {
			return rerr
		}
		// The recording is only complete once it is flushed, so a failure
		// here is reported unless something worse already happened.
		defer func() {
			if cerr := rec.Close(); cerr != nil && err == nil {
				err = cerr
			}
		}()
		pipeline.Record = rec.frames
		logger.Info("recording raw frames", "file", cfg.recordPath)
	}

	var hist *history
	if !cfg.noDB {
		hist, err = openHistory(ctx, dataDir, logger)
		if err != nil {
			return startupErr(ctx, err)
		}
		// The history is only complete once the pending batch is written, so
		// a failure here is reported unless something worse already happened.
		defer func() {
			if cerr := hist.Close(); cerr != nil && err == nil {
				err = cerr
			}
		}()
	}

	// The UI comes up before the source: when the game is not running yet,
	// the page is what tells the user so (KONZEPT.md section 8).
	web, err := startServer(cfg, st, hist, engine, logger)
	if err != nil {
		return err
	}
	logger.Info("serving the user interface", "url", web.url)
	// A serving failure is reported unless something worse already happened.
	defer func() {
		if cerr := web.Close(); cerr != nil && err == nil {
			err = cerr
		}
	}()
	if !cfg.noBrowser {
		openBrowser(web.url, logger)
	}

	src, cleanup, err := openSource(cfg, st, pipeline, logger, stdout)
	if err != nil {
		return err
	}
	defer cleanup()

	// openSource may have installed its own OnSnapshot (the replay console
	// printer), so the store is chained onto whatever is already there
	// instead of replacing it.
	if hist != nil {
		hist.attach(pipeline, st)
	}
	// The browser is told last, after the console and the history have had
	// the snapshot: publishing is a non-blocking hand-over to the connected
	// event streams, so it cannot hold the pipeline up either way.
	delivered := pipeline.OnSnapshot
	pipeline.OnSnapshot = func(snap model.IslandSnapshot) {
		if delivered != nil {
			delivered(snap)
		}
		web.server.PublishSnapshot(snap)
	}
	pipeline.OnStatus = web.server.PublishStatus
	attachAlerts(pipeline, engine, hist, web)

	runErr := pipeline.Run(ctx, src)

	if cfg.replayPath != "" {
		printSummary(stdout, st)
		if cfg.serveAfterReplay && ctx.Err() == nil {
			logger.Info("the recording has ended; the UI stays up until Ctrl+C", "url", web.url)
			<-ctx.Done()
		}
	}
	stats := pipeline.Stats()
	logger.Info("stopped", "frames", stats.Frames, "snapshots", stats.Snapshots,
		"decode_errors", stats.DecodeErrors, "dropped_by_version", stats.VersionDropped,
		"duplicate_products", stats.DuplicateProducts)

	switch {
	case runErr == nil:
		return nil
	case ctx.Err() != nil && errors.Is(runErr, ctx.Err()):
		// A stop we asked for - Ctrl+C, SIGTERM or a deadline - is a clean
		// shutdown, not a failure.
		return nil
	case errors.Is(runErr, pipe.ErrUnsupportedPlatform):
		return fmt.Errorf("cannot read the Anno 117 pipe: %w, and this is a %s build; "+
			"use --replay <file> to run without the game", pipe.ErrUnsupportedPlatform, runtime.GOOS)
	default:
		return runErr
	}
}

// attachAlerts chains the rule engine onto the pipeline, last in the chain.
//
// The snapshot is published to the browser before it is evaluated: the live
// numbers are what the user is looking at, and a warning about a snapshot
// nobody has seen yet would arrive out of order. Every event then goes to the
// history (when it is kept) and to the connected browsers, in that order -
// the same order the snapshots take, so an alert is stored after the island
// row its snapshot created.
//
// A session boundary resets the engine, because island identity stops being
// meaningful there (state.Reset does the same). The cleared events it returns
// close the open rows in the history.
func attachAlerts(pipeline *ingest.Pipeline, engine *alerts.Engine, hist *history, web *httpServer) {
	publish := func(events []alerts.Event) {
		for _, ev := range events {
			if hist != nil {
				hist.addAlert(ev)
			}
			web.server.PublishAlert(ev)
		}
	}

	previous := pipeline.OnSnapshot
	pipeline.OnSnapshot = func(snap model.IslandSnapshot) {
		if previous != nil {
			previous(snap)
		}
		publish(engine.Apply(snap))
	}

	previousStart := pipeline.OnSessionStart
	pipeline.OnSessionStart = func(headline string, at time.Time) {
		if previousStart != nil {
			previousStart(headline, at)
		}
		publish(engine.Reset())
	}

	previousEnd := pipeline.OnSessionEnd
	pipeline.OnSessionEnd = func(at time.Time) {
		if previousEnd != nil {
			previousEnd(at)
		}
		publish(engine.Reset())
	}
}

// openSource builds the frame source: a recording when --replay was given,
// otherwise the game's named pipe. The returned cleanup closes what was
// opened.
func openSource(cfg config, st *state.State, pipeline *ingest.Pipeline, logger *slog.Logger, stdout io.Writer) (source.Source, func(), error) {
	if cfg.replayPath == "" {
		client := pipe.New(pipe.Options{
			Dialer:   pipe.NewDialer(pipe.DefaultPipeName),
			OnStatus: pipeline.PipeStatus(),
		})
		logger.Info("reading the game pipe", "pipe", pipe.DefaultPipeName)
		return client, func() {}, nil
	}

	f, err := os.Open(cfg.replayPath)
	if err != nil {
		return nil, nil, fmt.Errorf("open recording: %w", err)
	}
	st.SetConnection(state.Connection{
		Mode:  state.ModeReplay,
		State: state.StateReplaying,
		Since: time.Now(),
	})
	// Until the UI exists, the replay reports what it stores on the console;
	// that is what a replay is for.
	pipeline.OnSnapshot = func(snap model.IslandSnapshot) {
		fmt.Fprintf(stdout, "island %q session=%d id=%d products=%d\n",
			snap.Name, snap.Key.SessionGUID, snap.Key.IslandID, len(snap.Products))
	}
	logger.Info("replaying a recording", "file", cfg.replayPath, "speed", cfg.replaySpeed, "loop", cfg.replayLoop)
	reader := replay.NewReader(f, replay.Options{Speed: cfg.replaySpeed, Loop: cfg.replayLoop})
	return reader, func() { f.Close() }, nil
}

// printSummary prints one row per island plus the totals, in the order
// State.Islands defines.
func printSummary(w io.Writer, st *state.State) {
	islands := st.Islands()
	products := 0

	fmt.Fprintln(w)
	if headline, _ := st.Session(); headline != "" {
		fmt.Fprintf(w, "session %q\n", headline)
	}
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "SESSION\tISLAND\tNAME\tPRODUCTS")
	for _, island := range islands {
		products += len(island.Products)
		// Names are stored as delivered and trimmed only for display
		// (docs/protocol.md).
		fmt.Fprintf(tw, "%d\t%d\t%s\t%d\n",
			island.Key.SessionGUID, island.Key.IslandID, strings.TrimSpace(island.Name), len(island.Products))
	}
	tw.Flush()
	fmt.Fprintf(w, "\n%d islands, %d products\n", len(islands), products)
}

// resolveDataDir reports where Tabularium 117 keeps its own files: on Windows
// %LOCALAPPDATA%\Tabularium117, elsewhere the user's config directory.
func resolveDataDir(flagValue string) (string, error) {
	if flagValue != "" {
		abs, err := filepath.Abs(flagValue)
		if err != nil {
			return "", fmt.Errorf("resolve --data-dir %q: %w", flagValue, err)
		}
		return abs, nil
	}
	if runtime.GOOS == "windows" {
		if base := os.Getenv("LOCALAPPDATA"); base != "" {
			return filepath.Join(base, "Tabularium117"), nil
		}
	}
	base, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("determine the data directory: %w; use --data-dir", err)
	}
	return filepath.Join(base, "Tabularium117"), nil
}

// recorder is the --record output: a buffered JSONL file.
type recorder struct {
	file   *os.File
	buf    *bufio.Writer
	frames *replay.Writer
}

// newRecorder creates or truncates the recording file.
func newRecorder(path string) (*recorder, error) {
	f, err := os.Create(path)
	if err != nil {
		return nil, fmt.Errorf("create recording: %w", err)
	}
	buf := bufio.NewWriter(f)
	return &recorder{file: f, buf: buf, frames: replay.NewWriter(buf)}, nil
}

// Close flushes the buffer and closes the file.
func (r *recorder) Close() error {
	flushErr := r.buf.Flush()
	closeErr := r.file.Close()
	if flushErr != nil {
		return fmt.Errorf("flush recording: %w", flushErr)
	}
	if closeErr != nil {
		return fmt.Errorf("close recording: %w", closeErr)
	}
	return nil
}
