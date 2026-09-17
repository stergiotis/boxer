---
type: how-to
audience: developer publishing tabular data from an app, or reading it from an applet or a play window
status: draft
# reviewed-by: "@<handle>"   # fill in and uncomment when flipping to stable
# reviewed-date: YYYY-MM-DD  # fill in and uncomment when flipping to stable
---

> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.

# How to publish an ad-hoc dataset and query it

The task-oriented walk: hand a table your app computed to SQL without
storing it anywhere, read it from a committed applet or a play window you
open, and let go of it when the window closes. Why it is shaped this way is
[ADR-0240](../adr/0240-adhoc-datasets-v2-sealed-store-owned-capability.md);
the placement rule a query naming one is subject to is
[ADR-0145](../adr/0145-sealed-app-data.md).

One fact carries the page. **A dataset lives as long as the window that
published it, unless the publisher says otherwise.** It is a sealed file
with no name on any filesystem, under a key that exists only inside the
process, registered under a handle nobody can guess and published under an
alias anyone can read. When the window closes, the runtime retracts it;
when the process ends, the kernel frees it; after a crash there is nothing
to find.

## 1. Publish from an app

Declare the capability on the manifest and publish through a
`Publisher`, one per alias. The publisher reuses its handle across
republishes, so re-running a computation replaces the dataset rather than
minting another against the quotas:

```go
// app_register.go
Caps: []app.SubjectFilter{
    {Pattern: adhocdata.SubjectPublish, Direction: app.CapDirectionPub, Reason: "…"},
},

// wherever the app holds per-window state
pub := adhocdata.NewPublisher("items", false)

// off the render thread — a publish is a bus round trip
stream, err := adhocdata.EncodeRecord(rec) // rec is an arrow.RecordBatch
res, err := pub.Publish(bus, stream)       // res.Handle, res.Revision, res.Rows
```

The alias must be a bare identifier (`[A-Za-z_][A-Za-z0-9_]*`, at most 64
bytes). The column types must be in the publish gate's set — the scalar
Arrow types, `Timestamp` in µs or ns, `FixedSizeBinary`, and `List`,
`Struct`, `Map` over them — or the publish is refused naming the column;
nothing is discovered at query time. Quotas: 256 MiB per dataset, 1 GiB
and 64 datasets live at once.

Pass `true` as the second argument of `NewPublisher` when the dataset is
the *app's* rather than the window's: it then survives the window that
published it, until the process ends, and any window of the same app may
republish or retract it. imzrt's profile captures are the case for this;
most datasets are not.

Nothing needs retracting. If you want the dataset gone before the window
closes, `pub.Retract(bus)`; a retract from any identity but the publisher's
is refused.

## 2. Read it from a committed applet

An applet document declares the aliases it names in frontmatter, and its
SQL names them as tables:

```markdown
---
datasets: [items]
datasets_hint: "Regenerate one from the demo's Regenerate button."
---
```sql
SELECT * FROM keelson('items') ORDER BY x
```
```

Both `keelson('items')` and a bare `items` are rewritten to the live
handle before the statement leaves the window. The applet host follows the
alias for the life of the window: it binds the newest live dataset at open,
re-runs under Live when that dataset is republished, shows a notice over
the empty panes — with your `datasets_hint` — while nothing is live under
the alias, and binds the next dataset when one appears. "Capture first,
then open" is not an ordering you have to teach anyone.

## 3. Open a play window on it

An app that publishes and wants a playground on the result opens play with
a launch config that names the alias in its SQL and declares it:

```go
cfg := launchcfg.PlayLaunch{
    Sql:      "SELECT * FROM keelson('items') ORDER BY x",
    AutoRun:  true,
    Endpoint: launchcfg.EndpointIntrospection, // where ad-hoc datasets resolve
    Datasets: []string{"items"},
}
cfgBytes, err := buscodec.Encode(cfg)
_, err = windowhost.RequestOpen(bus, launchcfg.AppId, launchcfg.Kind, cfgBytes)
```

The window follows the alias exactly as an applet does. Never splice a
handle into the SQL: a handle is unguessable on purpose, and a buffer that
carries one stops working the moment the dataset is republished under a
different one.

## 4. See what is live

```sql
SELECT handle, alias, publisher, publisher_instance, keep_after_close,
       rows, bytes, revision, open_readers
FROM keelson('adhoc')
```

The catalog is an ordinary introspection table, so it joins with
`keelson('windows')` and `keelson('subscriptions')` on the instance key.
A dataset that has been retracted leaves the catalog at once and stays
queryable by handle for one grace period, so a query that had already
resolved it completes.

## 5. Where the bytes are, and are not

Under the directory `BOXER_ADHOC_DIR` names (default
`<user cache dir>/boxer/adhoc`) — but nothing is ever listed there: the
files are unnamed inodes, freed by the kernel on last close. The directory
only decides which filesystem holds them and must support `O_TMPFILE`
(ext4 and tmpfs do; some FUSE and network filesystems do not, and the
service refuses to start on one). Plaintext exists in this process's memory
and, during a read, on the loopback socket between the in-process table
source and the `clickhouse-local` worker. It is never on disk. The threat
model is the disk — images, backups, removed media — not another process
on the same host.

A v1 store left named `.bxad` files under the same directory; they are
ciphertext whose key is gone, and can be deleted by hand.
