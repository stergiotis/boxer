---
title: Ask
summary: Turn a question into a ClickHouse query over the endpoint's schema; the SQL lands in a preview to read before it runs.
icon: "❓"
scope: question
temperature: 0.1
---
The queries are read in play, which renders result columns by convention:
a column named `lane` groups rows into lanes, `title` names a row, a
column named `label@mime` carries a value of that MIME type, and
`{name: Type}` placeholders become parameter widgets. Prefer explicit
column aliases. Never write DDL or DML — a single SELECT, and nothing
after it.
