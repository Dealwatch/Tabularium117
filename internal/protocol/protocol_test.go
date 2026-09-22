package protocol_test

import (
	"bytes"
	"encoding/binary"
	"errors"
	"io"
	"math"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/Dealwatch/Tabularium117/internal/model"
	"github.com/Dealwatch/Tabularium117/internal/protocol"
)

var receivedAt = time.Date(2026, 9, 21, 12, 0, 0, 0, time.UTC)

// sampleMessages covers one value of every message type plus the awkward cases
// from the capture: several products, an island with no products, a name with a
// trailing space and a non-ASCII name.
func sampleMessages() []struct {
	name string
	msg  protocol.Message
} {
	return []struct {
		name string
		msg  protocol.Message
	}{
		{"version", protocol.Version{Version: protocol.SupportedVersion}},
		{"version unsupported", protocol.Version{Version: -7}},
		{"session start", protocol.SessionStart{Headline: "A New Beginning"}},
		{"session start empty headline", protocol.SessionStart{Headline: ""}},
		{"session end", protocol.SessionEnd{}},
		{"island with products", protocol.AreaStatistics{Snapshot: model.IslandSnapshot{
			Key:           model.IslandKey{SessionGUID: 3245, IslandID: 5},
			SessionID:     1,
			AreaIndex:     1,
			Name:          "Juliana",
			ReceivedAt:    receivedAt,
			GameTimestamp: 374522200,
			Products: []model.ProductStat{
				{
					ProductGUID: 2068, Generation: 23.191875, Consumption: 24.699997,
					Delta: -1.5081215, PerfectGeneration: 27.916876, PerfectConsumption: 24.699997,
					Buildings: 6, Maintenance: 24, Income: 0, Profit: -24,
					SummedProductivity: 27.873547, AvgProductivity: 464.55911,
					Workforce:       map[int32]int32{2181: 18},
					BuildingsByGUID: map[int32]int32{2200: 6},
				},
				{
					ProductGUID: 2072, Buildings: 0, PerfectConsumption: 2.5333335,
					Workforce:       map[int32]int32{},
					BuildingsByGUID: map[int32]int32{},
				},
				{
					ProductGUID: math.MinInt32, Generation: float32(math.Inf(-1)),
					Workforce:       map[int32]int32{0: 3, math.MaxInt32: -1},
					BuildingsByGUID: map[int32]int32{2200: 6, 2693: 10},
				},
			},
		}}},
		{"empty island with trailing space in name", protocol.AreaStatistics{Snapshot: model.IslandSnapshot{
			Key:           model.IslandKey{SessionGUID: 3245, IslandID: 13},
			SessionID:     1,
			AreaIndex:     1,
			Name:          "Zycada ",
			ReceivedAt:    receivedAt,
			GameTimestamp: 374522200,
		}}},
		{"non-ascii name", protocol.AreaStatistics{Snapshot: model.IslandSnapshot{
			Key:           model.IslandKey{SessionGUID: 6627, IslandID: 255},
			SessionID:     3,
			AreaIndex:     1,
			Name:          "Grünmoos Süd – Ínsula ✱",
			ReceivedAt:    receivedAt,
			GameTimestamp: -1,
		}}},
	}
}

func TestEncodeDecodeRoundTrip(t *testing.T) {
	for _, tc := range sampleMessages() {
		t.Run(tc.name, func(t *testing.T) {
			payload, err := protocol.Encode(tc.msg)
			if err != nil {
				t.Fatalf("Encode: %v", err)
			}
			if len(payload) == 0 {
				t.Fatal("Encode returned an empty payload")
			}
			wantType, err := protocol.Type(tc.msg)
			if err != nil {
				t.Fatalf("Type: %v", err)
			}
			if got := protocol.MessageType(payload[0]); got != wantType {
				t.Errorf("payload type byte = %v, want %v", got, wantType)
			}

			got, err := protocol.Decode(payload, receivedAt)
			if err != nil {
				t.Fatalf("Decode: %v", err)
			}
			if !reflect.DeepEqual(got, tc.msg) {
				t.Errorf("round trip mismatch\n got: %#v\nwant: %#v", got, tc.msg)
			}
		})
	}
}

func TestDecodeIgnoresTrailingBytes(t *testing.T) {
	payload, err := protocol.Encode(protocol.Version{Version: 2})
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	got, err := protocol.Decode(append(payload, 0xde, 0xad, 0xbe, 0xef), receivedAt)
	if err != nil {
		t.Fatalf("Decode with trailing bytes: %v", err)
	}
	if got != (protocol.Version{Version: 2}) {
		t.Errorf("Decode = %#v, want Version{2}", got)
	}
}

func TestDecodeUsesReceivedAt(t *testing.T) {
	payload, err := protocol.Encode(protocol.AreaStatistics{Snapshot: model.IslandSnapshot{
		Key: model.IslandKey{SessionGUID: 1, IslandID: 2}, Name: "X", GameTimestamp: 42,
	}})
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	stamp := time.Date(2001, 2, 3, 4, 5, 6, 7, time.UTC)
	msg, err := protocol.Decode(payload, stamp)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	stats, ok := msg.(protocol.AreaStatistics)
	if !ok {
		t.Fatalf("Decode returned %T, want AreaStatistics", msg)
	}
	if !stats.Snapshot.ReceivedAt.Equal(stamp) {
		t.Errorf("ReceivedAt = %v, want %v", stats.Snapshot.ReceivedAt, stamp)
	}
	if stats.Snapshot.GameTimestamp != 42 {
		t.Errorf("GameTimestamp = %d, want 42", stats.Snapshot.GameTimestamp)
	}
}

// TestDecodeAccumulatesDuplicatePairKeys builds the payload by hand: Encode
// takes maps and so cannot produce a duplicate key.
func TestDecodeAccumulatesDuplicatePairKeys(t *testing.T) {
	var body bytes.Buffer
	body.WriteByte(byte(protocol.TypeAreaProductionStatistics))
	body.WriteByte(1)    // sessionID
	body.WriteByte(5)    // islandID
	body.WriteByte(1)    // areaIndex
	putInt32(&body, 100) // sessionGUID
	body.WriteByte(2)    // name length
	body.WriteString("Ix")
	putInt64(&body, 7) // timeStamp
	putInt32(&body, 1) // one entry
	putInt32(&body, 2068)
	for i := 0; i < 5; i++ { // the five float fields
		putInt32(&body, 0)
	}
	putInt32(&body, 3) // buildings
	putInt32(&body, 0) // maintenance
	putInt32(&body, 0) // income
	putInt32(&body, 0) // profit
	putInt32(&body, 0) // summed productivity
	putInt32(&body, 0) // average productivity
	putInt32(&body, 3) // three workforce pairs, two of them the same key
	putInt32(&body, 2181)
	putInt32(&body, 18)
	putInt32(&body, 2181)
	putInt32(&body, 4)
	putInt32(&body, 0)
	putInt32(&body, 1)
	putInt32(&body, 2) // two building pairs with the same key
	putInt32(&body, 2200)
	putInt32(&body, 6)
	putInt32(&body, 2200)
	putInt32(&body, 1)

	msg, err := protocol.Decode(body.Bytes(), receivedAt)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	stats := msg.(protocol.AreaStatistics)
	if len(stats.Snapshot.Products) != 1 {
		t.Fatalf("got %d products, want 1", len(stats.Snapshot.Products))
	}
	p := stats.Snapshot.Products[0]
	wantWorkforce := map[int32]int32{2181: 22, 0: 1}
	if !reflect.DeepEqual(p.Workforce, wantWorkforce) {
		t.Errorf("Workforce = %v, want %v", p.Workforce, wantWorkforce)
	}
	wantBuildings := map[int32]int32{2200: 7}
	if !reflect.DeepEqual(p.BuildingsByGUID, wantBuildings) {
		t.Errorf("BuildingsByGUID = %v, want %v", p.BuildingsByGUID, wantBuildings)
	}
}

// TestDecodeTruncated walks every proper prefix of every sample payload. A
// short body must be a clean ErrShortFrame, never a panic and never a success.
func TestDecodeTruncated(t *testing.T) {
	for _, tc := range sampleMessages() {
		t.Run(tc.name, func(t *testing.T) {
			payload, err := protocol.Encode(tc.msg)
			if err != nil {
				t.Fatalf("Encode: %v", err)
			}
			for n := 0; n < len(payload); n++ {
				_, err := protocol.Decode(payload[:n:n], receivedAt)
				if err == nil {
					t.Fatalf("Decode of %d-byte prefix succeeded, want an error", n)
				}
				if !errors.Is(err, protocol.ErrShortFrame) {
					t.Fatalf("Decode of %d-byte prefix: %v, want ErrShortFrame", n, err)
				}
			}
		})
	}
}

func TestDecodeErrors(t *testing.T) {
	tests := []struct {
		name    string
		payload []byte
		want    error
		substr  string
	}{
		{"empty payload", nil, protocol.ErrShortFrame, ""},
		{"unknown type", []byte{4, 0, 0, 0, 0}, protocol.ErrUnknownType, "4"},
		{"unknown type 255", []byte{255}, protocol.ErrUnknownType, "255"},
		{"negative entry count", func() []byte {
			var b bytes.Buffer
			b.WriteByte(byte(protocol.TypeAreaProductionStatistics))
			b.Write([]byte{1, 5, 1})
			putInt32(&b, 100)
			b.WriteByte(0)
			putInt64(&b, 0)
			putInt32(&b, -3)
			return b.Bytes()
		}(), protocol.ErrShortFrame, ""},
		{"entry count larger than the payload can hold", func() []byte {
			var b bytes.Buffer
			b.WriteByte(byte(protocol.TypeAreaProductionStatistics))
			b.Write([]byte{1, 5, 1})
			putInt32(&b, 100)
			b.WriteByte(0)
			putInt64(&b, 0)
			putInt32(&b, math.MaxInt32)
			return b.Bytes()
		}(), protocol.ErrShortFrame, ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := protocol.Decode(tc.payload, receivedAt)
			if !errors.Is(err, tc.want) {
				t.Fatalf("Decode: %v, want %v", err, tc.want)
			}
			if tc.substr != "" && !strings.Contains(err.Error(), tc.substr) {
				t.Errorf("error %q does not mention %q", err, tc.substr)
			}
		})
	}
}

func TestEncodeRejectsOversizedString(t *testing.T) {
	_, err := protocol.Encode(protocol.SessionStart{Headline: strings.Repeat("x", 256)})
	if err == nil {
		t.Fatal("Encode of a 256-byte headline succeeded, want an error")
	}
}

func TestCheckVersion(t *testing.T) {
	if err := protocol.CheckVersion(protocol.Version{Version: protocol.SupportedVersion}); err != nil {
		t.Errorf("CheckVersion(%d): %v, want nil", protocol.SupportedVersion, err)
	}
	for _, v := range []int32{1, 3} {
		err := protocol.CheckVersion(protocol.Version{Version: v})
		if !errors.Is(err, protocol.ErrUnsupportedVersion) {
			t.Errorf("CheckVersion(%d): %v, want ErrUnsupportedVersion", v, err)
		}
	}
}

func TestReadFrameStream(t *testing.T) {
	var stream bytes.Buffer
	var want [][]byte
	for _, tc := range sampleMessages() {
		payload, err := protocol.Encode(tc.msg)
		if err != nil {
			t.Fatalf("Encode: %v", err)
		}
		if err := protocol.WriteFrame(&stream, payload); err != nil {
			t.Fatalf("WriteFrame: %v", err)
		}
		want = append(want, payload)
	}

	r := bytes.NewReader(stream.Bytes())
	for i, wantPayload := range want {
		got, err := protocol.ReadFrame(r)
		if err != nil {
			t.Fatalf("ReadFrame %d: %v", i, err)
		}
		if !bytes.Equal(got, wantPayload) {
			t.Fatalf("ReadFrame %d returned a different payload", i)
		}
	}
	if _, err := protocol.ReadFrame(r); err != io.EOF {
		t.Fatalf("ReadFrame at the end of the stream: %v, want io.EOF", err)
	}
}

func TestReadFrameErrors(t *testing.T) {
	tooLarge := make([]byte, 4)
	binary.LittleEndian.PutUint32(tooLarge, uint32(protocol.MaxFrameSize+1))

	tests := []struct {
		name  string
		input []byte
		want  error
	}{
		{"length zero", []byte{0, 0, 0, 0}, protocol.ErrShortFrame},
		{"negative length", []byte{0xff, 0xff, 0xff, 0xff}, protocol.ErrShortFrame},
		{"length above the limit", tooLarge, protocol.ErrFrameTooLarge},
		{"truncated length prefix", []byte{3, 0}, io.ErrUnexpectedEOF},
		{"body ends early", []byte{5, 0, 0, 0, 3, 1}, io.ErrUnexpectedEOF},
		{"body missing entirely", []byte{5, 0, 0, 0}, io.ErrUnexpectedEOF},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := protocol.ReadFrame(bytes.NewReader(tc.input))
			if !errors.Is(err, tc.want) {
				t.Fatalf("ReadFrame: %v, want %v", err, tc.want)
			}
		})
	}
}

func TestWriteFrameRejectsBadPayloads(t *testing.T) {
	if err := protocol.WriteFrame(io.Discard, nil); !errors.Is(err, protocol.ErrShortFrame) {
		t.Errorf("WriteFrame(nil): %v, want ErrShortFrame", err)
	}
	if err := protocol.WriteFrame(io.Discard, make([]byte, protocol.MaxFrameSize+1)); !errors.Is(err, protocol.ErrFrameTooLarge) {
		t.Errorf("WriteFrame(oversized): %v, want ErrFrameTooLarge", err)
	}
}

// FuzzDecode asserts that no input makes the decoder panic and that a decoded
// message always has a known type.
func FuzzDecode(f *testing.F) {
	for _, tc := range sampleMessages() {
		if payload, err := protocol.Encode(tc.msg); err == nil {
			f.Add(payload)
		}
	}
	f.Add([]byte(nil))
	f.Add([]byte{3})
	f.Add([]byte{3, 1, 5, 1, 0xff, 0xff, 0xff, 0x7f})

	f.Fuzz(func(t *testing.T, payload []byte) {
		msg, err := protocol.Decode(payload, receivedAt)
		if err != nil {
			return
		}
		if _, err := protocol.Type(msg); err != nil {
			t.Fatalf("decoded %T has no wire type: %v", msg, err)
		}
	})
}

func putInt32(b *bytes.Buffer, v int32) {
	_ = binary.Write(b, binary.LittleEndian, v)
}

func putInt64(b *bytes.Buffer, v int64) {
	_ = binary.Write(b, binary.LittleEndian, v)
}
