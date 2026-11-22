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
_, err = windowopen.RequestOpen(bus, launchcfg.AppId, launchcfg.Kind, cfgBytes)
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

## 6. Join a dataset with server tables

A statement that names `keelson('alias')` and a server table runs on the
server: play's dispatch places it in the session data service, which
attaches the dataset to the statement as external data under the same
grant rules and the confinement wall — see
[How to keep named data sets for a task](./session-data-sets.md) §5. A
window not wired to the service, or a `keelson('…')` table of the host's
own plane rather than an ad-hoc dataset, is still refused there: no
endpoint serves both.

## 7. Hand a dataset to a program on this machine

An exposure makes one dataset readable outside the host's process, by a
program on the same machine, until it lapses
([ADR-0407](../adr/0407-an-exposed-dataset-a-program-on-this-machine-reads-an-ad-hoc-dataset-by-alias-and-token.md),
proposed). Nothing leaves the machine: the host serves it on its loopback
listener, to a request that presents the exposure's token.

**Expose it.** In play's *Data sets* tab, Expose beside an ad-hoc dataset;
the tab then shows a `curl` line and a `url()` line with Copy. An agent
calls play's `expose_dataset` with the alias, or the name the window binds
it under, and needs `expose:<alias>` — or `expose:<prefix>` for every alias
starting with the prefix — in its grant; it exposes only a dataset its own
call published. An app calls `adhocdata.ExposeRequest` and declares
`adhocdata.ExposeCap`. The answer is the only place the token is given:

```text
url        http://127.0.0.1:41537/exposed/kernel_top__result
csv url    http://127.0.0.1:41537/exposed/kernel_top__result/csv
token      bxe_3f0c…(64 hex digits)
clickhouse url('http://127.0.0.1:41537/exposed/kernel_top__result', 'ArrowStream', '`id` Int64, `score` Float64', headers('Authorization' = 'Bearer bxe_3f0c…'))
curl       curl -sf -H 'Authorization: Bearer bxe_3f0c…' 'http://127.0.0.1:41537/exposed/kernel_top__result'
expires    2026-11-23T15:04:05Z
```

**Read it from a ClickHouse server on this machine.** Paste the `url()`
expression where a table goes:

```sql
SELECT m.sender, r.score
FROM url('http://127.0.0.1:41537/exposed/kernel_top__result', 'ArrowStream',
         '`id` Int64, `score` Float64',
         headers('Authorization' = 'Bearer bxe_3f0c…')) AS r
INNER JOIN mail.messages AS m ON m.id = r.id
ORDER BY r.score DESC
LIMIT 20;
```

The server's user needs `READ ON URL` (`GRANT URL ON *.* TO reader`), and
the server must admit the host in its `remote_url_allow_hosts`:

```xml
<clickhouse>
  <remote_url_allow_hosts>
    <host>127.0.0.1</host>
    <host>localhost</host>
  </remote_url_allow_hosts>
</clickhouse>
```

A user without the grant is answered `ACCESS_DENIED`. ClickHouse shows the
header's value as `[HIDDEN]` in its query log; the token is still in the
statement text the server receives. If the user agent work runs as is the
one that reads, granting it `READ ON URL` lets it read any URL the setting
admits; play refuses `url()` in a task's statement, and a separate reading
user avoids the question.

**Fetch it from a script.**

```bash
curl -sf -H 'Authorization: Bearer bxe_3f0c…' \
  'http://127.0.0.1:41537/exposed/kernel_top__result' > result.arrows
curl -sf -H 'Authorization: Bearer bxe_3f0c…' \
  'http://127.0.0.1:41537/exposed/kernel_top__result/csv' > result.csv
```

```python
import urllib.request, pyarrow.ipc as ipc
req = urllib.request.Request(url, headers={"Authorization": "Bearer " + token})
table = ipc.open_stream(urllib.request.urlopen(req).read()).read_all()
```

**What a reader is told.** Every request that opens nothing — no token, a
wrong one, an exposure lapsed or revoked, an alias not exposed — is a `404`
with the same body. A request with the right token while 16 reads of the
exposure are open is a `503` to retry. A republish under the alias with
the same columns is served at its new revision, named in the `ETag`.

**When it ends.** At the end of its lifetime (an hour unless asked, at most
a day); when the dataset is retracted, its columns change or the window
that published it closes; when the window that exposed it closes; on
Revoke or `revoke_exposure`; when the host stops. On a host the
organisation manages (`BOXER_CELL_MANAGED`), an exposure above the export
clearance is refused. Every expose, read, revoke and lapse is on the trail
in the domain `adhoc-expose`, without the token.
