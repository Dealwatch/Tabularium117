// Command reencode-connector turns the connector's decoded JSON capture into a
// Tabularium 117 replay recording of raw frames.
//
// The connector fixture in testdata/connector/example_responses.txt contains 14
// real AreaProductionStatistics messages as SSE lines, i.e. already decoded. To
// exercise the decoder we need the binary form, so this tool encodes those
// messages back into pipe frames and writes them as JSONL:
//
//	go run ./tools/reencode-connector
//
// The result (testdata/connector-reencoded.jsonl) is re-encoded, not captured;
// it proves the decoder, not the game. Output is deterministic: timestamps are
// synthetic and map keys are written in ascending GUID order.
package main

import (
	"bufio"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/Dealwatch/Tabularium117/internal/model"
	"github.com/Dealwatch/Tabularium117/internal/protocol"
	"github.com/Dealwatch/Tabularium117/internal/replay"
	"github.com/Dealwatch/Tabularium117/internal/source"
)

// ssePrefix marks a payload line in the connector's SSE capture.
const ssePrefix = "data: "

// baseTime is the synthetic timestamp of the first frame; frames are one
// second apart. The capture has no wall-clock time of its own.
var baseTime = time.Date(2026, 9, 21, 12, 0, 0, 0, time.UTC)

// sseMessage mirrors the connector's BuildStatisticsPayload(). Its "version"
// field is the connector's own SSE schema version and is ignored here.
type sseMessage struct {
	AreaName    string     `json:"areaName"`
	AreaIndex   int32      `json:"areaIndex"`
	IslandID    int32      `json:"islandId"`
	SessionID   int32      `json:"sessionId"`
	SessionGUID int32      `json:"sessionGuid"`
	TimeStamp   int64      `json:"timeStamp"`
	Entries     []sseEntry `json:"entries"`
}

type sseEntry struct {
	ProductGUID         int32            `json:"productGuid"`
	Generation          float32          `json:"generation"`
	Consumption         float32          `json:"consumption"`
	Delta               float32          `json:"delta"`
	PerfectGeneration   float32          `json:"perfectGeneration"`
	PerfectConsumption  float32          `json:"perfectConsumption"`
	Buildings           int32            `json:"buildings"`
	TotalMaintenance    int32            `json:"totalMaintenance"`
	TotalIncome         float32          `json:"totalIncome"`
	TotalProfit         int32            `json:"totalProfit"`
	SummedProductivity  float32          `json:"summedProductivity"`
	AverageProductivity float32          `json:"averageProductivity"`
	Workforce           map[string]int32 `json:"workforce"`
	BuildingsByGUID     map[string]int32 `json:"buildingsByGuid"`
}

func main() {
	in := flag.String("in", "testdata/connector/example_responses.txt", "connector SSE capture to read")
	out := flag.String("out", "testdata/connector-reencoded.jsonl", "replay recording to write")
	flag.Parse()

	if err := run(*in, *out); err != nil {
		fmt.Fprintf(os.Stderr, "reencode-connector: %v\n", err)
		os.Exit(1)
	}
}

func run(inPath, outPath string) error {
	messages, err := readCapture(inPath)
	if err != nil {
		return err
	}
	if len(messages) == 0 {
		return fmt.Errorf("%s: no %q lines found", inPath, strings.TrimSpace(ssePrefix))
	}

	frames := make([]protocol.Message, 0, len(messages)+2)
	frames = append(frames,
		protocol.Version{Version: protocol.SupportedVersion},
		protocol.SessionStart{Headline: "reencoded from connector capture"},
	)
	for i, m := range messages {
		snapshot, err := toSnapshot(m)
		if err != nil {
			return fmt.Errorf("%s: message %d: %w", inPath, i+1, err)
		}
		frames = append(frames, protocol.AreaStatistics{Snapshot: snapshot})
	}

	f, err := os.Create(outPath)
	if err != nil {
		return err
	}
	defer f.Close()

	buf := bufio.NewWriter(f)
	w := replay.NewWriter(buf)
	for i, msg := range frames {
		payload, err := protocol.Encode(msg)
		if err != nil {
			return fmt.Errorf("encode frame %d: %w", i, err)
		}
		at := baseTime.Add(time.Duration(i) * time.Second)
		if err := w.Write(source.Frame{ReceivedAt: at, Payload: payload}); err != nil {
			return fmt.Errorf("write frame %d: %w", i, err)
		}
	}
	if err := buf.Flush(); err != nil {
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	fmt.Printf("wrote %d frames (%d statistics messages) to %s\n", len(frames), len(messages), outPath)
	return nil
}

func readCapture(path string) ([]sseMessage, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var messages []sseMessage
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64*1024), 8<<20)
	lineNo := 0
	for scanner.Scan() {
		lineNo++
		line := scanner.Text()
		if !strings.HasPrefix(line, ssePrefix) {
			continue
		}
		var m sseMessage
		if err := json.Unmarshal([]byte(strings.TrimPrefix(line, ssePrefix)), &m); err != nil {
			return nil, fmt.Errorf("%s: line %d: %w", path, lineNo, err)
		}
		messages = append(messages, m)
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return messages, nil
}

// toSnapshot converts one captured message. It leaves ReceivedAt zero: that
// field is not part of the wire format, and the recording's t value carries the
// time instead.
func toSnapshot(m sseMessage) (model.IslandSnapshot, error) {
	if m.IslandID < 0 || m.IslandID > 255 {
		return model.IslandSnapshot{}, fmt.Errorf("island ID %d does not fit in a byte", m.IslandID)
	}
	if m.SessionID < 0 || m.SessionID > 255 {
		return model.IslandSnapshot{}, fmt.Errorf("session ID %d does not fit in a byte", m.SessionID)
	}
	if m.AreaIndex < 0 || m.AreaIndex > 255 {
		return model.IslandSnapshot{}, fmt.Errorf("area index %d does not fit in a byte", m.AreaIndex)
	}
	snapshot := model.IslandSnapshot{
		Key:           model.IslandKey{SessionGUID: m.SessionGUID, IslandID: m.IslandID},
		SessionID:     uint8(m.SessionID),
		AreaIndex:     uint8(m.AreaIndex),
		Name:          m.AreaName,
		GameTimestamp: m.TimeStamp,
	}
	for i, e := range m.Entries {
		workforce, err := guidMap(e.Workforce)
		if err != nil {
			return model.IslandSnapshot{}, fmt.Errorf("entry %d workforce: %w", i, err)
		}
		buildings, err := guidMap(e.BuildingsByGUID)
		if err != nil {
			return model.IslandSnapshot{}, fmt.Errorf("entry %d buildingsByGuid: %w", i, err)
		}
		snapshot.Products = append(snapshot.Products, model.ProductStat{
			ProductGUID:        e.ProductGUID,
			Generation:         e.Generation,
			Consumption:        e.Consumption,
			Delta:              e.Delta,
			PerfectGeneration:  e.PerfectGeneration,
			PerfectConsumption: e.PerfectConsumption,
			Buildings:          e.Buildings,
			Maintenance:        e.TotalMaintenance,
			Income:             e.TotalIncome,
			Profit:             e.TotalProfit,
			SummedProductivity: e.SummedProductivity,
			AvgProductivity:    e.AverageProductivity,
			Workforce:          workforce,
			BuildingsByGUID:    buildings,
		})
	}
	return snapshot, nil
}

// guidMap converts the JSON object's string keys back into GUIDs.
func guidMap(in map[string]int32) (map[int32]int32, error) {
	out := make(map[int32]int32, len(in))
	for k, v := range in {
		guid, err := strconv.ParseInt(k, 10, 32)
		if err != nil {
			return nil, fmt.Errorf("GUID key %q: %w", k, err)
		}
		out[int32(guid)] = v
	}
	return out, nil
}
