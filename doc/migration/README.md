---
type: how-to
audience: operator upgrading a deployment that holds boxer tables; contributor changing a stored shape
status: draft
# reviewed-by: "@<handle>"     # fill in and uncomment when flipping to stable
# reviewed-date: YYYY-MM-DD    # fill in and uncomment when flipping to stable
---

> **Status: draft — pre-human-review.** The mechanism below has been run
> against clickhouse-local only, not against a deployment.

# Migrating stored boxer tables

`CREATE TABLE IF NOT EXISTS` leaves an existing table as it is, so a build
whose generated columns changed meets the old shape at its next start. What
that start does, and which changes are mechanical, is
[ADR-0387](../adr/0387-what-a-binary-does-when-a-stored-table-is-older-or-newer-than-its-schema.md).

## First step: `boxer schema plan`

```sh
boxer schema plan                       # every stored table this build knows
boxer schema plan --table boxer.watchbill
```

It reads the server the `CLICKHOUSE_*` entries name, prints each table's
verdict, and prints the statements that bring it in line as SQL, changing
nothing. A host shows the same verdicts as `keelson('schema_tables')`.

| Verdict | Meaning | What to do |
|---|---|---|
| `ok` | every generated column present with its type; extra columns are read past | nothing; the plan may still offer the deduplication window and the stamp |
| `migratable` | generated columns missing, or present under an older aspect spelling | `boxer schema apply --yes`, or run the printed SQL by hand |
| `incompatible` | a column the store reads has another type, or is computed | a recipe in this directory |
| `absent` | no table | nothing; the owning store creates it when it starts |
| `unchecked` | the table's shape could not be read | the reason is printed |

`apply` refuses a table whose plan is refused and stops at the first statement
the server rejects. No host applies a plan at boot.

## When a recipe is needed

The plan refuses what it cannot derive: a type change, a rename of a column a
key or skip index names (ClickHouse renames no such column), a change of the
engine, the sorting key or the partition key, and any change of meaning that
keeps a column's name and type. Those need a recipe here.

## What a recipe contains

One file per stored-shape change, named `YYYY-MM-<table>-<change>.md`, with:

- the change and the ADR that made it;
- when to use the recipe, and the symptom of skipping it (the verdict line
  and, where it differs, the error the first statement hits);
- the cheaper alternative where the rows are expendable (drop the table; the
  store recreates it);
- the statements, derived from the difference between the DDL goldens before
  and after, with the generated `.out.sql` named as the authority;
- how to check the result — a clean `boxer schema plan` is the usual check;
- what the existing rows read as afterwards.

## The commit that changes a stored shape

A change to a store's generated columns, its engine clause or its sorting key
is a breaking change of a stored format: its commit subject carries `!` and
its body says what an operator runs, per
[CODINGSTANDARDS § Version Control](../../CODINGSTANDARDS.md#version-control).
When the change is not mechanical, the recipe lands in the same commit.
