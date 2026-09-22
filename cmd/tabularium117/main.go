// Command tabularium117 is the Tabularium 117 executable: it reads the Anno 117 pipe (or
// a recording of one), keeps the live economy view and serves it to a browser
// on 127.0.0.1.
//
// This file owns the command-line interface and the exit codes; run.go owns
// the wiring, so it can be exercised by tests.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/netip"
	"os"
	"os/signal"
	"syscall"

	"github.com/Dealwatch/Tabularium117/internal/alerts"
	"github.com/Dealwatch/Tabularium117/internal/lan"
)

// version is injected at build time via -ldflags "-X main.version=...".
var version = "dev"

// defaultPort is 53118 so that Tabularium 117 and the anno-mods connector (53117)
// can run side by side (KONZEPT.md section 5).
const defaultPort = 53118

// exitUsage is returned for a bad command line and for a failed run.
const exitUsage = 2

// defaultAlertConfig supplies the flag defaults, so that the command line and
// the rule engine cannot drift apart.
var defaultAlertConfig = alerts.DefaultConfig()

// config is the parsed command line.
type config struct {
	replayPath  string
	replaySpeed float64
	replayLoop  bool
	recordPath  string
	port        int
	dataDir     string
	noBrowser   bool
	lan         bool
	// lanIP pins the address LAN mode binds instead of choosing one.
	lanIP string
	// lanAllowLoopback is the documented test aid: it allows --lan-ip to name
	// a loopback address, which is reachable from this PC only.
	lanAllowLoopback bool
	// serveAfterReplay keeps the HTTP server up when a replay has ended, so
	// that replayed data can be looked at in the browser.
	serveAfterReplay bool
	noDB             bool
	verbose          bool
	showVersion      bool
	// anonymizeIn is the recording --anonymize-recording rewrites without the
	// names in it. It is a tool run, not a session: nothing is served and the
	// pipe is never opened.
	anonymizeIn string
	// anonymizeOut is --out; empty means "<input>.anon.jsonl".
	anonymizeOut string
	// anonymizeIslands also replaces the island names, not only the profile
	// name in SessionStart.
	anonymizeIslands bool
	// alertDeficitSamples and alertDropPP are the two alert thresholds worth
	// a flag; the rest of alerts.Config keeps its defaults (AUFGABEN.md
	// T6.1).
	alertDeficitSamples int
	alertDropPP         float64
}

// validateLANFlags checks what can be checked without looking at this PC's
// interfaces: that --lan-ip is a usable address at all, and that the
// loopback test aid is not being used to bind anything else.
//
// Whether the address really exists on an interface is a question for the
// machine, not the command line, and is answered when LAN mode is switched
// on.
func validateLANFlags(cfg *config) error {
	if cfg.lanIP == "" {
		if cfg.lanAllowLoopback {
			return errors.New("--lan-allow-loopback is a test aid and only works together with " +
				"--lan-ip 127.0.0.x")
		}
		return nil
	}
	ip, err := netip.ParseAddr(cfg.lanIP)
	if err != nil {
		return fmt.Errorf("--lan-ip %q is not an IP address", cfg.lanIP)
	}
	ip = ip.Unmap()
	if !ip.Is4() {
		return fmt.Errorf("--lan-ip %s is not an IPv4 address; Tabularium 117 binds IPv4 only", ip)
	}
	if ip.IsLoopback() {
		if !cfg.lanAllowLoopback {
			return fmt.Errorf("--lan-ip %s is the loopback, which is already served; "+
				"name the address of the network you want to share with", ip)
		}
		return nil
	}
	if cfg.lanAllowLoopback {
		return fmt.Errorf("--lan-allow-loopback is a test aid for a loopback address; "+
			"--lan-ip %s is not one", ip)
	}
	if !lan.IsPrivate(ip) {
		return fmt.Errorf("--lan-ip %s is not a private address; Tabularium 117 only binds "+
			"10.0.0.0/8, 172.16.0.0/12 or 192.168.0.0/16", ip)
	}
	return nil
}

func main() {
	cfg, err := parseFlags(os.Args[1:], os.Stderr)
	switch {
	case errors.Is(err, flag.ErrHelp):
		os.Exit(0)
	case err != nil:
		fmt.Fprintf(os.Stderr, "tabularium117: %v\n", err)
		os.Exit(exitUsage)
	}

	if cfg.showVersion {
		fmt.Fprintf(os.Stdout, "tabularium117 %s\n", version)
		return
	}

	// Anonymising a recording is a file rewrite and nothing else: it runs
	// here, before any server or pipe exists, and the command then ends.
	if cfg.anonymizeIn != "" {
		opts := anonymizeOptions{in: cfg.anonymizeIn, out: cfg.anonymizeOut, islands: cfg.anonymizeIslands}
		if opts.out == "" {
			opts.out = anonymizedPath(opts.in)
		}
		stats, err := anonymizeRecording(context.Background(), opts, os.Stderr)
		if err != nil {
			fmt.Fprintf(os.Stderr, "tabularium117: %v\n", err)
			os.Exit(exitUsage)
		}
		printAnonymizeSummary(os.Stdout, opts, stats)
		return
	}

	// Ctrl+C (and SIGTERM) cancel the context; run shuts down cleanly and
	// returns without an error, so a deliberate stop exits 0.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := run(ctx, cfg, os.Stdout, os.Stderr); err != nil {
		fmt.Fprintf(os.Stderr, "tabularium117: %v\n", err)
		os.Exit(exitUsage)
	}
}

// parseFlags parses and validates the command line. Usage output goes to
// stderr, so tests can silence it.
func parseFlags(args []string, stderr io.Writer) (config, error) {
	fs := flag.NewFlagSet("tabularium117", flag.ContinueOnError)
	fs.SetOutput(stderr)

	var cfg config
	fs.StringVar(&cfg.replayPath, "replay", "", "replay this JSONL recording instead of reading the game's pipe")
	fs.Float64Var(&cfg.replaySpeed, "replay-speed", 1, "replay speed factor; 0 replays as fast as the pipeline accepts frames")
	fs.BoolVar(&cfg.replayLoop, "replay-loop", false, "start the recording over when it ends")
	fs.StringVar(&cfg.recordPath, "record", "", "write every raw frame to this JSONL file (created or truncated)")
	fs.IntVar(&cfg.port, "port", defaultPort, "HTTP port on 127.0.0.1; 0 picks any free port")
	fs.StringVar(&cfg.dataDir, "data-dir", "", "directory for Tabularium 117's own files (default: per-user application data)")
	fs.BoolVar(&cfg.noBrowser, "no-browser", false, "do not open a browser on start")
	fs.BoolVar(&cfg.lan, "lan", false,
		"also serve one private LAN address, protected by an access token (see --lan-ip)")
	fs.StringVar(&cfg.lanIP, "lan-ip", "",
		"LAN address to bind (default: this PC's best private address); must be an RFC 1918 address of this PC")
	fs.BoolVar(&cfg.lanAllowLoopback, "lan-allow-loopback", false,
		"test aid: allow --lan-ip to name a loopback address (reachable from this PC only)")
	fs.BoolVar(&cfg.serveAfterReplay, "serve-after-replay", false,
		"keep serving after a replay has ended, until Ctrl+C")
	fs.BoolVar(&cfg.noDB, "no-db", false, "do not keep a history database; show the live data only")
	fs.IntVar(&cfg.alertDeficitSamples, "alert-deficit-samples", defaultAlertConfig.DeficitSamples,
		"raise a deficit warning after this many consecutive measurements with a negative delta")
	fs.Float64Var(&cfg.alertDropPP, "alert-drop-pp", defaultAlertConfig.DropPercentagePoints,
		"raise a productivity warning when efficiency falls this many percentage points below its 15-minute mean")
	fs.StringVar(&cfg.anonymizeIn, "anonymize-recording", "",
		"rewrite this JSONL recording without the profile name in it and exit; nothing is served")
	fs.StringVar(&cfg.anonymizeOut, "out", "",
		"where --anonymize-recording writes its result (default: the input with .anon.jsonl appended)")
	fs.BoolVar(&cfg.anonymizeIslands, "anonymize-islands", false,
		"--anonymize-recording also replaces every island name with \"Island <sessionGUID>-<islandID>\"")
	fs.BoolVar(&cfg.verbose, "verbose", false, "log at debug level")
	fs.BoolVar(&cfg.showVersion, "version", false, "print the version and exit")

	if err := fs.Parse(args); err != nil {
		return config{}, err
	}
	if fs.NArg() > 0 {
		return config{}, fmt.Errorf("unexpected argument %q", fs.Arg(0))
	}
	// Port 0 is "any free port", which is how the tests bind without
	// colliding with a running Tabularium 117 or anything else.
	if cfg.port < 0 || cfg.port > 65535 {
		return config{}, fmt.Errorf("--port %d is outside the valid range 0-65535", cfg.port)
	}
	if cfg.replaySpeed < 0 {
		return config{}, fmt.Errorf("--replay-speed %v is negative; use 0 to replay as fast as possible", cfg.replaySpeed)
	}
	if cfg.replayLoop && cfg.replayPath == "" {
		return config{}, errors.New("--replay-loop only makes sense together with --replay")
	}
	if cfg.serveAfterReplay && cfg.replayPath == "" {
		return config{}, errors.New("--serve-after-replay only makes sense together with --replay")
	}
	if cfg.anonymizeIn == "" {
		if cfg.anonymizeOut != "" {
			return config{}, errors.New("--out only makes sense together with --anonymize-recording")
		}
		if cfg.anonymizeIslands {
			return config{}, errors.New("--anonymize-islands only makes sense together with --anonymize-recording")
		}
	}
	if err := validateLANFlags(&cfg); err != nil {
		return config{}, err
	}
	if cfg.alertDeficitSamples < 1 {
		return config{}, fmt.Errorf("--alert-deficit-samples %d is not a number of measurements; use 1 or more",
			cfg.alertDeficitSamples)
	}
	if cfg.alertDropPP <= 0 {
		return config{}, fmt.Errorf("--alert-drop-pp %v is not a drop; use a positive number of percentage points",
			cfg.alertDropPP)
	}
	return cfg, nil
}
