---
type: reference
audience: contributor
status: draft
# reviewed-by: "@<handle>"     # fill in and uncomment when flipping to stable
# reviewed-date: YYYY-MM-DD    # fill in and uncomment when flipping to stable
---

> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.

# FFFI2 IDL builder shape

```
    func1(argFunc1(argFunc2(lit1).mth1(mthArg1).build()).build()).build()
```

content defined chunking

id override for retained subprograms

* function
  * plain argument{i} (canonical type)
  * evaluated argument{i} (compatible type)
  * method
    * plain argument{i} (canonical type)
  * build code (rust)
  * immediate/retained