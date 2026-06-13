package anchor

import (
	"crypto/sha256"
	"strconv"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/memory"

	// Boxer standard observability & error modules
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
)

/*
Because Leeway uses highly sparse, nested Apache Arrow lists (`ListOfNonNullable`), an empty section consumes exactly **zero bytes** of storage penalty. This allows an entire organization to use a **Single Unified Enterprise Event Bus** table in ClickHouse.

To demonstrate this, let's look at an entirely orthogonal application: **AlpWatch - An Alpine Avalanche & Seismic Sensor Network**, operating in the Swiss Alps (e.g., around Zermatt).

### 🏔️ The Orthogonal Use Case: AlpWatch Sensor Network

Even though an avalanche warning has nothing to do with a drone delivery, it maps perfectly to the *exact same* Leeway schema:
* **Entity `id` & `naturalKey`:** The Event Sequence Number and the Sensor Node ID (e.g., `SENS-ZRM-042`).
* **`Symbol` (Categorical String):** The Event Type (`"SEISMIC_ANOMALY"`, `"SNOW_SHIFT"`, `"HEARTBEAT"`).
* **`U64Array`:** Snow load in kg/m² or peak seismic amplitude (single-value array via `BeginAttributeSingle`).
* **`TimeRange`:** The time window over which the anomaly accumulated — two Z64 (`time.Time`) wall-clock bounds.
* **`TimeArray`:** Detection wall-clock (Z64 `time.Time`), via `BeginAttributeSingle`.
* **`GeoPoint`:** The static GPS coordinates of the sensor station.
* **`GeoArea`:** The dynamically calculated *Avalanche Danger Polygon* projected down the mountain.
* **`Text`:** Automated weather bulletin text ("Heavy snowfall warning above 2000m..."). Single-word bags use `BeginAttributeSingle(text, wordLength, wordBag)`.
* **`BlobArray`:** A cryptographic hash of the high-frequency raw seismic waveform stored in cold storage (S3), via `BeginAttributeSingle`.
*/

// GenerateAlpineEvents generates mock Arrow records for 20 avalanche sensor events.
// It uses the EXACT SAME Leeway schema as the Drone Delivery use case.
func GenerateAlpineEvents(recordsIn []arrow.RecordBatch, n int) (recordsOut []arrow.RecordBatch, err error) {
	allocator := memory.NewGoAllocator()

	// Pre-allocate buffers to adhere to low-allocation coding practices
	nodeBuffer := make([]byte, 0, 32)
	hashBuffer := make([]byte, 0, 64)

	// Initialize the Leeway Entity Builder
	table := NewInEntityTestTable(allocator, 20)

	// Generate n Alpine Sensor Events for March 2026
	for i := 1; i <= n; i++ {
		eventID := uint64(500000 + i)

		// Low-allocation string building for Sensor Node ID (e.g., "SENS-ZRM-1")
		nodeBuffer = append(nodeBuffer[:0], "SENS-ZRM-"...)
		nodeBuffer = strconv.AppendInt(nodeBuffer, int64(i), 10)

		// 1. Begin the Entity & Set Primary Keys
		table.BeginEntity().SetId(eventID, nodeBuffer)

		// 2. Add Symbol (Event Category)
		eventType := "HEARTBEAT"
		if i%3 == 0 {
			eventType = "SNOW_SHIFT"
		} else if i%7 == 0 {
			eventType = "SEISMIC_ANOMALY"
		}
		table.GetSectionSymbol().
			BeginAttribute(eventType).
			AddMembershipLowCardRef(12). // lr = 12 (Sensor Hardware Version)
			EndAttribute().
			EndSection()

		// 3. Add TimeRange (Anomaly Detection Window) — two Z64 bounds.
		// e.g., anomaly detected over a 5-minute rolling window.
		baseTime := time.Unix(int64(1773269000+(i*300)), 0).UTC() // around March 2026
		table.GetSectionTimeRange().
			BeginAttribute(baseTime.Add(-5*time.Minute), baseTime).
			EndAttribute().
			EndSection()

		// 3b. Add TimeArray (Detection Wall-clock).
		// Z64 timestamp anchored to the anomaly window's end —
		// *Single keeps the single-stamp call shape scalar.
		table.GetSectionTimeArray().
			BeginAttributeSingle(baseTime).
			EndAttribute().
			EndSection()

		// 4. Add GeoPoint (Exact location of the sensor on the mountain)
		// Coordinates around Zermatt, Switzerland
		lat := float32(45.992) + float32(i)*0.005
		lng := float32(7.739) - float32(i)*0.002
		h3Index := uint64(61029384)
		techID := uint64(88812) // hr = ID of the technician responsible for this node

		table.GetSectionGeoPoint().
			BeginAttribute(lat, lng, h3Index).
			AddMembershipHighCardRef(techID).
			EndAttribute().
			EndSection()

		// 5. Add Text (Automated Weather/System Bulletin) - Sparse data!
		// One-word bag — fold the (text, wordLength, wordBag) tuple through *Single.
		if eventType == "SEISMIC_ANOMALY" {
			table.GetSectionText().
				BeginAttributeSingle("High risk of slab avalanche on northern face.", 9, "avalanche").
				EndAttribute().
				EndSection()
		}

		// 6. Add GeoArea (Projected Avalanche Danger Polygon)
		// Only generated if an anomaly is detected
		if eventType == "SEISMIC_ANOMALY" || eventType == "SNOW_SHIFT" {
			geoAreaSection := table.GetSectionGeoArea()
			attr := geoAreaSection.BeginAttribute()
			// A polygon representing the expected runout zone of the avalanche
			attr.AddToCoContainers(lat-0.01, lng-0.01, h3Index+10)
			attr.AddToCoContainers(lat-0.02, lng, h3Index+11)
			attr.AddToCoContainers(lat-0.01, lng+0.01, h3Index+12)
			attr.EndAttribute()
			geoAreaSection.EndSection()
		}

		// 7. Add U64Array (Snow Load in kg/m^2).
		// One value per sensor event — *Single keeps the call shape scalar.
		snowLoad := uint64(150 + (i * 15))
		table.GetSectionU64Array().
			BeginAttributeSingle(snowLoad).
			EndAttribute().
			EndSection()

		// 8. Add BlobArray (Hash of the raw 1000Hz seismic waveform data).
		// Single-blob attribute via *Single helper.
		hashBuffer = append(hashBuffer[:0], "waveform-s3-path-"...)
		hashBuffer = strconv.AppendUint(hashBuffer, eventID, 10)
		waveformHash := sha256.Sum256(hashBuffer)

		table.GetSectionBlobArray().
			BeginAttributeSingle(waveformHash[:]).
			EndAttribute().
			EndSection()

		// Commit the event
		err = table.CommitEntity()
		if err != nil {
			// Boxer-standard structured error building
			err = eb.Build().
				Int("iteration", i).
				Uint64("eventId", eventID).
				Str("eventType", eventType). // Useful telemetry mapping
				Errorf("failed to commit alpine sensor entity: %w", err)
			return
		}
	}

	// Transfer to fully materialized Arrow Records
	recordsOut, err = table.TransferRecords(recordsIn)
	if err != nil {
		err = eh.Errorf("failed to transfer alpine arrow records: %w", err)
		return
	}

	return
}
