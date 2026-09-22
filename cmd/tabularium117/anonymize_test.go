package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/Dealwatch/Tabularium117/internal/model"
	"github.com/Dealwatch/Tabularium117/internal/protocol"
)

// livePath is the live capture of 2026-09-22: 35 frames, one SessionStart
// with a headline, and ten islands (docs/protocol.md).
const livePath = "../../testdata/live-2026-09-22.jsonl"

// recordLine mirrors one line of the recording format. The replay package
// keeps its own copy unexported, and the point of these tests is to read the
// file as a file rather than through the code under test.
type recordLine struct {
	T     string `json:"t"`
	Frame string `json:"frame"`
}

// readRecordingLines parses a recording into its lines, failing the test on
// anything it cannot read.
func readRecordingLines(t *testing.T, path string) []recordLine {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("open %s: %v", path, err)
	}
	defer f.Close()

	var out []recordLine
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64*1024), 4<<20)
	for scanner.Scan() {
		if strings.TrimSpace(scanner.Text()) == "" {
			continue
		}
		var line recordLine
		if err := json.Unmarshal(scanner.Bytes(), &line); err != nil {
			t.Fatalf("%s: line %d: %v", path, len(out)+1, err)
		}
		out = append(out, line)
	}
	if err := scanner.Err(); err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return out
}

// decodeLine decodes one recorded line's payload.
func decodeLine(t *testing.T, line recordLine) protocol.Message {
	t.Helper()
	payload, err := base64.StdEncoding.DecodeString(line.Frame)
	if err != nil {
		t.Fatalf("decode base64: %v", err)
	}
	msg, err := protocol.Decode(payload, recordedTime(t, line))
	if err != nil {
		t.Fatalf("decode frame: %v", err)
	}
	return msg
}

// recordedTime parses a line's recorded time, the same way the replay reader
// does.
func recordedTime(t *testing.T, line recordLine) time.Time {
	t.Helper()
	ts, err := time.Parse(time.RFC3339Nano, line.T)
	if err != nil {
		t.Fatalf("parse the recorded time %q: %v", line.T, err)
	}
	return ts
}

// Anonymising the live capture must change exactly one thing: the profile
// name in SessionStart. Every other frame has to come out byte for byte as it
// went in, or the recording is no longer evidence of what the game sent.
func TestAnonymizeRecordingReplacesOnlyTheHeadline(t *testing.T) {
	out := filepath.Join(t.TempDir(), "anon.jsonl")
	var stderr bytes.Buffer
	stats, err := anonymizeRecording(context.Background(), anonymizeOptions{in: livePath, out: out}, &stderr)
	if err != nil {
		t.Fatalf("anonymizeRecording: %v", err)
	}

	in := readRecordingLines(t, livePath)
	got := readRecordingLines(t, out)
	if len(got) != len(in) {
		t.Fatalf("wrote %d frames, want the recorded %d", len(got), len(in))
	}
	if stats.frames != len(in) {
		t.Errorf("stats.frames = %d, want %d", stats.frames, len(in))
	}
	if stats.islands != 0 || stats.undecodable != 0 {
		t.Errorf("stats = %+v, want no renamed islands and nothing undecodable", stats)
	}
	if stderr.Len() != 0 {
		t.Errorf("unexpected warning on stderr: %s", stderr.String())
	}

	headlines := 0
	for i := range in {
		if got[i].T != in[i].T {
			t.Fatalf("frame %d: t = %q, want the recorded %q", i, got[i].T, in[i].T)
		}
		start, isStart := decodeLine(t, in[i]).(protocol.SessionStart)
		if !isStart {
			if got[i].Frame != in[i].Frame {
				t.Fatalf("frame %d is not a SessionStart but its payload changed", i)
			}
			continue
		}
		headlines++
		if start.Headline == anonymousHeadline {
			t.Fatalf("frame %d: the fixture already carries the anonymous headline, "+
				"so this test would pass without doing anything", i)
		}
		cleaned, ok := decodeLine(t, got[i]).(protocol.SessionStart)
		if !ok {
			t.Fatalf("frame %d: a SessionStart came out as %T", i, decodeLine(t, got[i]))
		}
		if cleaned.Headline != anonymousHeadline {
			t.Errorf("frame %d: headline = %q, want %q", i, cleaned.Headline, anonymousHeadline)
		}
	}
	if headlines == 0 {
		t.Fatal("the fixture has no SessionStart frame; this test proves nothing")
	}
	if stats.headlines != headlines {
		t.Errorf("stats.headlines = %d, want %d", stats.headlines, headlines)
	}
}

// --anonymize-islands replaces the island names as well, and nothing else:
// the numbers a bug report is about have to survive untouched.
func TestAnonymizeRecordingRenamesIslands(t *testing.T) {
	out := filepath.Join(t.TempDir(), "anon.jsonl")
	stats, err := anonymizeRecording(context.Background(),
		anonymizeOptions{in: livePath, out: out, islands: true}, io.Discard)
	if err != nil {
		t.Fatalf("anonymizeRecording: %v", err)
	}

	in := readRecordingLines(t, livePath)
	got := readRecordingLines(t, out)
	if len(got) != len(in) {
		t.Fatalf("wrote %d frames, want the recorded %d", len(got), len(in))
	}

	renamed := 0
	for i := range in {
		if got[i].T != in[i].T {
			t.Fatalf("frame %d: t = %q, want the recorded %q", i, got[i].T, in[i].T)
		}
		before, isArea := decodeLine(t, in[i]).(protocol.AreaStatistics)
		if !isArea {
			continue
		}
		after, ok := decodeLine(t, got[i]).(protocol.AreaStatistics)
		if !ok {
			t.Fatalf("frame %d: a statistics frame came out as %T", i, decodeLine(t, got[i]))
		}
		want := islandPlaceholder(before.Snapshot)
		if after.Snapshot.Name != want {
			t.Fatalf("frame %d: island name = %q, want %q", i, after.Snapshot.Name, want)
		}
		if before.Snapshot.Name != want {
			renamed++
		}
		// Everything but the name has to be the frame that was recorded.
		before.Snapshot.Name = want
		if !reflect.DeepEqual(before.Snapshot, after.Snapshot) {
			t.Fatalf("frame %d: the snapshot changed beyond its name:\n%+v\n%+v", i, before.Snapshot, after.Snapshot)
		}
	}
	if renamed == 0 {
		t.Fatal("no island was renamed; the fixture cannot prove this flag works")
	}
	if stats.islands != renamed {
		t.Errorf("stats.islands = %d, want %d", stats.islands, renamed)
	}
}

// islandPlaceholder is the name the anonymiser is expected to write, spelled
// out here rather than reused from the implementation.
func islandPlaceholder(snap model.IslandSnapshot) string {
	return "Island " + strconv.Itoa(int(snap.Key.SessionGUID)) + "-" + strconv.Itoa(int(snap.Key.IslandID))
}

// A recording that is not there is a command-line mistake, and the command
// has to say so and exit 2 - the same code every other bad invocation uses.
func TestAnonymizeMissingRecordingExitsTwo(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "nope.jsonl")
	stdout, stderr, code := runCommand(t, "--anonymize-recording", missing)
	if code != exitUsage {
		t.Fatalf("exit code = %d, want %d\nstdout: %s\nstderr: %s", code, exitUsage, stdout, stderr)
	}
	if !strings.Contains(stderr, "open recording") {
		t.Errorf("stderr = %q, want it to name the unreadable recording", stderr)
	}
}

// The successful path is a process too: it must not start a server or open a
// pipe, and it must exit 0 with the summary on stdout.
func TestAnonymizeCommandWritesTheDefaultOutputAndExitsZero(t *testing.T) {
	dir := t.TempDir()
	in := filepath.Join(dir, "capture.jsonl")
	recorded, err := os.ReadFile(livePath)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(in, recorded, 0o644); err != nil {
		t.Fatal(err)
	}

	stdout, stderr, code := runCommand(t, "--anonymize-recording", in)
	if code != 0 {
		t.Fatalf("exit code = %d, want 0\nstdout: %s\nstderr: %s", code, stdout, stderr)
	}
	if !strings.Contains(stdout, "headlines replaced: 1") || !strings.Contains(stdout, "frames:             35") {
		t.Errorf("summary = %q, want the frame and headline counts", stdout)
	}
	if _, err := os.Stat(anonymizedPath(in)); err != nil {
		t.Fatalf("the default output %s was not written: %v", anonymizedPath(in), err)
	}
	if strings.Contains(stderr, "serving the user interface") {
		t.Errorf("the anonymiser started the server:\n%s", stderr)
	}
}

// --out and --anonymize-islands are meaningless on their own, and saying so
// is better than silently ignoring them.
func TestAnonymizeFlagValidation(t *testing.T) {
	for name, args := range map[string][]string{
		"out without input":     {"--out", "x.jsonl"},
		"islands without input": {"--anonymize-islands"},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := parseFlags(args, io.Discard); err == nil {
				t.Fatalf("parseFlags(%v) accepted an invalid command line", args)
			}
		})
	}
	cfg := parse(t, "--anonymize-recording", "a.jsonl", "--anonymize-islands")
	if cfg.anonymizeIn != "a.jsonl" || !cfg.anonymizeIslands || cfg.anonymizeOut != "" {
		t.Errorf("flags not parsed: %+v", cfg)
	}
	if got := anonymizedPath("a.jsonl"); got != "a.jsonl.anon.jsonl" {
		t.Errorf("default output = %q, want a.jsonl.anon.jsonl", got)
	}
}

// Writing the result over the recording would destroy the original halfway
// through reading it.
func TestAnonymizeRefusesToOverwriteTheInput(t *testing.T) {
	_, err := anonymizeRecording(context.Background(), anonymizeOptions{in: livePath, out: livePath}, io.Discard)
	if err == nil {
		t.Fatal("anonymising a recording onto itself must fail")
	}
	if !strings.Contains(err.Error(), "--out") {
		t.Errorf("error = %v, want it to point at --out", err)
	}
}

// --- running the command as a process ---

// childEnv makes the test binary run main() instead of the test suite, which
// is the only way to observe an exit code.
const childEnv = "TABULARIUM117_RUN_AS_COMMAND"

func TestMain(m *testing.M) {
	if _, ok := os.LookupEnv(childEnv); ok {
		// The command line to run follows a "--" separator. The test binary's
		// own flags are never looked at: m.Run, which parses them, is not
		// reached.
		args := os.Args[1:]
		if i := slices.Index(args, "--"); i >= 0 {
			args = args[i+1:]
		}
		os.Args = append([]string{"tabularium117"}, args...)
		main()
		return
	}
	os.Exit(m.Run())
}

// runCommand runs the command line in a child process and reports what it
// wrote and how it ended.
func runCommand(t *testing.T, args ...string) (stdout, stderr string, code int) {
	t.Helper()
	cmd := exec.Command(os.Args[0], append([]string{"--"}, args...)...)
	cmd.Env = append(os.Environ(), childEnv+"=1")
	var out, errOut bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errOut

	err := cmd.Run()
	var exitErr *exec.ExitError
	switch {
	case err == nil:
		code = 0
	case errors.As(err, &exitErr):
		code = exitErr.ExitCode()
	default:
		t.Fatalf("run the command: %v", err)
	}
	return out.String(), errOut.String(), code
}
