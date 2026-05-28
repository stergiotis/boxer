//go:build llm_generated_gemini3pro

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
This is the perfect opportunity to stretch the limits of the **Single Unified Enterprise Event Bus** concept.

To prove that Leeway’s sparse columnar design allows *truly* orthogonal data to live in the exact same table without performance or storage penalties, let's step completely out of the physical world of drones and mountains.

### 🛡️ The Third Orthogonal Use Case: CyberShield (Threat Intelligence Network)

Imagine a Swiss national cybersecurity initiative monitoring the critical infrastructure (data centers, banks, power grids). We need to log high-frequency **Network Intrusion & Threat Events**.

Astonishingly, a cyber attack maps perfectly to the *exact same Leeway schema*:
* **Entity `id` & `naturalKey`:** The Security Information and Event Management (SIEM) Alert ID and the Incident Ticket (e.g., `INC-2026-CH-001`).
* **`Symbol` (Categorical String):** The Attack Vector (`"DDOS"`, `"SQL_INJECTION"`, `"PORT_SCAN"`). We map the Low-Cardinality reference (`lr`) to the targeted Network Port (e.g., Port 443).
* **`TimeRange`:** The exact start and end time of the sustained attack — two Z64 (`time.Time`) wall-clock bounds.
* **`TimeArray`:** SIEM alert wall-clock (Z64 `time.Time`), via `BeginAttributeSingle`.
* **`GeoPoint`:** The IP Geo-location of the attacker's origin. We map the High-Cardinality reference (`hr`) to the attacker's ASN (Autonomous System Number).
* **`GeoArea`:** The physical geofence of the targeted Swiss Data Center (e.g., the underground bunkers in the Swiss Alps or Zurich's financial district).
* **`Text`:** The parsed malicious payload or CVE vulnerability description.
* **`U64Array`:** The severity scale, or the total number of malicious packets dropped by the firewall (single-value array via `BeginAttributeSingle`).
* **`BlobArray`:** A cryptographic hash of the PCAP (Packet Capture) file for forensic auditing (single-value array via `BeginAttributeSingle`).
*/

// GenerateCyberThreatEvents generates mock Arrow records for 20 network security incidents.
// It uses the EXACT SAME Leeway schema as the Drones and Alpine Sensors.
func GenerateCyberThreatEvents(recordsIn []arrow.RecordBatch) (recordsOut []arrow.RecordBatch, err error) {
	allocator := memory.NewGoAllocator()

	// Pre-allocate buffers for low-allocation string building
	incBuffer := make([]byte, 0, 32)
	pcapBuffer := make([]byte, 0, 64)

	// Initialize the Leeway Entity Builder
	table := NewInEntityTestTable(allocator, 20)

	// Generate 20 Cyber Incident Events for March 11, 2026
	for i := 1; i <= 20; i++ {
		alertID := uint64(9900000 + i)

		// Low-allocation string building (e.g., "INC-2026-CH-1")
		incBuffer = append(incBuffer[:0], "INC-2026-CH-"...)
		incBuffer = strconv.AppendInt(incBuffer, int64(i), 10)

		// 1. Begin the Entity & Set Primary Keys
		table.BeginEntity().SetId(alertID, incBuffer)

		// 2. Add Symbol (Attack Vector)
		attackType := "PORT_SCAN"
		targetPort := uint64(22) // SSH
		if i%2 == 0 {
			attackType = "SQL_INJECTION"
			targetPort = 443 // HTTPS
		} else if i%5 == 0 {
			attackType = "DDOS"
			targetPort = 53 // DNS
		}

		table.GetSectionSymbol().
			BeginAttribute(attackType).
			AddMembershipLowCardRef(targetPort). // lr = Target Port
			EndAttribute().
			EndSection()

		// 3. Add TimeRange (Sustained Attack Window) — two Z64 bounds.
		// March 11, 2026, ~23:30 Zurich time is approx 1773268200 epoch.
		baseTime := time.Unix(int64(1773268200+(i*10)), 0).UTC()
		table.GetSectionTimeRange().
			BeginAttribute(baseTime.Add(-2*time.Minute), baseTime). // Attack lasted 2 minutes
			EndAttribute().
			EndSection()

		// 3b. Add TimeArray (SIEM Alert Wall-clock).
		// Z64 timestamp anchored to the attack-window end —
		// *Single keeps the single-stamp call shape scalar.
		table.GetSectionTimeArray().
			BeginAttributeSingle(baseTime).
			EndAttribute().
			EndSection()

		// 4. Add GeoPoint (Attacker Origin IP Geo-Location)
		lat := float32(55.7558) + float32(i)*0.1 // Mock origin (e.g., somewhere in Eastern Europe)
		lng := float32(37.6173) - float32(i)*0.1
		h3Index := uint64(59920111)
		attackerASN := uint64(3356 + i) // hr = Attacker's Autonomous System Number

		table.GetSectionGeoPoint().
			BeginAttribute(lat, lng, h3Index).
			AddMembershipHighCardRef(attackerASN).
			EndAttribute().
			EndSection()

		// 5. Add Text (Intrusion Detection Payload Snippet) - two-word bag.
		if attackType == "SQL_INJECTION" {
			textSection := table.GetSectionText()
			attr := textSection.BeginAttribute("UNION SELECT username, password FROM users--")
			attr.AddToCoContainers(5, "union")
			attr.AddToCoContainers(6, "select")
			attr.EndAttribute()
			textSection.EndSection()
		}

		// 6. Add GeoArea (Targeted Swiss Data Center Geofence)
		// e.g., A polygon representing a facility in Zurich
		if attackType == "DDOS" {
			geoAreaSection := table.GetSectionGeoArea()
			attr := geoAreaSection.BeginAttribute()
			attr.AddToCoContainers(47.38, 8.51, h3Index+50)
			attr.AddToCoContainers(47.39, 8.52, h3Index+51)
			attr.AddToCoContainers(47.37, 8.53, h3Index+52)
			attr.EndAttribute()
			geoAreaSection.EndSection()
		}

		// 7. Add U64Array (Network Traffic / Packets Dropped).
		// One value per incident — *Single keeps the call shape scalar.
		packetsDropped := uint64(1000000 * i)
		table.GetSectionU64Array().
			BeginAttributeSingle(packetsDropped).
			EndAttribute().
			EndSection()

		// 8. Add BlobArray (Hash of the PCAP Forensic File).
		// Single-blob attribute via *Single helper.
		pcapBuffer = append(pcapBuffer[:0], "pcap-forensics-s3-hash-"...)
		pcapBuffer = strconv.AppendUint(pcapBuffer, alertID, 10)
		pcapHash := sha256.Sum256(pcapBuffer)

		table.GetSectionBlobArray().
			BeginAttributeSingle(pcapHash[:]).
			EndAttribute().
			EndSection()

		// Commit the event to the Leeway builder
		err = table.CommitEntity()
		if err != nil {
			// Boxer-standard structured error building
			err = eb.Build().
				Int("iteration", i).
				Uint64("alertId", alertID).
				Str("attackType", attackType).
				Errorf("failed to commit cyber threat entity: %w", err)
			return
		}
	}

	// Transfer to fully materialized Arrow Records
	recordsOut, err = table.TransferRecords(recordsIn)
	if err != nil {
		err = eh.Errorf("failed to transfer cyber arrow records: %w", err)
		return
	}

	return
}
