---
type: how-to
audience: developer or operator putting a file tree into the lading store
status: draft
# reviewed-by: "@<handle>"   # fill in and uncomment when flipping to stable
# reviewed-date: YYYY-MM-DD  # fill in and uncomment when flipping to stable
---

> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.

# How to snapshot a file tree and query it

The task-oriented walk through the lading store: provision it, snapshot a
tree, then read that snapshot back three ways — as an `io/fs.FS` in Go, as SQL,
and as a file system rclone can mount. What the store *is* and why it is shaped
this way is [ADR-0198](../adr/0198-fs-snapshot-store.md); this page is the order
to do things in, and what the failures look like.

One distinction carries the page. **A snapshot is written once and never
changes.** There is no update path anywhere — not in the walker, not in the
adapter, not over SFTP — so every "modify" question below has the same answer:
take another snapshot. That is what makes the reads cacheable, the retention
declarative, and an interrupted walk something you can ignore rather than clean
up.

Package names are `lading*` and the ClickHouse tables are `boxer.fs*`. The two
answer different questions: `lading` is which subsystem owns them, `fs` is what
they hold, and it is the table names a query author sees.

To read the page by running it, [`scripts/dev/lading-demo.sh`](../../scripts/dev/lading-demo.sh)
does §1 to §3 against this repository — the tables, then five mounts: the
tracked tree, `doc/`, the `lading` packages, the checkout metadata-only, and a
generated tree carrying the edge cases §9 lists. It prints §5's and §6's read
paths with the ids filled in, writes a file of §5's queries with this run's
mounts and snapshots in them, and `lading-demo.sh purge` takes it back out.

## 1. Provision the tables

Once per store, and idempotently at every start:

```go
err := lading.Provision(ctx, exec, ladingschema.ProfileCorpus)   // creates + finishes
err = lading.Verify(ctx, exec)                                    // checks the live shape
```

`exec` is any `recordstore.ExecutorI` — `storeexec.New(chclient…)` against a
server, or `chexec.NewLocalExecutor` for a test. Provision creates
`boxer.fsmeta`, `boxer.fsdata` and `boxer.fssnap`, adds the materialised tree
columns, the path constraint and the directory skip index, and installs the
view that fills the snapshot index. `Verify` is not optional in a long-running
process: `CREATE TABLE IF NOT EXISTS` succeeds against any older shape, and the
store decodes positionally.

**The profile is fixed at creation.** `ProfileCorpus` (few mounts, large files:
one block per mark, 1 MiB blocks, per-block digests) and `ProfileFleet` (very
many small trees) differ only in table parameters. A table that already exists
keeps the profile it was made under — changing it is a migration, not a
restart.

## 2. Get a mount id

A mount is identified by **a tagged id your application mints under its own
tag** ([ADR-0106](../adr/0106-identity-fibonacci-tags-build-tag-retirement.md)). The store claims no tag,
mints no id, never inspects the body and never assumes a tag width; it carries
what you give it. Two applications' mounts can share one store and cannot
collide, because the code is prefix-free.

Practically: claim a tag with `tagmint`, mint under it, and keep the id. Every
surface below accepts it in the spellings that suit them — a bare `uint64` in
Go, decimal or `0x`-prefixed in SQL and on the command line, 16 hex digits as a
directory name over SFTP.

Name→id resolution is yours, not the store's. `ladingingest.RecordPolicy`
writes a mount's declared policy — name, store, retention class, inline
threshold, text rule — to `boxer.facts` as a registry you can read back, and it
is deliberately *not* called by `Snapshot`: a policy is edited rarely, a
snapshot happens often, and calling it per walk turns a registry into a log.

## 3. Take a snapshot

```go
pol := ladingingest.DefaultPolicy()      // 30 days, 4 MiB inline, newline-cut text
pol.Ttl = ladingingest.TtlClass7d

res, err := ladingingest.Snapshot(ctx, os.DirFS("/some/tree"), mount, pol,
    lading.Stores{Meta: metaStore, Data: dataStore})
```

`res` carries the snapshot's instant (`res.Snap` — the name you pin to later),
its expiry, and the walk's totals. Any `fs.FS` works: `os.DirFS`, an
`embed.FS`, a `zip.Reader`, an `fstest.MapFS`, a capability grant, or the
rclone source of §7.

The same walk from the command line, for a tree the process can read:

```sh
boxer fs snapshot --mount 0x3BFE363BCF148002 --name my-tree --ttl-days 7 /some/tree
boxer fs snapshot --mount 0x3BFE363BCF148002 --remote s3:bucket/prefix   # any rclone remote (§7)
```

`--name` also writes the policy record, so the `lading` sqlapplet book and the
browser show the mount by that name; `--meta-only`, `--inline-max`,
`--text-rule` and `--profile` mirror the policy fields above, and the tables are
provisioned idempotently first unless `--no-provision` is given. The command
prints the snapshot's instant and the walk's totals.

What the policy decides:

| Field | Effect |
|---|---|
| `Ttl` | retention class, **in whole days**. Rows expire at the end of the snapshot's day plus the class. |
| `InlineMax` | files up to this size have their content stored; larger ones record `ref` — size, mtime and hash, no bytes. It is also the walker's memory bound per file. |
| `Text` | `TextRuleSniff` cuts text at newlines; `TextRuleNever` cuts everything at fixed offsets. |
| `MetaOnly` | stat only: no blocks at all, one row per node. |
| `Profile` | block size, and whether each block carries its own BLAKE3 digest. |

**Whole days is a constraint, not a style.** The tables partition by expiry day
and drop whole parts; a sub-day class would leave partitions partially expired,
and a partially expired part keeps its expired rows through every background
merge. `Policy` refuses a zero class for the same reason.

**A failed walk needs no cleanup.** The root row is written last, alone, after
everything else is durable — and a snapshot is complete exactly when that row
exists. A walk that dies half way leaves rows no query can reach, which `TTL`
then removes. Retry by taking a new snapshot; there is nothing to roll back.

**A node that cannot be read becomes a row, not an error.** Its `err` column
carries the failure and the walk continues, so a tree with one unreadable
directory is still a snapshot and the failure is queryable:
`SELECT path, err FROM fs(m) WHERE err != ''`.

## 4. Read it back in Go

```go
view, found, err := ladingadapter.OpenLatest(ctx, exec, stores, mount)
data, err := fs.ReadFile(view, "a/b.txt")
```

The result is an ordinary `fs.FS` — `StatFS`, `ReadDirFS`, `ReadFileFS`,
`GlobFS`, `ReadLinkFS`, `SubFS`, with a `File` that is also an `io.ReaderAt`
and an `io.Seeker`. `testing/fstest.TestFS` passes against it.

Pin a specific snapshot with `ladingadapter.Open(stores, mount, snap)`;
`Snapshots(ctx, exec, mount)` lists the complete ones newest-first, and
`Mounts(ctx, exec)` lists the store's mounts. Both read the snapshot index, so
they cost one row per snapshot rather than a scan.

**The view cannot shift under you.** Every query it issues carries the mount
and the snapshot, so a walk finishing while you read changes nothing you can
see. That is also why its caches never invalidate.

Two reads fail on purpose rather than returning zero bytes:

- `ErrNoContent` — a directory, a symlink, or a file under a metadata-only
  policy. It lists and stats; it has no bytes.
- `ErrReferenced` — a `ref` entry, whose content was above the inline
  threshold. Supply a `SourceFetcherI` through
  `ladingadapter.WithSourceFetcher` to serve those from wherever the original
  still lives; without one the store will not pretend.

## 5. Query it in SQL

Three table-function macros, expanded before the statement leaves the process:

```sql
SELECT path, size FROM fs(0x3BFE363BCF148001) WHERE NOT is_dir ORDER BY size DESC LIMIT 20
```

| Macro | Grain | Columns |
|---|---|---|
| `fs(m[, snap])` | one row per node per snapshot | `mount`, `path`, `snap`, `expires_at`, `node_kind`, `content`, `mode`, `block_size`, `blocks`, `size`, `mtime`, `link_target`, `err`, `content_hash`, `text`, `name`, `dir`, `depth`, `ext`, `is_dir`, `is_symlink` |
| `fsdata(m[, snap])` | one row per stored block | `mount`, `path`, `seq`, `snap`, `expires_at`, `data`, `hash`, `line0` |
| `fssnap(m)` | one row per complete snapshot | `mount`, `snap`, `expires_at`, `snap_entries`, `snap_bytes`, `ttl_class`, `text_rule`, `inline_max` |

The second argument selects the snapshot: omitted means the newest complete
one, `'*'` means all of them, a **string** is a datetime literal, and a
**number** is Unix nanoseconds. Those are not interchangeable — a plain number
handed to `toDateTime64` is read as *seconds* whatever the scale says, so
nanoseconds saturate to the year 2262 and match nothing. The expansion uses
`fromUnixTimestamp64Nano`; if you build such a literal by hand, do the same.

In `play` the macros expand through the standard pre-execute pass registry, so
typing one into the SQL editor works with nothing to wire. Elsewhere, run a
statement through the expansion directly:

```go
sql, err := ladingsql.Expand(ladingsql.Config{Visibility: vis}, userSQL)
```

A host that applies the registry instead binds a `MountVisibilityI` into the
value it hands `ApplyBestEffortBound`; without one the factory declines, the
macro is left alone, and the server answers "unknown table function `fs`". That
is deliberate — an expansion is an authorisation decision, so a host that
states no policy gets no expansion rather than a default one.

`Visibility` decides which mounts the statement may read, and **nil refuses
every mount** — a capability check that defaults to open is not one. Use
`VisibleSet{…}` for an explicit list, `VisibleUnderTag{…}` for "every id under
this tag", or `VisibleAll{}` where the caller has already decided. A mount you
may not read is refused at expansion rather than filtered, because "no rows"
and "not yours" are different answers.

What the expansion carries that you never type: the newest snapshot resolved
from the index (so it can only be a *complete* one), `expiresAt > now()` on the
same column the `TTL` names (so results and disk usage can only diverge in disk
usage), and the visibility check.

Worked queries — grep with real line numbers, history, diff between two
snapshots, `du`, identical content, the block audit — are ADR-0198 §7, and each
one runs as a test in `ladingsql`'s integration lane.

## 6. Mount it with rclone

The store speaks SFTP on a pipe. rclone runs the head in place of ssh, so
there is no socket, no port and no credential — possession of the pipe is the
authorisation:

```sh
rclone mount --read-only \
  ':sftp,ssh="boxer fs sftp-stdio --mount 0x3BFE363BCF148001",shell_type=unix:/3bfe363bcf148001/latest' \
  /mnt/x
```

`shell_type=unix` is worth setting: without it rclone runs the command a second
time to probe the remote shell. The tree is:

```
/                              every mount this head may serve
/<mount>/                      its complete snapshots, and `latest`
/<mount>/<snapshot>/<path>     the snapshot itself
```

Mount directories are 16 hex digits; snapshot directories are
`20060102T150405.000000000Z`, so they sort chronologically. Time travel is `cd`
into another snapshot. `latest` is the only mutable name in the tree, which is
why a VFS cache of a snapshot path can be kept for a very long time
(`--read-only --dir-cache-time 1000h --vfs-cache-mode full`).

`boxer fs sftp-stdio` requires `--mount <id>` (repeatable) or an explicit
`--all-mounts`. A mount you may not read is *absent*, not forbidden — a tree
that answered "permission denied" for one name and "no such file" for another
would let a client enumerate what it cannot read.

When the head cannot start, rclone reports it as a broken SFTP handshake —
`couldn't initialise SFTP: error receiving version packet from server` — and
what follows names the symptom, not the cause:

- **`unexpected EOF`** — the command exited without writing anything. A
  `boxer` binary built before this subcommand existed answers `No help topic
  for 'fs'` and exits; that is the usual cause, and it looks identical to a
  store that is genuinely unreachable.
- **`packet too long`** — the command wrote something that is not SFTP to
  stdout. Anything on stdout corrupts the stream, a usage message included.

rclone discards the head's stderr, which is where the diagnosis goes, so run
the `ssh=` command by hand to read it.

Everything else is rclone's: `rclone serve s3/webdav/nfs/docker` in front of
the pipe with rclone's own users, keys and TLS; `hasher` for checksums in
rclone's vocabulary; `union` for a writable scratch layer over a read-only
snapshot; filters; the web GUI.

## 7. Snapshot anything rclone can reach

The other direction — S3, Azure, Drive, WebDAV, a `crypt` remote, any of
rclone's backends — is an `fs.FS` like any other:

```go
src, err := ladingremote.Serve(ctx, "s3:bucket/prefix",
    ladingremote.WithFilters("--exclude", "*.tmp"))
defer src.Close()

res, err := ladingingest.Snapshot(ctx, src, mount, pol, stores)
```

Filters run at the *source*, so what they exclude never reaches this process.
`WithSubdir` roots the walk inside what rclone serves. **`Close` is not
optional**: it shuts the pipe and reaps the process.

## 8. Retire a mount

Retention is declarative, so the normal answer is to do nothing — rows expire
and whole parts drop. When you need it gone sooner, a per-mount purge is one
lightweight statement per table, because nothing is shared and nothing is
reference-counted:

```sql
DELETE FROM boxer.fsmeta WHERE "id:id:u64:47::0:" = <mount>;
-- and the same for boxer.fsdata and boxer.fssnap
```

Resolve the physical column name with `ladingschema.PhysicalPlainName("id")`
rather than typing it.

## 9. What does not work, and why

Collected here rather than discovered later. Each was measured, and most are
somebody else's limit rather than the store's.

- **`PREWHERE` does not survive a macro.** The expansion is a subquery, and
  ClickHouse allows `PREWHERE` only against a table or table function. Use
  `WHERE`; what is lost is the pre-filter. A caller who needs it reads the
  physical table.
- **rclone carries no file modes, in either direction.** `rclone copy
  --metadata` out of the store restores content and mtime but not permissions —
  its `sftp` backend documents no system metadata — and `rclone serve sftp`
  reports 0644 for every file on the way in. The store records the mode
  correctly at both ends; rclone is the part that does not ask.
- **mtimes over SFTP are whole seconds.** A snapshot of a remote records
  seconds where a local walk records nanoseconds.
- **Symlinks through rclone ingress need `--links`.** Without it,
  `rclone serve sftp` does not show a symlink at all. With it, the node arrives
  as a symlink with its target, and the adapter resolves it normally.
- **`text = true` is a guarantee, and a file with a very long line will not get
  it.** Where the flag is set, every block boundary falls immediately after a
  newline, so a line-oriented query is exact. A file carrying a line longer
  than one block is stored `text = false` and cut at fixed offsets instead —
  raise the block size if you need the guarantee for it.
- **A byte offset maps to a block ordinal only for non-text files.** A text
  file's blocks end where its newlines were, so the adapter materialises it
  whole on first read — bounded by `InlineMax`, since a file has blocks at all
  only if it was under it.
- **Nothing is deduplicated.** Two identical files are two copies; identical
  content is a *question* you can ask (`GROUP BY content_hash HAVING count() >
  1`) rather than a storage strategy. Sharing would make a block's lifetime the
  maximum over its references, which `TTL` cannot express.
- **This is not a hot serving path.** Every adapter call is a query —
  millisecond latency, batch and templating shaped. The SFTP head serialises
  against the store, because the generated stores are single-goroutine. One
  open `*File` is safe for the parallel `ReadAt` calls `io.ReaderAt` promises;
  an `FS`, and two handles on one, are not.
- **A snapshot is addressable only once it is complete.** `ladingingest.
  Snapshot` returns its `Result` even when the walk failed, so a caller can
  hold the instant of a walk that never committed — but neither the adapter nor
  the head will open it, and no macro will select it. A walk whose root could
  not be stat'd writes no snapshot at all rather than one nothing can list.
- **A profile only applies at creation.** `Provision` renders the granularity
  into the `CREATE TABLE`, and `IF NOT EXISTS` means an existing table keeps
  the profile it was made under. Changing one is a migration.

Related: [ADR-0198](../adr/0198-fs-snapshot-store.md) for the decisions and the
dated record of what each milestone corrected;
[the compact design](../adr-background-work/iofs-clickhouse-snapshot-store-compact.md)
for the schema and the SQL catalogue;
[lessons from rclone's architecture](../explanation/rclone-architecture-lessons.md)
for why the transport is shaped this way.
