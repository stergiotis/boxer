package anchor

import (
	"crypto/sha256"
	"strconv"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
)

/*
First demo domain: a fictional drone-delivery operator. Each flight yields a
semi-structured mission log — one flight carries delivery notes and no-fly
zones, another only telemetry and status changes — the per-entity variability
the tagged sections absorb without per-domain tables.

Mapping onto the anchor sections:
  - entity `id` / `naturalKey`: the mission id and the external tracking
    number (e.g. `TRK-8829A`).
  - `symbol`: the drone status ("IN_TRANSIT", "DELIVERED", "WEATHER_DELAY"),
    with a low-cardinality ref (`lr`) carrying the drone model id.
  - `geoPoint`: the drop-off coordinates, with a high-cardinality ref (`hr`)
    carrying the customer id.
  - `geoArea`: dynamic geofences (e.g. a restricted-airspace polygon).
  - `text`: customer delivery instructions, with the co-containers
    (wordLength, wordBag) carrying the pre-tokenized form for token search.
  - `timeRange`: the promised delivery window, two Z64 (`time.Time`) bounds.
  - `timeArray`: the dispatch wall-clock, via BeginAttributeSingle.
  - `u64Array`: telemetry such as remaining battery capacity (single-value
    array via BeginAttributeSingle).
  - `blobArray`: a hash of the customer's delivery signature (single-value
    array via BeginAttributeSingle).
*/

// GenerateDroneMissionEvents generates mock Arrow records for 20 drone missions.
func GenerateDroneMissionEvents(recordsIn []arrow.RecordBatch) (recordsOut []arrow.RecordBatch, err error) {
	allocator := memory.NewGoAllocator()

	// Pre-allocate buffers to adhere to low-allocation coding practices
	// This prevents memory allocation inside the hot loop
	trkBuffer := make([]byte, 0, 32)
	sigBuffer := make([]byte, 0, 64)

	// Initialize the Leeway Entity Builder estimating 20 records
	table := NewInEntityTestTable(allocator, 20)

	// Generate 20 Autonomous Drone Missions
	for i := 1; i <= 20; i++ {
		missionID := uint64(10000 + i)

		// Low-allocation string building for tracking code (e.g., "TRK-CH-1")
		trkBuffer = append(trkBuffer[:0], "TRK-CH-"...)
		trkBuffer = strconv.AppendInt(trkBuffer, int64(i), 10)

		// 1. Begin the Entity & Set Primary Keys
		table.BeginEntity().SetId(missionID, trkBuffer)

		// 2. Add Symbol (Categorical Status)
		status := "DELIVERED"
		if i%4 == 0 {
			status = "IN_TRANSIT"
		}
		table.GetSectionSymbol().
			BeginAttribute(status).
			AddMembershipLowCardRef(5). // lr = 5 (Drone Model AeroQuad)
			EndAttribute().
			EndSection()

		// 3. Add TimeRange (Promised Delivery Window) — two Z64 bounds.
		baseTime := time.Unix(int64(1710000000+(i*3600)), 0).UTC()
		table.GetSectionTimeRange().
			BeginAttribute(baseTime, baseTime.Add(time.Hour)).
			EndAttribute().
			EndSection()

		// 3b. Add TimeArray (Dispatch Wall-clock).
		// Z64 observation timestamp anchored to the TimeRange start —
		// *Single keeps the single-stamp call shape scalar.
		table.GetSectionTimeArray().
			BeginAttributeSingle(baseTime).
			EndAttribute().
			EndSection()

		// 4. Add GeoPoint (Drop-off Location)
		lat := float32(47.3769) + float32(i)*0.001
		lng := float32(8.5417) - float32(i)*0.001
		h3Index := uint64(60893402) // Mock H3 Geo-Index
		customerUUID := uint64(999000 + i)

		table.GetSectionGeoPoint().
			BeginAttribute(lat, lng, h3Index).
			AddMembershipHighCardRef(customerUUID).
			EndAttribute().
			EndSection()

		// 5. Add Text (customer instructions) — only for odd-numbered missions,
		// so the section stays sparse (absent attributes allocate nothing)
		if i%2 != 0 {
			textSection := table.GetSectionText()
			attr := textSection.BeginAttribute("Leave quietly at the back door.")
			attr.AddToCoContainers(5, "leave")
			attr.AddToCoContainers(7, "quietly")
			attr.EndAttribute()
			textSection.EndSection()
		}

		// 6. Add GeoArea (Avoided Flight Corridor / No Fly Zone) - Every 5th mission
		if i%5 == 0 {
			geoAreaSection := table.GetSectionGeoArea()
			attr := geoAreaSection.BeginAttribute()
			attr.AddToCoContainers(47.38, 8.55, h3Index+1)
			attr.AddToCoContainers(47.39, 8.56, h3Index+2)
			attr.AddToCoContainers(47.37, 8.57, h3Index+3)
			attr.EndAttribute()
			geoAreaSection.EndSection()
		}

		// 7. Add U64Array (Telemetry: Final Battery Capacity in mAh).
		// One value per mission — the *Single API keeps the call shape scalar.
		batteryLevel := uint64(8500 - (i * 100))
		table.GetSectionU64Array().
			BeginAttributeSingle(batteryLevel).
			EndAttribute().
			EndSection()

		// 8. Add BlobArray (Cryptographic signature of delivery proof).
		// Single-blob attribute via *Single helper; low-allocation payload assembly.
		sigBuffer = append(sigBuffer[:0], "signed-by-"...)
		sigBuffer = strconv.AppendUint(sigBuffer, customerUUID, 10)
		sigHash := sha256.Sum256(sigBuffer)

		table.GetSectionBlobArray().
			BeginAttributeSingle(sigHash[:]).
			EndAttribute().
			EndSection()

		// Finalize the record into the Arrow builders
		err = table.CommitEntity()
		if err != nil {
			// structured error context via eb
			err = eb.Build().
				Int("iteration", i).
				Uint64("missionId", missionID).
				Errorf("failed to commit drone mission entity: %w", err)
			return
		}
	}

	// Extract the fully populated Arrow Records ready for ClickHouse
	recordsOut, err = table.TransferRecords(recordsIn)
	if err != nil {
		err = eh.Errorf("failed to transfer arrow records: %w", err)
		return
	}

	return
}
