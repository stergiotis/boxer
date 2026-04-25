# Third-Party Notices

This document enumerates third-party software incorporated into boxer
along with its license. It complements the root [LICENSE](LICENSE) and
[NOTICE](NOTICE) files. boxer itself is MIT-licensed; see LICENSE for
the full text.

The categories below correspond to distinct redistribution mechanics:

- **Inline ports** are third-party source files committed directly into
  this repository's tree. Their original license text is reproduced
  verbatim both in the file header and in this document.
- **Vendored binary artifacts** are pre-built binaries committed to the
  repository whose source lives elsewhere (or in a sibling directory).
- **Module-level Go dependencies** are pulled by `go.mod` at build time
  and are not redistributed in source form by this repository.

## 1. Inline ports (committed source)

### 1.1 Justin Talbot -- labeling (MIT)

- File: `public/math/numerical/finddivisions/finddivisions_talbot.go`
- Origin: <https://cran.r-project.org/web/packages/labeling/index.html>
- License: MIT

```
Copyright (c) 2020, Justin Talbot

Permission is hereby granted, free of charge, to any person obtaining
a copy of this software and associated documentation files (the
"Software"), to deal in the Software without restriction, including
without limitation the rights to use, copy, modify, merge, publish,
distribute, sublicense, and/or sell copies of the Software, and to
permit persons to whom the Software is furnished to do so, subject to
the following conditions:

The above copyright notice and this permission notice shall be
included in all copies or substantial portions of the Software.

THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND,
EXPRESS OR IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF
MERCHANTABILITY, FITNESS FOR A PARTICULAR PURPOSE AND
NONINFRINGEMENT. IN NO EVENT SHALL THE AUTHORS OR COPYRIGHT HOLDERS BE
LIABLE FOR ANY CLAIM, DAMAGES OR OTHER LIABILITY, WHETHER IN AN ACTION
OF CONTRACT, TORT OR OTHERWISE, ARISING FROM, OUT OF OR IN CONNECTION
WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE SOFTWARE.
```

### 1.2 Stanford University -- labeling exhaustive (BSD-2-Clause)

- File: `public/math/numerical/finddivisions/finddivisions_talbot_legibility_exhaustive.go`
- Origin: <https://github.com/jtalbot/Labeling/blob/master/Layout/Formatters/QuantitativeFormatter.cs>
- License: BSD 2-Clause

```
Copyright (c) 2012, Stanford University
All rights reserved.

Redistribution and use in source and binary forms, with or without
modification, are permitted provided that the following conditions are met:

1. Redistributions of source code must retain the above copyright notice, this
   list of conditions and the following disclaimer.
2. Redistributions in binary form must reproduce the above copyright notice,
   this list of conditions and the following disclaimer in the documentation
   and/or other materials provided with the distribution.

THIS SOFTWARE IS PROVIDED BY THE COPYRIGHT HOLDERS AND CONTRIBUTORS "AS IS" AND
ANY EXPRESS OR IMPLIED WARRANTIES, INCLUDING, BUT NOT LIMITED TO, THE IMPLIED
WARRANTIES OF MERCHANTABILITY AND FITNESS FOR A PARTICULAR PURPOSE ARE
DISCLAIMED. IN NO EVENT SHALL THE COPYRIGHT OWNER OR CONTRIBUTORS BE LIABLE FOR
ANY DIRECT, INDIRECT, INCIDENTAL, SPECIAL, EXEMPLARY, OR CONSEQUENTIAL DAMAGES
(INCLUDING, BUT NOT LIMITED TO, PROCUREMENT OF SUBSTITUTE GOODS OR SERVICES;
LOSS OF USE, DATA, OR PROFITS; OR BUSINESS INTERRUPTION) HOWEVER CAUSED AND
ON ANY THEORY OF LIABILITY, WHETHER IN CONTRACT, STRICT LIABILITY, OR TORT
(INCLUDING NEGLIGENCE OR OTHERWISE) ARISING IN ANY WAY OUT OF THE USE OF THIS
SOFTWARE, EVEN IF ADVISED OF THE POSSIBILITY OF SUCH DAMAGE.
```

### 1.3 Leon Sorokin -- uPlot time-axis design (MIT)

- File: `public/math/numerical/timeticks/timeticks_uplot.go`
- Origin: <https://github.com/leeoniya/uPlot> (`src/opts.js`)
- License: MIT
- Scope: the curated tick-step ladder, format-by-bucket convention, and
  dual-row context-label boundary detection follow uPlot's design. The Go
  code is an independent re-implementation written from notes, not a
  line-by-line port; the rendering is simplified to two label rows
  instead of uPlot's three-row rollover.

```
The MIT License (MIT)

Copyright (c) 2022 Leon Sorokin

Permission is hereby granted, free of charge, to any person obtaining
a copy of this software and associated documentation files (the
"Software"), to deal in the Software without restriction, including
without limitation the rights to use, copy, modify, merge, publish,
distribute, sublicense, and/or sell copies of the Software, and to
permit persons to whom the Software is furnished to do so, subject to
the following conditions:

The above copyright notice and this permission notice shall be
included in all copies or substantial portions of the Software.

THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND,
EXPRESS OR IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF
MERCHANTABILITY, FITNESS FOR A PARTICULAR PURPOSE AND
NONINFRINGEMENT. IN NO EVENT SHALL THE AUTHORS OR COPYRIGHT HOLDERS BE
LIABLE FOR ANY CLAIM, DAMAGES OR OTHER LIABILITY, WHETHER IN AN ACTION
OF CONTRACT, TORT OR OTHERWISE, ARISING FROM, OUT OF OR IN CONNECTION
WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE SOFTWARE.
```

## 2. Vendored binary artifacts

### 2.1 h3.wasm -- H3 hierarchical geospatial index (Apache-2.0)

- Artifact: `public/science/geo/h3/internal/h3o_wasm/h3.wasm`
- Built from: `rust/h3bridge/` (MIT OR Apache-2.0, this repository)
- Embeds: the [`h3o`](https://crates.io/crates/h3o) Rust crate by
  HydroniumLabs (Apache-2.0), which is a Rust port of Uber's H3 library.
- Build provenance: the artifact is byte-reproducible from the bridge
  sources. CI enforces parity via `scripts/ci/h3_wasm_parity.sh`, which
  rebuilds the wasm and byte-compares against the committed copy.

When a downstream binary embeds or redistributes this `.wasm` file, that
constitutes binary redistribution of `h3o`. Per Apache-2.0 section 4,
the Apache-2.0 license text plus any upstream NOTICE content from `h3o`
must accompany the redistribution. The `rust/h3bridge` crate itself is
dual-licensed `MIT OR Apache-2.0`, allowing downstream redistributors to
elect either license for the bridge layer; the underlying `h3o`
attribution remains required in either case.

## 3. Module-level Go dependencies

The authoritative list of Go module dependencies is `go.mod` (with
exact versions pinned in `go.sum`). Their license files are present in
the local module cache (`$(go env GOMODCACHE)`) once dependencies are
fetched.

A machine-readable license inventory can be regenerated at any time:

```
go tool github.com/google/go-licenses csv ./public/... > third_party_licenses.csv
```

This is not committed to the repository because it is fully derived
from `go.mod` + `go.sum` and would otherwise drift on every dependency
update.

### 3.1 Compliance gate

CI rejects any transitive dependency whose license falls into the
[`forbidden`](https://github.com/google/licenseclassifier) or
`restricted` categories used by `go-licenses` -- principally AGPL-*,
GPL-*, LGPL-*, SSPL, and similar copyleft or commercially-restrictive
terms. boxer's MIT license is incompatible with copyleft inbound
dependencies, and the gate enforces this prospectively. See
`.github/workflows/licenses.yaml` and `scripts/ci/golicenses.sh`.

The gate does **not** fail on `unknown` classifications. A handful of
upstream modules ship a single `LICENSE` at the module root and
`go-licenses` cannot always resolve it for subpackages (e.g.
`github.com/golang/freetype/{raster,truetype}`, transitively via
`github.com/fogleman/gg`). The CI script surfaces these cases as a
trailing "unresolved licenses" block for periodic manual review --
typically the upstream license is well-known and permissive, but the
classifier's regex did not locate it. If a new entry appears in that
block on a dependency bump, verify the upstream license manually before
merging.

### 3.2 Apache-2.0 dependencies and downstream NOTICE propagation

A subset of `go.mod` dependencies are Apache-2.0-licensed and ship
their own `NOTICE` files (notably `github.com/apache/arrow-go/v18`,
`github.com/apache/thrift`, and `github.com/tetratelabs/wazero`; the
authoritative list is the `go-licenses` CSV output above).

When boxer is consumed in **source form** (`go get`, module proxy),
downstream users receive these dependencies independently with their
own LICENSE/NOTICE files intact -- no propagation by boxer is required.

When a downstream **binary** statically links these dependencies, the
binary's distributor must propagate the upstream NOTICE contents per
Apache-2.0 section 4(d). boxer's own [NOTICE](NOTICE) is not a
substitute for the upstream NOTICEs; both must travel with the binary.

## Maintaining this document

- When adding an inline port of third-party code, append a subsection
  under section 1 with the original license text reproduced verbatim,
  matching the file header.
- When adding a vendored binary artifact, append a subsection under
  section 2 with the build provenance and license chain.
- Module-level dependency updates do not require edits here: `go.mod`
  is the source of truth and the `go-licenses` CI gate is the guard.
