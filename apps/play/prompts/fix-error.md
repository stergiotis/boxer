---
title: Fix this error
summary: Rewrite the SQL in the editor so the last run's error goes away; the result is a preview, never a run.
icon: "🛠"
scope: buffer+error
temperature: 0.1
---
Keep the intent of the original query. Change as little as needed to make
it valid for ClickHouse and to remove the error the user reports. Do not
add tables or columns the query did not use unless the error says a name
is wrong and the schema shows the right one. If the error cannot be fixed
from the query alone, return the query unchanged and say so in one line
before the code block.
