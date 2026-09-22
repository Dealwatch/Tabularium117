package protocol

import (
	"encoding/binary"
	"fmt"
	"math"
	"time"

	"github.com/Dealwatch/Tabularium117/internal/model"
)

// Wire sizes used to bound counted lists before allocating. See the layout in
// docs/protocol.md: an entry is 48 fixed bytes plus two counted pair lists, a
// pair is two int32s.
const (
	entryFixedSize = 48
	minEntrySize   = entryFixedSize + 4 + 4
	pairSize       = 8
)

// Decode turns one payload (type byte plus body) into a Message.
//
// It is strict about truncation: any read past the end of the payload is an
// error wrapping ErrShortFrame. Bytes left over after a message has been fully
// decoded are ignored, so an added trailing field in a future format revision
// does not break us; the version gate (CheckVersion) is what catches a format
// we genuinely cannot read. Decode does not check the version itself.
//
// receivedAt is stamped onto the snapshot of an AreaProductionStatistics frame.
func Decode(payload []byte, receivedAt time.Time) (Message, error) {
	r := &reader{buf: payload}
	msgType := MessageType(r.uint8())
	if r.err != nil {
		return nil, r.err
	}

	switch msgType {
	case TypeVersion:
		m := Version{Version: r.int32()}
		return finish(m, r)
	case TypeSessionStart:
		m := SessionStart{Headline: r.string()}
		return finish(m, r)
	case TypeSessionEnd:
		return SessionEnd{}, nil
	case TypeAreaProductionStatistics:
		return decodeAreaStatistics(r, receivedAt)
	default:
		return nil, fmt.Errorf("type byte %d: %w", uint8(msgType), ErrUnknownType)
	}
}

func finish(m Message, r *reader) (Message, error) {
	if r.err != nil {
		return nil, r.err
	}
	return m, nil
}

func decodeAreaStatistics(r *reader, receivedAt time.Time) (Message, error) {
	snap := model.IslandSnapshot{
		SessionID:  r.uint8(),
		ReceivedAt: receivedAt,
	}
	islandID := r.uint8()
	snap.AreaIndex = r.uint8()
	snap.Key = model.IslandKey{SessionGUID: r.int32(), IslandID: int32(islandID)}
	snap.Name = r.string()
	snap.GameTimestamp = r.int64()

	count, err := r.count(minEntrySize, "product entries")
	if err != nil {
		return nil, err
	}
	if count > 0 {
		snap.Products = make([]model.ProductStat, 0, count)
	}
	for i := 0; i < count; i++ {
		stat, err := decodeEntry(r)
		if err != nil {
			return nil, fmt.Errorf("product entry %d: %w", i, err)
		}
		snap.Products = append(snap.Products, stat)
	}
	if r.err != nil {
		return nil, r.err
	}
	return AreaStatistics{Snapshot: snap}, nil
}

func decodeEntry(r *reader) (model.ProductStat, error) {
	stat := model.ProductStat{
		ProductGUID:        r.int32(),
		Generation:         r.float32(),
		Consumption:        r.float32(),
		Delta:              r.float32(),
		PerfectGeneration:  r.float32(),
		PerfectConsumption: r.float32(),
		Buildings:          r.int32(),
		Maintenance:        r.int32(),
		Income:             r.float32(),
		Profit:             r.int32(),
		SummedProductivity: r.float32(),
		AvgProductivity:    r.float32(),
	}
	workforce, err := r.pairs("workforce")
	if err != nil {
		return model.ProductStat{}, err
	}
	buildings, err := r.pairs("buildings")
	if err != nil {
		return model.ProductStat{}, err
	}
	stat.Workforce = workforce
	stat.BuildingsByGUID = buildings
	if r.err != nil {
		return model.ProductStat{}, r.err
	}
	return stat, nil
}

// reader walks a payload. The first failure is sticky: every later read is a
// no-op, so a decode step can chain reads and check err once.
type reader struct {
	buf []byte
	off int
	err error
}

func (r *reader) take(n int) []byte {
	if r.err != nil {
		return nil
	}
	if n < 0 || n > len(r.buf)-r.off {
		r.err = fmt.Errorf("need %d bytes at offset %d, have %d: %w", n, r.off, len(r.buf)-r.off, ErrShortFrame)
		return nil
	}
	b := r.buf[r.off : r.off+n]
	r.off += n
	return b
}

func (r *reader) uint8() uint8 {
	b := r.take(1)
	if b == nil {
		return 0
	}
	return b[0]
}

func (r *reader) int32() int32 {
	b := r.take(4)
	if b == nil {
		return 0
	}
	return int32(binary.LittleEndian.Uint32(b))
}

func (r *reader) int64() int64 {
	b := r.take(8)
	if b == nil {
		return 0
	}
	return int64(binary.LittleEndian.Uint64(b))
}

func (r *reader) float32() float32 {
	b := r.take(4)
	if b == nil {
		return 0
	}
	return math.Float32frombits(binary.LittleEndian.Uint32(b))
}

// string reads a uint8-length-prefixed string and stores it as delivered: no
// trimming, no encoding validation.
func (r *reader) string() string {
	n := int(r.uint8())
	b := r.take(n)
	if b == nil {
		return ""
	}
	return string(b)
}

// count reads an int32 element count and rejects one that the remaining bytes
// cannot possibly satisfy, so a corrupt length never drives a large allocation.
func (r *reader) count(elemSize int, what string) (int, error) {
	n := r.int32()
	if r.err != nil {
		return 0, r.err
	}
	remaining := len(r.buf) - r.off
	if n < 0 || int(n) > remaining/elemSize {
		return 0, fmt.Errorf("%s count %d with %d bytes left: %w", what, n, remaining, ErrShortFrame)
	}
	return int(n), nil
}

// pairs reads a counted (GUID, amount) list. Duplicate keys accumulate, as the
// reference reader does (docs/protocol.md).
func (r *reader) pairs(what string) (map[int32]int32, error) {
	n, err := r.count(pairSize, what)
	if err != nil {
		return nil, err
	}
	m := make(map[int32]int32, n)
	for i := 0; i < n; i++ {
		guid := r.int32()
		amount := r.int32()
		if r.err != nil {
			return nil, r.err
		}
		m[guid] += amount
	}
	return m, nil
}
