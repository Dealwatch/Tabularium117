// Package replay records and replays raw frames as JSONL, so Tabularium 117 can be
// developed and tested without a running game.
//
// The record format is one JSON object per line:
//
//	{"t":"2026-09-21T13:00:00.123Z","frame":"<base64 payload>"}
//
// t is the wall-clock receive time, frame is the standard-base64 payload (type
// byte plus body, without the length prefix). Reader implements source.Source,
// like the pipe, and never decodes a payload: it only carries bytes.
package replay
