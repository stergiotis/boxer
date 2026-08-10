---
type: explanation
audience: package maintainer
status: draft
# reviewed-by: "@<handle>"     # fill in and uncomment when flipping to stable
# reviewed-date: YYYY-MM-DD    # fill in and uncomment when flipping to stable
---

> **Status: draft — pre-human-review.** Not verified by a second reader; do not
> cite as authoritative.

# Column-store market survey — lineages and generations (August 2026)

> **Provenance.** A dated snapshot, not a maintained reference — per this
> directory's convention it is not kept true against the market after
> compilation, and it decays on the scale of months. Compiled 2026-08-06 from
> prior knowledge plus five same-day web fact-check passes; extended 2026-08-09
> with the §1 vocabulary ladder and §7 (next-generation file formats, after a
> sixth fact-check pass); extended 2026-08-10 with §9 (erasure under
> GDPR/nFADP, after a seventh fact-check pass). Dated claims carry a
> month/year. Valuations and "reportedly" items are press-level accuracy, not
> audited fact.

Scope: OSS and COTS engines a team can actually run **on-premise** for real
analytical work over large datasets. Cloud-only services (Snowflake, BigQuery,
Redshift, MotherDuck) appear only as lineage nodes and market forces.
"Column store" is read broadly: columnar-at-rest stores, columnar-execution
engines over open files, and embeddable columnar libraries.

## 1. How the market decomposes

Four layers, increasingly decoupled since ~2013:

| Layer | What competes there | Examples |
| --- | --- | --- |
| Integrated stores | own storage + own execution; serving latency | ClickHouse, Doris, StarRocks, Pinot, Druid, Vertica, Exasol, Kinetica, kdb+ |
| Engines over open files | stateless SQL on Parquet + table formats | Trino, Presto, Impala, Spark, DuckDB-on-lake, DataFusion |
| Embeddable libraries | execution as a component inside other software | DuckDB, DataFusion, Velox, Polars, chDB |
| Formats & protocols | the durable substrate; deliberately moat-free | Parquet, ORC, Arrow, Iceberg / Delta / Hudi / Paimon, DuckLake, Flight SQL, ADBC |

The persistent tension: engine-native formats win serving latency (the
real-time systems keep their MergeTree parts / segments / tablets), open
formats win portability and have absorbed everything batch-shaped. Since 2024
the real-time engines write open formats too — ClickHouse 25.8 Iceberg DML
(Aug 2025), StarRocks 4.0 Iceberg writes (fall 2025), Doris 4.1 Iceberg
v2/v3 read/write (2026) — closing the gap from their side.

Orthogonal to the layers: the names in this field denote different *kinds* of
artifact, and comparisons go wrong when a word slides between kinds. The
ladder, one canonical example per rung:

| Kind | Examples | The tell |
| --- | --- | --- |
| query-execution engine | DataFusion, Velox | runs plans over data it does not own; no server, no durable state of its own |
| in-process database | DuckDB | storage format + catalog + transactions, inside the host process |
| database server (DBMS) | ClickHouse, Doris | adds the operational shell: server, auth, replication, backup, upgrades |
| data-frame library | Polars, pandas | API calls instead of a query language; an optimizer may hide inside (Polars lazy frames) or not (pandas) |
| at-rest data format | Parquet, ORC, Lance | bytes on storage, readable by engines not yet written (§6–§7) |
| interchange format | Arrow; Arrow IPC stream/file | an in-memory layout for zero-copy hand-off, plus its serializations |
| query language | SQL dialects, PromQL | a text contract with many independent implementations; outlives its engines |
| wire protocol | Postgres wire, Flight SQL | how client bytes reach a server; speaking Postgres ≠ being Postgres (§3) |
| driver API | JDBC, ODBC, ADBC | the client-side binding a tool links against; ADBC is the columnar successor to the row-oriented pair |

Two footnotes to the ladder. The cut under "database vs DBMS" that actually
separates DuckDB from ClickHouse is *in-process vs operated server* — DuckDB
brands itself an in-process DBMS, so the classic word pair alone does not do
it. And the load-bearing systems straddle rungs on purpose: DuckDB is also
used as a bare engine and reads Parquet/Iceberg/Vortex; chDB repackages the
ClickHouse DBMS as an in-process library; Polars fronts its own engine with a
data-frame surface; Arrow the interchange format grew a wire protocol (Flight
SQL) and a driver API (ADBC). G5's "dissolved the boundary between database
and library" legacy is exactly this straddling, industrialized. The layer
table places products in the market; the ladder pins what a name denotes.

## 2. Generations

Each generation was pulled into existence by a new *community*, not by engine
research alone. Years are centers of mass; generations overlap and persist.

### G0 — proof niches (pre-2005)

- Systems: **Sybase IQ** (1996, first commercial columnar for decision
  support; today SAP IQ), **kdb+** (KX; k/kdb heritage from 1998), **MonetDB**
  (CWI Amsterdam, research roots 1993, OSS 2004).
- Driving communities: decision-support reporting; quant finance (kdb+ still
  owns tick data); database research.
- Formats: proprietary, per-engine. Legacy: proved the physics — transposed
  storage, late materialization, compression on sorted columns.

### G1 — columnar MPP warehouses (2005–2012)

- Trigger papers, both 2005: **C-Store** (Stonebraker, MIT) and
  **MonetDB/X100** (CWI) — decomposition storage plus *vectorized execution*.
- Systems: **Vertica** (2005, from C-Store), **VectorWise** (2010, from X100;
  today Actian's engine), **ParAccel** (2005; engine later licensed to become
  AWS Redshift, 2012), **Exasol** (in-memory MPP, Nuremberg), **Greenplum**
  (2005, Postgres MPP; columnar option 2010). Appliance cousins (Netezza) were
  row-based — not column stores, but they shaped the MPP market.
- Incumbent retrofits, slightly later: Teradata Columnar (2011), **SAP HANA**
  (2011), SQL Server columnstore (2012), DB2 BLU (2013), Oracle In-Memory
  (2014). These made "columnar" a checkbox rather than a product category.
- Driving community: enterprise BI / data-warehouse consolidation.
- Formats: proprietary. Legacy: vectorized execution and columnar compression
  became the standard execution model everyone since uses.

### G2 — open formats on commodity clusters (2010–2016)

- Trigger paper: Google **Dremel** (2010) — nested columnar encoding.
- Systems: Hive (RCFile 2011 → **ORC** 2013, Hortonworks+Facebook),
  **Impala** (Cloudera, 2012, C++ MPP), **Presto** (Facebook, 2012, OSS 2013),
  **Spark** SQL (2014; Parquet as default), Drill, Kudu (2015, mutable
  columnar storage), Kylin (2014, MOLAP cubes).
- Format: **Parquet** (2013, Twitter+Cloudera, Dremel's record shredding) —
  the single most consequential artifact of the generation.
- Driving community: web-scale batch on Hadoop; the Cloudera / Hortonworks /
  MapR vendor ecosystem.
- Legacy: **the format decoupled from the engine.** SQL-on-files became
  normal; every later generation builds on that assumption. The engines
  themselves are now legacy: Impala's last release was Mar 2025 (nothing in
  the 17 months since), Drill and Kylin are near-dormant, Kudu is in
  maintenance (1.18.1, Jan 2026).

### G3 — real-time OLAP serving (2011–2020; peak adoption now)

- Systems: **Druid** (2011, Metamarkets, ad-tech), **Pinot** (LinkedIn 2013,
  OSS 2015), **ClickHouse** (built at Yandex for Metrica web analytics, in
  production ~2012, OSS Jun 2016), **Apache Doris** (Baidu "Palo", built for
  ad reporting from ~2008, OSS 2017, Apache TLP 2022), **StarRocks** (2020
  fork of Doris, Linux Foundation Feb 2023).
- Driving communities: ad-tech and clickstream first (Druid, ClickHouse);
  user-facing product analytics at consumer scale (Pinot: "who viewed my
  profile", Uber Eats dashboards); then the Chinese internet platforms
  (Doris/StarRocks: e-commerce, logistics, ads) — a distinct, vendor-driven
  OSS culture that now ships features fastest.
- Formats: engine-native columnar (MergeTree parts, segments, tablets) —
  open formats were too slow for sub-second, high-QPS serving over streaming
  ingest. Kafka is the standard front door.
- State 2026: ClickHouse is the breakout — Series D $400M at ~$15B valuation
  (Jan 2026) after the $350M Series C (May 2025). Druid is fading
  (release-note contributors fell 48 → 29 across 2025–2026; Imply quiet since
  its 2022 round). Pinot 1.5 (Jun 2026) and Doris 4.x / StarRocks 4.x are
  healthy; Doris's international vendor SelectDB now brands as VeloDB.

### G4 — lakehouse (2017–)

- Table formats: **Hudi** (Uber, 2017; 1.0 Dec 2024), **Iceberg** (Netflix,
  2018), **Delta** (Databricks, 2019), **Paimon** (Flink lineage, TLP Apr
  2024). Iceberg won the neutrality war (2024, Databricks buying Tabular was
  the concession); Iceberg v3 ratified mid-2025 (deletion vectors, VARIANT,
  geometry, row lineage), v4 in progress; Delta 5.0 proposes adopting
  Iceberg's metadata tree outright — convergence, not war.
- Engines: **Trino** (2019 fork of Presto, renamed end-2020; Starburst) as
  the lake SQL head; Spark as the ETL workhorse; everyone else retrofitting
  reads and now writes.
- Catalogs standardized on the **Iceberg REST protocol**: Apache Polaris (TLP
  Feb 2026), Apache Gravitino (TLP Jun 2025), Lakekeeper, Unity Catalog.
- Driving community: platform engineering — "assemble Snowflake from parts",
  on-prem viable via self-hosted S3-compatible object stores (MinIO, Ceph
  RGW, SeaweedFS, Garage — see §10).
- On-prem note: storage–compute separation stopped being cloud-only; Doris
  3.0 (Jun 2024) and StarRocks 3.x shipped decoupled modes you can run
  yourself.

### G5 — embedded and composable (2019–)

- Systems: **DuckDB** (CWI, 2019 — the "SQLite of OLAP"; 1.5.x current, 1.4
  LTS added encryption, MERGE, Iceberg writes), **DataFusion** (donated to
  Arrow 2019, Apache TLP 2024; passed 1M monthly crates.io downloads Mar
  2026), **Velox** (Meta, 2022), **Polars** (2020; €18M Series A Sep 2025),
  **chDB** (embedded ClickHouse; chDB 4.0 Mar 2026), **GizmoSQL** (DuckDB or
  SQLite behind Arrow Flight SQL — a server shell around an embedded engine).
- Interchange: **Arrow** (2016) in memory; **Flight SQL** on the wire and
  **ADBC** as the driver API (adoption knee 2025–2026: Snowflake, BigQuery,
  DuckDB, Trino drivers; Microsoft moving Fabric connectivity to ADBC).
- Driving communities: data science and the single-node counterrevolution
  ("Big Data Is Dead", 2023 — most real datasets fit one big box now), plus
  Rust database-builders using DataFusion as the LLVM of databases
  (InfluxDB 3, GreptimeDB, Comet, dbt Fusion, Cloudflare, Spice.ai, LanceDB).
- This generation dissolved the boundary between "database" and "library",
  and made single-node on-prem analytics respectable again.

### G6 — current fronts (2023–)

- **Observability wide-events**: VictoriaLogs, ClickHouse-backed stacks
  (SigNoz; HyperDX, acquired by ClickHouse Mar 2025), Quickwit (acquired by
  Datadog Jan 2025; OSS repo still active Jul 2026), Grafana Loki. Elastic is
  responding in kind: `logsdb` index mode (Dec 2024), and new `columnar` /
  `logsdb_columnar` index modes in tech preview (9.5, 2026). OTel-Arrow makes
  the telemetry pipe itself columnar (~10x wire reduction; phase 2 in 2026).
- **Post-Parquet formats**: Vortex (LF AI & Data, incubation Aug 2025), Lance
  (AI/multimodal niche), Nimble (Meta, Velox-tied), FastLanes (CWI), BtrBlocks
  (TUM) — deep dive in §7. Parquet is *not* being displaced — it is absorbing the ideas
  (footer redesign WG, VARIANT + shredding finalized 2.12 Aug 2025, native
  geo types Feb 2026, float stats Jun 2026). **DuckLake** (May 2025; 1.0 Apr
  2026) attacks the *metadata* layer instead: lakehouse catalog as plain SQL
  tables, third-party clients for Spark/Trino/DataFusion already exist.
- **AI convergence**: vector indexes went GA across the board (ClickHouse
  25.8, Doris 4.0, StarRocks roadmap); MCP servers and text-to-SQL are now
  checkbox features; Spice.ai positions the query engine as agent
  infrastructure. Agent-facing analytics is the claimed next driving
  community — too early to call.
- **GPU second wave**: Kinetica (NVIDIA cuVS partner), HEAVY.AI (reportedly
  acquired by Nvidia, 2025 — weakly documented), SQream, Voltron Data's
  Theseus (US-gov channel via Carahsoft, Apr 2025), Polars GPU engine
  (cuDF-based). AI budgets revived a lane that had gone quiet.
- **Academic frontier commercialized**: TUM's Umbra became **CedarDB** (€5.3M
  seed; Community Edition May 2025; FSST compression Jan 2026, RBAC Mar 2026).

### G7? — the AI plant as data platform (prospective, 2025–)

A candidate next generation, not an established one: storage moving from
flat S3-like object stores toward random-I/O-optimized substrates that also
hold ML artifacts (training samples, checkpoints, KV caches). Driving
community, if it materializes: ML-infrastructure teams. Three concurrent
movements, unequally evidenced:

- **The S3 API decouples from S3 physics.** Object storage rebuilt for
  random I/O behind an unchanged interface: MinIO AIStor (S3-over-RDMA, S3
  Express API); NVIDIA GPUDirect RDMA for S3-compatible storage (MinIO,
  Cloudian — vendor-led, notably not AWS); AWS cutting S3 Express One Zone
  GET prices 85% (Apr 2025) and shipping S3 Vectors (GA Dec 2025); Google's
  Colossus-backed sub-ms zonal buckets GA'd 2026, marketed on checkpoint
  restore and GPU idle time. The API shows SQL-like survivorship — physics
  changes underneath, interface persists.
- **The AI plant absorbs analytics.** DeepSeek's 3FS (OSS Feb 2025, MIT):
  NVMe+RDMA parallel FS, random-read-first (read caching deliberately
  dropped as useless under shuffled training reads), FoundationDB metadata,
  6.6 TiB/s across 180 nodes; **smallpond** runs one embedded DuckDB per
  partition directly on it — the engine goes to the training data,
  reversing the move-data-to-the-warehouse habit, and thin precisely
  because G5 made engines into libraries. No adoption outside DeepSeek
  verified 17 months on. The commercial form of the same bet: VAST (native
  columnar "DataBase" layer; Trino running against it on NVMe, May 2025;
  ~$500M Series F at $30B valuation, spring 2026; $1.17B CoreWeave deal)
  and WEKA NeuralMesh (LLM KV caches persisted to NVMe). OSS analogs beyond
  3FS — Mooncake (Moonshot; KV cache, adopted by vLLM/SGLang), CubeFS,
  JuiceFS — are real but fragmented, and mostly Chinese-AI-lab-driven.
- **The format layer follows** (overlapping G6's post-Parquet front, §7):
  Lance is random-access-first with tensor/embedding types; Iceberg's File
  Format API (shipped in 1.11.0, May 2026) admits Lance-class formats;
  Parquet's footer redesign targets low-latency opens.

Precision matters: model *weights* are sequential blobs — plain object
storage holds them fine (S3 now accepts 50 TB objects). The genuinely
random workloads are shuffled sample fetch, KV-cache paging, vector and
point lookups, metadata opens. Counter-evidence: erasure-coded HDD object
storage stays unbeatable per TB for cold data, and the late-2025 NAND price
spike (75–125%) works against all-flash; JuiceFS documented an LLM shop
migrating *off* a parallel FS onto object-store-plus-cache; engine adoption
of AI-plant storage is so far inference-side (vLLM/SGLang → Mooncake), not
analytics-side — Trino-on-VAST is the lone strong analytics signal.

Likeliest end-state: explicit **tiering, not replacement** — object lake as
cold truth, a random-I/O NVMe/KV tier for samples, caches, and vectors, the
S3 API stretched over both. Tells that would confirm the generation: a
mainstream OLAP engine shipping first-class support for one of these
substrates, or a smallpond-style deployment surfacing outside an AI lab.

## 3. Lineages

Read `→` as "begat" (code fork, engine license, or founding team). The two
academic schools are the deepest roots in the field:

**Dutch school — CWI Amsterdam (vectorized execution):**

    MonetDB (1993–)
      → X100 / VectorWise (2005/2010) → Actian Vector → "Actian Analytics Engine" (v8, 2026 — alive)
      → (people) Snowflake (Żukowski, co-founder)
      → DuckDB (2019) → MotherDuck, pg_duckdb, DuckLake, GizmoSQL (server shell)
      → FastLanes, ALP float compression (research feeding Parquet/Vortex)

**Munich school — TUM (query compilation):**

    HyPer (2010) → acquired by Tableau 2016 (the "Hyper" engine)
    Umbra (2019) → CedarDB (2024, Postgres-compatible HTAP)
    research artifacts: BtrBlocks, FSST (string compression — now in DuckDB, CedarDB, Vortex)

**Stonebraker line:** C-Store (2005) → Vertica → HP (2011) → Micro Focus →
OpenText → **Rocket Software ($150M, closed May 2026)**. Sibling: ParAccel →
Amazon Redshift (2012).

**Google-papers diaspora:** Dremel (2010) → Parquet, Drill, BigQuery.
MapReduce/GFS → Hadoop → the whole of G2.

**Meta line:** Presto (2012) → **Trino** (2019 fork; Starburst) and
**PrestoDB** (Linux Foundation; IBM watsonx.data; "Presto 2.0" C++ rewrite on
Velox still not GA). Also ORC (with Hortonworks), Velox (2022 → Prestissimo,
Gluten), Nimble format.

**Yandex line:** ClickHouse (2016 OSS)
- forks/derivatives: TiFlash (TiDB's columnar replica), ByConity (ByteDance;
  quiet), Timeplus Proton (streaming), chDB (embedded; now in ClickHouse Inc);
  Firebolt began as a ClickHouse fork before rewriting (Firebolt Core, free
  self-hosted, Jun 2025 — free but not OSS)
- inspired-by (no shared code): VictoriaMetrics (2018, Go) → **VictoriaLogs**
  (GA Nov 2024) → VictoriaTraces (v0.10, Jul 2026, pre-1.0)
- ecosystem vendors: Altinity, Tinybird, Hydrolix (ClickHouse-compatible
  log lake); app layer: SigNoz, HyperDX (acquired Mar 2025), Langfuse
  (acquired Jan 2026)

**Baidu line:** Palo → Apache Doris (TLP 2022) → fork **StarRocks** (2020;
Linux Foundation 2023). Vendors: VeloDB (ex-SelectDB) vs CelerData. Both are
converging on lakehouse writes + AI features (Doris 4.x, StarRocks 4.1).

**LinkedIn line:** Pinot (sibling of Kafka) → StarTree (Series B 2022).

**Metamarkets line:** Druid (2011) → Imply — the oldest real-time OLAP
lineage, now visibly waning.

**Berkeley/AMPLab line:** Spark (2009) → Databricks. Spark's columnar story
is accelerators: Photon (closed), **Gluten**+Velox (Apache TLP Feb 2026),
**Comet** on DataFusion (0.16, May 2026), NVIDIA RAPIDS. Spark 4.2 (Jul 2026).

**Postgres world** (the "columnar Postgres solutions" cluster):

    Greenplum (2005) → closed by Broadcom May 2024 → Apache Cloudberry (incubating; 2.1.0 Apr 2026)
    cstore_fdw (Citus, 2014) → Citus columnar (2021 — stagnant since)
    Timescale → TigerData (renamed Jun 2025) — hypercore row→columnar compression
    pg_duckdb (DuckDB org + MotherDuck; v1.1.x) — DuckDB engine inside Postgres
    pg_mooncake (Mooncake Labs → acquired by Databricks Oct 2025 for Lakebase; repo still active)
    Crunchy Data Warehouse (DuckDB+Iceberg in Postgres) → Snowflake (Jun 2025, ~$250M)
    ParadeDB pg_analytics — deprecated; company refocused on pg_search
    Hydra — abandoned (~end 2025)
    AlloyDB Omni (Google, COTS on-prem Postgres + columnar engine — still pushed)

  Pattern: independents get absorbed by platforms; the pragmatic survivor is
  "embed DuckDB", the strategic play is "Postgres as the front-end protocol"
  (CedarDB, QuestDB, RisingWave all speak PG wire).

**Arrow/Rust school:** pandas (McKinney) + Drill (Nadeau) → **Arrow** (2016)
→ **DataFusion** → InfluxDB 3, GreptimeDB, Comet, Spice.ai, dbt Fusion
engine, Cloudflare pipelines, LanceDB. Also tantivy → Quickwit → Datadog.
Polars is Arrow-compatible with its own compute.

**GPU line:** GPUdb (~2012, US Army intelligence project) → **Kinetica**
(renamed 2016). MapD (2013) → OmniSci → HEAVY.AI (reportedly → Nvidia 2025).
BlazingSQL (dead 2021) → Voltron Data Theseus. Plus SQream (2010), PG-Strom,
and RAPIDS cuDF as the substrate (Polars GPU engine).

**Finance line:** kdb+ → KDB-X (GA + free Community Edition, Nov 2025);
imitators/descendants DolphinDB, Shakti; ArcticDB (Man Group, 2023);
QuestDB (kdb-adjacent inspiration; 10.0, Aug 2026).

## 4. Roster — the named systems, placed

| System | Gen | Lineage | Driving community | At-rest format | 2026 state (license · vendor · on-prem) |
| --- | --- | --- | --- | --- | --- |
| ClickHouse | G3 | Yandex | web analytics → observability, RT analytics | native MergeTree; Iceberg r/w | Apache-2 · ClickHouse Inc ($15B, Jan 2026) · single binary, excellent on-prem |
| Apache Pinot | G3 | LinkedIn | user-facing metrics at consumer scale | native segments | Apache-2 · StarTree · JVM cluster + ZK/Kafka |
| Apache Druid | G3 | Metamarkets | ad-tech clickstream | native segments | Apache-2 · Imply · waning contributor base |
| Apache Doris | G3 | Baidu | Chinese internet RT-OLAP; lakehouse | native tablets; Iceberg r/w | Apache-2 · VeloDB (ex-SelectDB) · FE/BE, decoupled mode GA |
| StarRocks | G3 | Baidu (Doris fork) | customer-facing analytics, lakehouse | native; Iceberg writes (4.0) | Apache-2 (LF) · CelerData · shared-nothing or shared-data |
| DuckDB | G5 | CWI | data science, "small data", embedded | own file; Parquet/Iceberg/DuckLake | MIT · DuckDB Foundation/Labs · in-process, trivially on-prem |
| Umbra / CedarDB | G6 | TUM | academic frontier; HTAP | native | proprietary, free Community Ed · CedarDB GmbH · PG-compatible, on-prem |
| GizmoSQL | G5 | CWI (DuckDB shell) | composable/edge deployments | DuckDB or SQLite | Apache-2 core + commercial · GizmoData (single-maintainer scale) · Flight SQL server |
| DataFusion | G5 | Arrow/Rust | database *builders* | Parquet (via ecosystem) | Apache-2 · ASF · library, not a server |
| Spice.ai | G6 | Arrow/Rust (DataFusion) | AI-agent builders | federated; accel into DuckDB/SQLite/Arrow | Apache-2 · Spice AI (~$14.5M) · self-hostable runtime |
| VictoriaLogs | G6 | Yandex-inspired | observability cost rebellion | custom columnar | Apache-2 · VictoriaMetrics (bootstrapped) · single binary + cluster |
| Trino | G4 | Meta (Presto fork) | lakehouse / federated BI | none (Parquet+Iceberg) | Apache-2 · Starburst · JVM cluster; release cadence slowed 2026 |
| Presto | G2/G4 | Meta | legacy Facebook-scale + IBM watsonx | none (Parquet/ORC) | Apache-2 · LF/IBM · C++ rewrite still pre-GA |
| Apache Impala | G2 | Cloudera | Hadoop enterprises | Parquet/ORC/Kudu | Apache-2 · Cloudera · dormant (last release Mar 2025) |
| Apache Spark | G2 | Berkeley | enterprise ETL | Parquet + Delta/Iceberg | Apache-2 · Databricks et al. · columnar via Gluten/Comet plugins |
| Kinetica | G6 | GPU (GPUdb) | geospatial / defense / telco | native | COTS, free dev edition · Kinetica · on-prem incl. classified envs |
| Vertica | G1 | C-Store | enterprise DW | native (ROS) | COTS · **Rocket Software** (May 2026) · on-prem stalwart, watch the transition |
| Exasol | G1 | independent | enterprise BI (DACH) | native in-memory | COTS · Exasol AG (listed) · on-prem MPP |
| Greenplum/Cloudberry | G1 | Postgres MPP | ex-Greenplum installed base | native heap/AO-columnar | Apache-2 (incubating) · community · Broadcom orphaned the original |
| kdb+ / KDB-X | G0 | APL/k | quant finance | native | COTS, free community ed (Nov 2025) · KX · on-prem standard in trading |
| MonetDB | G0 | CWI | research, niche | native BAT | MPL · MonetDB Solutions · alive, niche |
| SAP HANA / IQ, MS SQL columnstore, Db2 BLU, Oracle In-Mem, Teradata | G1 | incumbent retrofits | captive enterprise estates | native | COTS · strong on-prem, zero OSS gravity |
| Actian "Analytics Engine" (ex-Vector) | G1 | CWI (X100) | residual enterprise | native | COTS · Actian/HCL · alive (v8, 2026) |
| SingleStore | G1.5 | MemSQL | HTAP enterprise | native columnstore | COTS · Vector Capital (PE, 2025) · self-managed exists |
| InfluxDB 3 / QuestDB / GreptimeDB / TDengine | G6 | Arrow-Rust / finance / — | time series, IoT | Parquet (Influx/Greptime), native (QuestDB, TDengine) | mixed OSS+enterprise · Influx 3 migration friction; QuestDB 10.0 Aug 2026 |
| HEAVY.AI / SQream / Theseus | G6 | GPU | GPU analytics, gov | native / Parquet | COTS-ish · HEAVY reportedly → Nvidia; Theseus sells via Carahsoft |
| Databend | G4 | Snowflake-alike (Rust) | object-store-native DW | native on S3, Parquet | Apache-2 · Databend Labs · self-hostable |
| TiFlash / ByConity / Timeplus Proton | G3 | ClickHouse forks | HTAP replica / internal cloud / streaming | native | Apache-2 · PingCAP / ByteDance (quiet) / Timeplus |
| Elasticsearch / OpenSearch / Quickwit / Loki | adjacent | Lucene & co. | observability, search | inverted index (+ new columnar modes) | Elastic 9.5 ships columnar index modes (preview) — the search lineage converging on columnar |

## 5. Driving communities

The generations map almost one-to-one onto communities:

- **Enterprise BI / DW** (G1): sticky, shrinking-share, on-prem by default —
  Vertica, Exasol, Teradata, HANA. M&A churn (Vertica → Rocket) signals
  harvest mode, not growth.
- **Hadoop diaspora** (G2): migrated to lakehouse; the engines (Impala, Drill,
  Kylin) were left behind, the *format habit* (Parquet) is the inheritance.
- **Ad-tech / clickstream / user-facing analytics** (G3): sub-second serving,
  high QPS — Druid → Pinot/ClickHouse; the demanding tail keeps native
  formats alive.
- **Chinese internet platforms** (G3): Doris/StarRocks; vendor-driven OSS,
  fastest feature cadence (vector, lakehouse writes, AI functions).
- **Platform engineering** (G4): Trino + Iceberg + object store as the
  buildable Snowflake; catalogs now interoperable via Iceberg REST.
- **Data science / single-node** (G5): DuckDB and Polars; "most data fits one
  box" is now the default assumption, which quietly favors on-prem.
- **Database builders** (G5): DataFusion/Velox/Arrow — the community *is*
  other databases; fastest-compounding lineage of the decade.
- **Observability / SRE** (G6): the loudest current driver — wide events,
  OTel, cost rebellion against Elastic/Datadog pricing. ClickHouse,
  VictoriaLogs, Loki, Quickwit; Elastic counter-attacking with columnar modes.
- **Quant finance** (G0, evergreen): kdb+/KDB-X, DolphinDB, ArcticDB, QuestDB.
- **Geospatial / defense / GPU** (G6): Kinetica, Theseus, HEAVY.AI —
  procurement-driven, on-prem-mandatory, revived by AI hardware budgets.
- **AI-agent builders** (G6, emergent): Spice.ai, LanceDB, MCP-everywhere,
  vector-in-OLAP. The claimed next generation-driver; unproven.

## 6. Data formats

- **At rest, open**: Parquet is the lingua franca and is consolidating, not
  dying — VARIANT + shredding (format 2.12, Aug 2025), native geospatial
  types (Feb 2026), ordered float stats (2.13, Jun 2026), footer-redesign
  working group. ORC is in maintenance mode. Challengers occupy niches:
  Lance (multimodal/vector AI), Vortex (LF incubation), Nimble (Meta/Velox),
  with FastLanes/BtrBlocks as the research feeders — deep dive in §7.
- **Table metadata**: Iceberg v3 ratified (mid-2025), v4 underway; Delta
  converging onto Iceberg's metadata; Hudi third; Paimon owns streaming-
  native. DuckLake's contrarian bet: catalog state belongs in a SQL database,
  not in JSON-on-object-store.
- **Engine-native**: MergeTree parts, Pinot/Druid segments, Doris/StarRocks
  tablets, Vertica ROS, kdb+ splayed tables — still the price of admission
  for sub-second serving; now usually paired with open-format import/export.
- **In memory**: Arrow won outright; Velox vectors and DuckDB vectors are
  near-Arrow with conversion at the edges.
- **On the wire and at the driver**: Arrow Flight SQL (wire protocol) and
  ADBC (driver API — the columnar successor to JDBC/ODBC) hit their adoption
  knee (2025–2026); the Postgres wire protocol is the other de-facto
  standard — systems that are not Postgres speak it anyway (CedarDB, QuestDB,
  RisingWave, Materialize).
- **Telemetry**: OTel-Arrow makes the pipe columnar end-to-end (phase 2,
  2026) — a hint that "columnar" is escaping the database into the transport.

## 7. Next-generation file formats (the post-Parquet front)

Three pressures pulled this front open around 2023. **Hardware**: NVMe and
fast object storage moved the bottleneck from I/O to decode CPU — Parquet's
design point (HDFS-era disks, heavyweight general-purpose compression) is the
wrong trade now. **Workloads**: AI access patterns are random — shuffled
sample fetch, vector probes, point lookups — and row-group granularity fights
all of them. **Governance**: Parquet's compatibility contract freezes its
encoding set (every reader must implement every encoding), so a decade of
compression research (ALP, FSST, FastLanes) had nowhere to land. One
terminology guard: **Velox is an execution library (G5), not a format** — its
at-rest format is Nimble.

| Format | Origin (lineage) | The bet | State, Aug 2026 |
| --- | --- | --- | --- |
| **Vortex** | SpiralDB 2024 → LF AI & Data incubation (Aug 2025); Rust; CWI research inside | extensible encoding registry, compute pushed onto compressed data, Arrow-compatible memory — the general-purpose successor claim | file format backwards-compatible since v0.36; official DuckDB extension, read + write (Jan 2026); DataFusion / Spark / Polars bindings; first new format being wired through Iceberg's File Format API |
| **Lance** | LanceDB, 2022; v2 2024 | random access as the design centre — no row groups; file + table format with secondary (vector/ANN) indexes in-format; tensor types | format 2.1 stable (Oct 2025); DuckDB SQL over Lance datasets, vendor-reported ~1.5M IOPS on S3 (early 2026); Apache Polaris generic-table integration (Jun 2026) |
| **Nimble** | Meta, OSS Apr 2024; decode via Velox | very-wide ML feature tables (thousands of columns); pluggable encodings; stream-oriented layout | Meta-reported 2–3× ORC decode speed; little visible adoption outside Meta |
| **F3** (research name "FFF") | Tsinghua + CMU + Wisconsin + Wes McKinney; SIGMOD 2026 paper | future-proof the format *itself*: each file embeds WASM decoders (~150 KB per encoding), so the encoding set can evolve without coordinating every reader | research artifact; WASM decode 15–35% off native — a portability floor under native fast paths, not a replacement for them |
| **FastLanes** | CWI; layout paper VLDB 2023, file format VLDB 2025 | one transposed 1024-value virtual-vector layout so decode saturates SIMD/GPU regardless of register width | research format; the layout and ALP already ship inside Vortex (and DuckDB) |
| **BtrBlocks** | TUM, SIGMOD 2023 | cascaded lightweight encodings chosen by sampling; decompression speed over ratio | research; effectively absorbed — FSST and the cascade idea are everywhere downstream |

What changed in 2026 is distribution, not design. A challenger format's
chicken-and-egg problem (no readers → no data → no readers) got two
structural exits this year:

- **The embedded-engine route.** DuckDB ships Vortex read/write (official
  extension, Jan 2026) and SQL over Lance — the G5 embeddable engine acts as
  a universal adapter that hands a new format an installed base overnight.
- **The lakehouse route.** Iceberg's File Format API shipped in 1.11.0 (May
  2026): the file layer under Iceberg tables is now pluggable, and Vortex is
  the first new format being integrated through it. The G4 neutrality
  settlement extends one layer down.

Readings:

- Parquet is running the incumbent playbook (§6): absorb the features
  (VARIANT, geo, float stats), fix the bottlenecks (footer redesign), and let
  compatibility gravity hold the batch-analytics core. For scan-shaped work
  that likely holds.
- The wedge absorption cannot close is **random access**: Lance's
  no-row-groups layout and in-format ANN indexes serve point lookups that
  Parquet's architecture fights — which is why Lance owns the AI-retrieval
  niche rather than competing head-on for scans.
- The research pipeline is the same two schools as the engine market (§3):
  CWI (FastLanes, ALP → Vortex, DuckDB) and TUM (BtrBlocks, FSST →
  everywhere). The lineage map extends to the format layer intact.
- Tells that this front matures into a generation: a G3 engine (ClickHouse,
  Doris, StarRocks) shipping a post-Parquet reader; an Iceberg release
  enabling a second pluggable format by default; F3's embedded-decoder idea
  surfacing inside Parquet's own evolution.

## 8. Market dynamics, 2024 – mid-2026

Consolidation accelerated sharply; the buyers are platforms, the sellers are
single-product independents:

- ClickHouse Inc: $350M Series C (May 2025, $6.35B) → **$400M Series D
  (Jan 2026, ~$15B)**; acquired PeerDB (2024), HyperDX (Mar 2025), LibreChat
  (Nov 2025), Langfuse (Jan 2026); launched managed Postgres with CDC.
- Databricks: Tabular (Jun 2024), Neon (~$1B, May 2025), Mooncake Labs
  (Oct 2025). Snowflake: Crunchy Data (~$250M, Jun 2025), Select Star
  (Nov 2025). Both hyperscaled into Postgres.
- **Fivetran + dbt Labs merged** (announced Oct 2025, completed Jun 2026);
  dbt Fusion engine is Rust on DataFusion — the transformation layer joined
  the Arrow/Rust lineage.
- **SAP acquired Dremio** (announced May 2026, completed Jul 2026) — a G4
  independent absorbed by a G1 incumbent.
- **Vertica divested** by OpenText to Rocket Software ($150M, closed May
  2026) — the C-Store lineage is now PE-portfolio infrastructure.
- IBM completed the $11B Confluent acquisition (Mar 2026); Datadog took
  Quickwit (Jan 2025); Cloudflare took Arroyo (Apr 2025); Vector Capital took
  SingleStore private-er (Sep 2025); HEAVY.AI reportedly folded into Nvidia.
- Independent and bootstrapped holdouts: VictoriaMetrics (zero external
  funding), DuckDB (foundation + services company), Exasol (listed),
  Materialize (still independent), QuestDB, Polars (€18M A, Sep 2025).

Technical currents:

- Real-time engines gained open-format *writes* (ClickHouse 25.8+/26.2,
  StarRocks 4.0/4.1, Doris 4.1) — "fast native serving + open lake archive"
  in one system is the new normal.
- JVM engines are racing to vectorize: Trino "Project Hummingbird"
  (incremental, no GA milestone), Presto C++ (still pre-GA). Meanwhile Trino
  release cadence visibly slowed in 2026 — worth watching.
- The AI checkbox arrived everywhere: vector indexes GA (ClickHouse 25.8
  HNSW, Doris 4.0), `ai_query`-style SQL functions (StarRocks 4.1, Doris),
  MCP servers from most vendors.
- Single-node keeps winning benchmarks-per-dollar; DuckDB/chDB/Polars soak
  up workloads that used to justify clusters.

## 9. Erasure under GDPR / nFADP — time travel's legal counterparty

Time travel (§2 G4, §7) and legally mandated erasure are one mechanism seen
from opposite sides: a format whose contract is "old versions stay readable"
is a format whose contract is "deleted data stays readable". This section
reads the tension through Iceberg as the settlement winner (§4), but the
property belongs to the snapshot-log architecture — Delta, Hudi, Paimon and
DuckLake inherit it identically. Engineering-grade legal context, not legal
advice.

The legal floor, compressed:

- **GDPR Art 17** grants erasure on enumerated grounds, "without undue
  delay" (read against Art 12(3)'s one month, extendable). **EDPB 02/2025**
  (blockchain guidelines, Apr 2025) forecloses the architectural defence:
  "technical impossibility cannot be invoked" — immutability-by-design is
  not an excuse. **WP216** (2014, still the anonymisation bar) rates
  encryption-plus-key-destruction as *pseudonymisation*, not anonymisation —
  crypto-shredding alone does not satisfy Art 17 on a strict reading.
  **EDPB CEF 2025** (right-to-erasure enforcement report, Feb 2026) allows
  deferred erasure in backups but demands it apply on restore and be
  demonstrable.
- **Swiss nFADP** (in force Sep 2023) is materially more permissive on
  exactly these axes: destruction *or anonymisation* satisfies both the
  storage-limitation duty (Art 6(4) — which binds with no request on file)
  and the erasure remedy (Art 32(2)(c)); identifiability is an effort test
  (BGE 136 II 508); deletion means "prevents restoring under normal
  circumstances", so key-destruction plausibly does clear the Swiss bar.
  The FDPIC has published no guidance on append-only/distributed systems
  (checked 2026-05) — such architectures are unsupervised by
  sector-specific guidance in Switzerland.
- Post-**CJEU *EDPS v SRB*** (Sep 2025) both regimes converge on a
  recipient-perspective identifiability test: tokenized copies held by a
  party without the key can be non-personal data *for that party* — the
  legal foundation under personal-data-vault and tokenization patterns.

What a delete actually is in Iceberg:

- **Logical first.** `DELETE` writes v2 position/equality delete files or
  v3 deletion vectors; data files are untouched. Two self-defeating
  details: an *equality* delete file carries the matched values by
  definition — the daily GDPR delete batch that vendor guidance recommends
  accumulates files listing exactly the identifiers to be forgotten — and
  v2 *position* delete files may embed copies of the deleted rows (v3
  deletion vectors close that one).
- **History keeps everything reachable.** Every earlier snapshot still
  references the pre-delete files; until they expire, the data is one
  `VERSION AS OF` away. Physical erasure is a pipeline, not an operation:
  delete → compaction (merge-on-read tables must materialize deletes
  first) → `expire_snapshots` past the write → `remove_orphan_files`.
- **Metadata leaks.** Manifests carry per-column lower/upper bounds; a
  value at a file's edge sits in metadata verbatim (long strings
  truncated), so stats-bearing manifests carry personal data themselves
  until rewritten.
- **The grain is wrong for subjects.** Snapshot expiry is the only
  reach-back into history, and it is timeline-grained: expiring snapshots
  to erase one subject destroys time travel over that window for *every*
  subject. A per-subject redaction that leaves surrounding history intact
  does not exist at spec level, and none is publicly proposed (checked
  Aug 2026). Table encryption landed in 1.11 (May 2026) as envelope
  encryption — per-file DEKs wrapped by a *table* master key — so
  key-shredding erases tables, not subjects; subject-grain keys remain an
  application-layer construction (and WP216 caps what they prove under
  GDPR either way).
- **The perimeter is out of reach.** Object-store versioning, cross-region
  replication and backups hold file copies the table format never
  references and cannot erase; CEF 2025's backup position lands on the
  operator, not the format.

The operating envelope follows: snapshot retention = time-travel horizon =
erasure-latency floor, plus whatever the perimeter adds. Compliant
deployments square it by spending the headline feature — days-not-months
retention on tables bearing personal data plus scheduled hard-delete
pipelines — or by keeping personal data out of the lake entirely.

**The master-data reading.** MDM is where this bites hardest, because
master data is simultaneously the class where personal data concentrates
(customer, employee,
patient, supplier), the most history-hungry (SCD lineages, survivorship and
merge provenance *are* the product), the longest-lived, and the primary
target of subject-rights requests. It needs surgical per-subject erasure
with otherwise intact history — precisely the operation the snapshot model
cannot express. It is also small (§5's one-box point): nobody needs a
lakehouse for master data's *volume*; the lake provides distribution.
Practice therefore converges on the pattern the case law now rewards
(*SRB*): the system of record for master data stays an operational,
mutable, erasable store — personal data vaulted or tokenized — and the lakehouse
carries token-keyed projections. The "lakehouse absorbs everything"
narrative (§2 G4) has a governance-shaped boundary exactly at master data.

The symmetry deserves stating once: mutable engines forget cheaply and
travel poorly; snapshot formats travel cheaply and forget poorly. Designs
that treat history *and* forgetting as first-class data-level operations —
vault-by-design personal-data segregation, patch-based stores with compensating
forget operations over content-addressed history — exist in VCS theory and
niche systems, not in mainstream OLAP.

## 10. Reading the field for on-prem work

- Two architectures look durable on-prem: the **single-binary integrated
  store** (ClickHouse, Doris/StarRocks, VictoriaLogs, DuckDB at the small
  end) and the **composable lake stack** (Trino/Spark/DataFusion over
  Parquet+Iceberg on MinIO-class storage). The Hadoop-era middle ground is
  gone; the G1 COTS estate persists but no longer accretes new workloads.

  "MinIO-class storage" = self-hosted S3-compatible object storage — the
  piece that made the composable stack portable off the cloud. OSS: MinIO,
  Ceph RGW, SeaweedFS, Garage. COTS appliances: Dell ECS, NetApp
  StorageGRID, Cloudian, Scality, Pure FlashBlade. Engines see only an
  endpoint URL and credentials, so the G4 architecture runs on private racks
  unchanged. Caveat: MinIO itself is currently the least settled member of
  its own class — during 2025 it stripped management features from the
  community edition in favor of its commercial AIStor line, pushing part of
  its community toward Ceph, Garage, and SeaweedFS.
- Format risk is at a historic low: Parquet + Iceberg + Arrow are shared,
  actively converging, and vendor-neutral (ASF/LF). Engine choice is
  increasingly reversible; data outlives engines.
- Lineage health is a better predictor than feature lists: CWI and
  Arrow/Rust lineages are compounding (DuckDB, DataFusion everywhere); the
  Baidu line ships fastest; the Metamarkets and Hadoop lines are in decline;
  the C-Store line just changed hands.
- License posture in this field is unusually clean — the load-bearing engines
  are Apache-2/MIT with foundation governance (contrast the BSL wave that hit
  streaming and app databases). The COTS exceptions (Kinetica, CedarDB,
  Firebolt Core, KDB-X) all now ship free tiers to stay in the conversation.
- Watch items: whether "agent-facing analytics" becomes a real generation
  driver or stays marketing; whether Elastic's columnar modes blunt the
  ClickHouse/VictoriaLogs observability rebellion; DuckLake vs Iceberg-REST
  catalog gravity; Trino's slowed cadence; Vertica under Rocket; whether a
  post-Parquet format crosses from the DuckDB / Iceberg-plugin routes (§7)
  into a G3 engine's format roster; whether AI-plant storage (G7?) earns
  first-class OLAP-engine support beyond Trino-on-VAST, or a smallpond-style
  pattern appears outside an AI lab; whether any open table format grows a
  spec-level, subject-grain redaction primitive (§9).

---

*Local note: this repo sits on the Yandex line (ClickHouse via leeway, G3)
with G5 touchpoints (Parquet/Arrow interchange). The currents that matter
here: Iceberg-write maturity in ClickHouse, OTel-style wide events, and
agent-facing SQL surfaces. On the format layer (§7), the interchange bet
stays Parquet + Arrow until a post-Parquet reader reaches the G3 engines; the
random-access-first designs are the ones to watch against leeway's
point-lookup ambitions. Market background for the substrate premises in
[why-boxer](../explanation/why-boxer.md) and for the leeway read-surface work
([ADR-0171](../adr/0171-leeway-sql-read-surface.md)). The §9 erasure analysis
condenses the legal groundwork of
[ADR-0025](../adr/0025-pushout-forget-architecture.md) /
[ADR-0027](../adr/0027-pushout-forget-swiss-fadp.md) and the taxonomy in
[erasure-design-space](../explanation/erasure-design-space.md); it is also
the counterpoint behind the local time-travel direction — history modelled
as data on an append-only substrate with first-class forget, rather than
frozen into storage snapshots.*

*Method: prior knowledge to Jan 2026; five web fact-check passes on
2026-08-06 (corporate events; formats/standards; long-tail systems; release
status; AI-plant storage substrate); a sixth pass on 2026-08-09 for §7
(next-generation file formats); a seventh pass on 2026-08-10 for §9
(Iceberg delete/expiry mechanics and encryption status — the legal rows in
§9 are condensed from the ADR-0025/0027 groundwork, not re-verified here).
Facts that could not be verified were omitted or marked "reportedly".
Primary sources preferred: vendor blogs, ASF/LF announcements, GitHub
releases.*
