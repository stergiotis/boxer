// Package recordstoretest holds executor building blocks for code that runs
// a generated record store over something other than a ClickHouse server:
// unit tests, and in-memory backends. It does not import the testing
// package.
//
// Two hazards motivate it (ADR-0387, Consequences):
//
//   - A store reads system.tables and system.columns when it is judged
//     (JudgeSchema, VerifySchema, a host's schema check). An executor that
//     answers no catalog read makes every table look absent, and a check
//     that requires an ok verdict refuses to open the store.
//     [CatalogExecutor] answers those two reads from the CREATE TABLE
//     statements it has been sent.
//   - A store inserts through recordstore.InsertArrowWith, which prefers an
//     executor's InsertArrowWith (recordstore.InsertSettingsI) over its
//     InsertArrow. A wrapper that embeds a concrete executor and overrides
//     InsertArrow is skipped, because the promoted InsertArrowWith reaches
//     the embedded executor directly. [HookExecutor] routes both insert
//     seams through the one hook a wrapper sets.
package recordstoretest
