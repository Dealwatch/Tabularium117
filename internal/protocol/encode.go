package protocol

import (
	"encoding/binary"
	"fmt"
	"math"
	"sort"

	"github.com/Dealwatch/Tabularium117/internal/model"
)

// Encode builds the payload (type byte plus body) for a message. It exists for
// tests and for the fixture tool; nothing is ever sent to the game.
//
// Map-valued fields are written in ascending GUID order so that encoding is
// deterministic.
func Encode(m Message) ([]byte, error) {
	msgType, err := Type(m)
	if err != nil {
		return nil, err
	}
	w := &writer{buf: []byte{byte(msgType)}}

	switch v := m.(type) {
	case Version:
		w.int32(v.Version)
	case SessionStart:
		if err := w.string(v.Headline); err != nil {
			return nil, fmt.Errorf("headline: %w", err)
		}
	case SessionEnd:
	case AreaStatistics:
		if err := encodeAreaStatistics(w, v.Snapshot); err != nil {
			return nil, err
		}
	}
	if len(w.buf) > MaxFrameSize {
		return nil, fmt.Errorf("encoded payload of %d bytes exceeds %d: %w", len(w.buf), MaxFrameSize, ErrFrameTooLarge)
	}
	return w.buf, nil
}

func encodeAreaStatistics(w *writer, s model.IslandSnapshot) error {
	if s.Key.IslandID < 0 || s.Key.IslandID > math.MaxUint8 {
		return fmt.Errorf("island ID %d does not fit in a byte", s.Key.IslandID)
	}
	w.uint8(s.SessionID)
	w.uint8(uint8(s.Key.IslandID))
	w.uint8(s.AreaIndex)
	w.int32(s.Key.SessionGUID)
	if err := w.string(s.Name); err != nil {
		return fmt.Errorf("area name: %w", err)
	}
	w.int64(s.GameTimestamp)
	w.int32(int32(len(s.Products)))
	for i, p := range s.Products {
		w.int32(p.ProductGUID)
		w.float32(p.Generation)
		w.float32(p.Consumption)
		w.float32(p.Delta)
		w.float32(p.PerfectGeneration)
		w.float32(p.PerfectConsumption)
		w.int32(p.Buildings)
		w.int32(p.Maintenance)
		w.float32(p.Income)
		w.int32(p.Profit)
		w.float32(p.SummedProductivity)
		w.float32(p.AvgProductivity)
		if err := w.pairs(p.Workforce); err != nil {
			return fmt.Errorf("product entry %d workforce: %w", i, err)
		}
		if err := w.pairs(p.BuildingsByGUID); err != nil {
			return fmt.Errorf("product entry %d buildings: %w", i, err)
		}
	}
	return nil
}

type writer struct {
	buf []byte
}

func (w *writer) uint8(v uint8) { w.buf = append(w.buf, v) }

func (w *writer) int32(v int32) {
	w.buf = binary.LittleEndian.AppendUint32(w.buf, uint32(v))
}

func (w *writer) int64(v int64) {
	w.buf = binary.LittleEndian.AppendUint64(w.buf, uint64(v))
}

func (w *writer) float32(v float32) {
	w.buf = binary.LittleEndian.AppendUint32(w.buf, math.Float32bits(v))
}

func (w *writer) string(s string) error {
	if len(s) > math.MaxUint8 {
		return fmt.Errorf("string of %d bytes exceeds the 255-byte limit", len(s))
	}
	w.uint8(uint8(len(s)))
	w.buf = append(w.buf, s...)
	return nil
}

func (w *writer) pairs(m map[int32]int32) error {
	if len(m) > math.MaxInt32 {
		return fmt.Errorf("%d pairs exceed the int32 count", len(m))
	}
	keys := make([]int32, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool { return keys[i] < keys[j] })
	w.int32(int32(len(keys)))
	for _, k := range keys {
		w.int32(k)
		w.int32(m[k])
	}
	return nil
}
