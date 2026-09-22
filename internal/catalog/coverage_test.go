package catalog

import (
	"bufio"
	"encoding/json"
	"os"
	"sort"
	"strconv"
	"strings"
	"testing"
)

const fixturePath = "../../testdata/connector/example_responses.txt"
const ssePrefix = "data: "

type fixtureEntry struct {
	ProductGUID     int32          `json:"productGuid"`
	Workforce       map[string]any `json:"workforce"`
	BuildingsByGUID map[string]any `json:"buildingsByGuid"`
}

type fixtureMessage struct {
	Entries []fixtureEntry `json:"entries"`
}

// TestFixtureCoverage checks how large a share of the GUIDs seen in the
// connector's captured fixture (testdata/connector/example_responses.txt)
// the embedded catalog resolves. It fails if the combined share across
// products, buildings, and workforce drops below 95%.
func TestFixtureCoverage(t *testing.T) {
	f, err := os.Open(fixturePath)
	if err != nil {
		t.Fatalf("open %s: %v", fixturePath, err)
	}
	defer f.Close()

	products := make(map[int32]bool)
	buildings := make(map[int32]bool)
	workforce := make(map[int32]bool)

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, ssePrefix) {
			continue
		}
		var msg fixtureMessage
		if err := json.Unmarshal([]byte(strings.TrimPrefix(line, ssePrefix)), &msg); err != nil {
			t.Fatalf("decode fixture line: %v", err)
		}
		for _, e := range msg.Entries {
			products[e.ProductGUID] = true
			for k := range e.BuildingsByGUID {
				guid, err := strconv.Atoi(k)
				if err != nil {
					t.Fatalf("non-integer building guid %q: %v", k, err)
				}
				buildings[int32(guid)] = true
			}
			for k := range e.Workforce {
				guid, err := strconv.Atoi(k)
				if err != nil {
					t.Fatalf("non-integer workforce guid %q: %v", k, err)
				}
				workforce[int32(guid)] = true
			}
		}
	}
	if err := scanner.Err(); err != nil {
		t.Fatalf("scan %s: %v", fixturePath, err)
	}

	c := Default()

	resolvedP, missingP := resolve(products, c.Product)
	resolvedB, missingB := resolve(buildings, c.Building)
	resolvedW, missingW := resolve(workforce, c.Workforce)

	t.Logf("products:  %d/%d resolved, missing %v", resolvedP, len(products), missingP)
	t.Logf("buildings: %d/%d resolved, missing %v", resolvedB, len(buildings), missingB)
	t.Logf("workforce: %d/%d resolved, missing %v", resolvedW, len(workforce), missingW)

	// Every product GUID the capture carries resolves, including the
	// synthetic 0 (tools/gen-catalog): a missing one here means the pinned
	// calculator revision or the generator lost an entry.
	if len(missingP) != 0 {
		t.Errorf("unresolved product GUIDs %v, want none", missingP)
	}

	totalResolved := resolvedP + resolvedB + resolvedW
	total := len(products) + len(buildings) + len(workforce)
	share := float64(totalResolved) / float64(total)
	t.Logf("overall: %d/%d resolved (%.1f%%)", totalResolved, total, share*100)

	if share < 0.95 {
		t.Errorf("overall resolved share %.1f%% is below the 95%% threshold", share*100)
	}
}

func resolve(guids map[int32]bool, lookup func(int32) (Entry, bool)) (resolved int, missing []int32) {
	for guid := range guids {
		if _, ok := lookup(guid); ok {
			resolved++
		} else {
			missing = append(missing, guid)
		}
	}
	sort.Slice(missing, func(i, j int) bool { return missing[i] < missing[j] })
	return resolved, missing
}
