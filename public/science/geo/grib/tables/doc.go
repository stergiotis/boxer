// Package tables carries the WMO's machine-readable GRIB2 code tables,
// flag tables and template layouts, and the common table of originating
// centres, as data (ADR-0292 §R8). A parameter is always its coded
// triplet; the name a table gives it is a lookup that may say unknown,
// and the local ranges 192–254 are never named.
//
// The tables under wmo/ are derived from the wmo-im/GRIB2 and wmo-im/CCT
// repositories by [GenerateE] — every code, flag and template CSV
// reduced to one tab-separated file per kind, stamped with the source
// version — and embedded; wmo/SOURCES.txt records the origin and licence.
// [Version] is the GRIB2 master table version the data came from. To move
// to a newer version, check out the two repositories beside the corpus and
// run TestGenerateWmoTables with BOXER_GRIB_CORPUS set.
package tables
