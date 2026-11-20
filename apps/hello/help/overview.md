---
type: explanation
audience: end-user
status: draft
title: Hello
---

> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.

# Hello

Hello is the smallest windowed app: a text field for a name, a **Greet**
button, the greeting itself, and a count of the greetings (`greeted N×`).

The count is kept in the app's storage under the key `greetings`, so a
new Hello window starts from the count the last one reached. Where the
host has no durable store the count lasts as long as the process. The
name is not kept.

It is the app the first-app tutorial builds, and does nothing else.
