---
type: reference
audience: contributor
status: stable
reviewed-by: "p@stergiotis"
reviewed-date: 2026-04-26
---

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
  multi-row context-label boundary detection follow uPlot's design. The
  Go code is an independent re-implementation written from notes, not a
  line-by-line port. Rollover rows are range-based here (each row groups
  ticks into contiguous runs sharing a boundary value) — a small
  generalisation over uPlot's point-anchored rendering of the same data.

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

### 1.4 Redpanda Data -- Connect kafka I/O port (Apache-2.0)

- Files: `public/streaming/persisted/kafka/` (entire package directory).
- Origin: <https://github.com/redpanda-data/connect/tree/50aa034a668cc7d03d6acdcf63791fc36906a21c/internal/impl/kafka>
- Pinned commit: `50aa034a668cc7d03d6acdcf63791fc36906a21c` (2026-04-24).
- License: Apache License, Version 2.0.
- Decision record: [ADR-0005](doc/adr/0005-streaming-persisted-kafka-from-connect.md).
- Per-package NOTICE (provenance + modifications log per Apache-2.0 §4.b): [`public/streaming/persisted/kafka/NOTICE`](public/streaming/persisted/kafka/NOTICE).
- Scope: the franz-go (`kgo`) consumer and producer were ported and refactored to boxer style; the Benthos `service` framework dependency was dropped. The sarama variant, schema-registry input/output, Redpanda-specific wrappers, and the RCL-licensed `enterprise/` subdirectory were not ported. Transitive Go module dependencies introduced by this port (`github.com/twmb/franz-go` and submodules — BSD-3-Clause; `github.com/Jeffail/checkpoint` and `github.com/Jeffail/shutdown` — MIT; `github.com/testcontainers/testcontainers-go/modules/redpanda` — MIT, test-only) are tracked under section 3 via `go.mod`.

```
                                 Apache License
                           Version 2.0, January 2004
                        http://www.apache.org/licenses/

   TERMS AND CONDITIONS FOR USE, REPRODUCTION, AND DISTRIBUTION

   1. Definitions.

      "License" shall mean the terms and conditions for use, reproduction,
      and distribution as defined by Sections 1 through 9 of this document.

      "Licensor" shall mean the copyright owner or entity authorized by
      the copyright owner that is granting the License.

      "Legal Entity" shall mean the union of the acting entity and all
      other entities that control, are controlled by, or are under common
      control with that entity. For the purposes of this definition,
      "control" means (i) the power, direct or indirect, to cause the
      direction or management of such entity, whether by contract or
      otherwise, or (ii) ownership of fifty percent (50%) or more of the
      outstanding shares, or (iii) beneficial ownership of such entity.

      "You" (or "Your") shall mean an individual or Legal Entity
      exercising permissions granted by this License.

      "Source" form shall mean the preferred form for making modifications,
      including but not limited to software source code, documentation
      source, and configuration files.

      "Object" form shall mean any form resulting from mechanical
      transformation or translation of a Source form, including but
      not limited to compiled object code, generated documentation,
      and conversions to other media types.

      "Work" shall mean the work of authorship, whether in Source or
      Object form, made available under the License, as indicated by a
      copyright notice that is included in or attached to the work
      (an example is provided in the Appendix below).

      "Derivative Works" shall mean any work, whether in Source or Object
      form, that is based on (or derived from) the Work and for which the
      editorial revisions, annotations, elaborations, or other modifications
      represent, as a whole, an original work of authorship. For the purposes
      of this License, Derivative Works shall not include works that remain
      separable from, or merely link (or bind by name) to the interfaces of,
      the Work and Derivative Works thereof.

      "Contribution" shall mean any work of authorship, including
      the original version of the Work and any modifications or additions
      to that Work or Derivative Works thereof, that is intentionally
      submitted to Licensor for inclusion in the Work by the copyright owner
      or by an individual or Legal Entity authorized to submit on behalf of
      the copyright owner. For the purposes of this definition, "submitted"
      means any form of electronic, verbal, or written communication sent
      to the Licensor or its representatives, including but not limited to
      communication on electronic mailing lists, source code control systems,
      and issue tracking systems that are managed by, or on behalf of, the
      Licensor for the purpose of discussing and improving the Work, but
      excluding communication that is conspicuously marked or otherwise
      designated in writing by the copyright owner as "Not a Contribution."

      "Contributor" shall mean Licensor and any individual or Legal Entity
      on behalf of whom a Contribution has been received by Licensor and
      subsequently incorporated within the Work.

   2. Grant of Copyright License. Subject to the terms and conditions of
      this License, each Contributor hereby grants to You a perpetual,
      worldwide, non-exclusive, no-charge, royalty-free, irrevocable
      copyright license to reproduce, prepare Derivative Works of,
      publicly display, publicly perform, sublicense, and distribute the
      Work and such Derivative Works in Source or Object form.

   3. Grant of Patent License. Subject to the terms and conditions of
      this License, each Contributor hereby grants to You a perpetual,
      worldwide, non-exclusive, no-charge, royalty-free, irrevocable
      (except as stated in this section) patent license to make, have made,
      use, offer to sell, sell, import, and otherwise transfer the Work,
      where such license applies only to those patent claims licensable
      by such Contributor that are necessarily infringed by their
      Contribution(s) alone or by combination of their Contribution(s)
      with the Work to which such Contribution(s) was submitted. If You
      institute patent litigation against any entity (including a
      cross-claim or counterclaim in a lawsuit) alleging that the Work
      or a Contribution incorporated within the Work constitutes direct
      or contributory patent infringement, then any patent licenses
      granted to You under this License for that Work shall terminate
      as of the date such litigation is filed.

   4. Redistribution. You may reproduce and distribute copies of the
      Work or Derivative Works thereof in any medium, with or without
      modifications, and in Source or Object form, provided that You
      meet the following conditions:

      (a) You must give any other recipients of the Work or
          Derivative Works a copy of this License; and

      (b) You must cause any modified files to carry prominent notices
          stating that You changed the files; and

      (c) You must retain, in the Source form of any Derivative Works
          that You distribute, all copyright, patent, trademark, and
          attribution notices from the Source form of the Work,
          excluding those notices that do not pertain to any part of
          the Derivative Works; and

      (d) If the Work includes a "NOTICE" text file as part of its
          distribution, then any Derivative Works that You distribute must
          include a readable copy of the attribution notices contained
          within such NOTICE file, excluding those notices that do not
          pertain to any part of the Derivative Works, in at least one
          of the following places: within a NOTICE text file distributed
          as part of the Derivative Works; within the Source form or
          documentation, if provided along with the Derivative Works; or,
          within a display generated by the Derivative Works, if and
          wherever such third-party notices normally appear. The contents
          of the NOTICE file are for informational purposes only and
          do not modify the License. You may add Your own attribution
          notices within Derivative Works that You distribute, alongside
          or as an addendum to the NOTICE text from the Work, provided
          that such additional attribution notices cannot be construed
          as modifying the License.

      You may add Your own copyright statement to Your modifications and
      may provide additional or different license terms and conditions
      for use, reproduction, or distribution of Your modifications, or
      for any such Derivative Works as a whole, provided Your use,
      reproduction, and distribution of the Work otherwise complies with
      the conditions stated in this License.

   5. Submission of Contributions. Unless You explicitly state otherwise,
      any Contribution intentionally submitted for inclusion in the Work
      by You to the Licensor shall be under the terms and conditions of
      this License, without any additional terms or conditions.
      Notwithstanding the above, nothing herein shall supersede or modify
      the terms of any separate license agreement you may have executed
      with Licensor regarding such Contributions.

   6. Trademarks. This License does not grant permission to use the trade
      names, trademarks, service marks, or product names of the Licensor,
      except as required for reasonable and customary use in describing the
      origin of the Work and reproducing the content of the NOTICE file.

   7. Disclaimer of Warranty. Unless required by applicable law or
      agreed to in writing, Licensor provides the Work (and each
      Contributor provides its Contributions) on an "AS IS" BASIS,
      WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or
      implied, including, without limitation, any warranties or conditions
      of TITLE, NON-INFRINGEMENT, MERCHANTABILITY, or FITNESS FOR A
      PARTICULAR PURPOSE. You are solely responsible for determining the
      appropriateness of using or redistributing the Work and assume any
      risks associated with Your exercise of permissions under this License.

   8. Limitation of Liability. In no event and under no legal theory,
      whether in tort (including negligence), contract, or otherwise,
      unless required by applicable law (such as deliberate and grossly
      negligent acts) or agreed to in writing, shall any Contributor be
      liable to You for damages, including any direct, indirect, special,
      incidental, or consequential damages of any character arising as a
      result of this License or out of the use or inability to use the
      Work (including but not limited to damages for loss of goodwill,
      work stoppage, computer failure or malfunction, or any and all
      other commercial damages or losses), even if such Contributor
      has been advised of the possibility of such damages.

   9. Accepting Warranty or Additional Liability. While redistributing
      the Work or Derivative Works thereof, You may choose to offer,
      and charge a fee for, acceptance of support, warranty, indemnity,
      or other liability obligations and/or rights consistent with this
      License. However, in accepting such obligations, You may act only
      on Your own behalf and on Your sole responsibility, not on behalf
      of any other Contributor, and only if You agree to indemnify,
      defend, and hold each Contributor harmless for any liability
      incurred by, or claims asserted against, such Contributor by reason
      of your accepting any such warranty or additional liability.

   END OF TERMS AND CONDITIONS

   APPENDIX: How to apply the Apache License to your work.

      To apply the Apache License to your work, attach the following
      boilerplate notice, with the fields enclosed by brackets "[]"
      replaced with your own identifying information. (Don't include
      the brackets!)  The text should be enclosed in the appropriate
      comment syntax for the file format. We also recommend that a
      file or class name and description of purpose be included on the
      same "printed page" as the copyright notice for easier
      identification within third-party archives.

   Copyright [yyyy] [name of copyright owner]

   Licensed under the Apache License, Version 2.0 (the "License");
   you may not use this file except in compliance with the License.
   You may obtain a copy of the License at

       http://www.apache.org/licenses/LICENSE-2.0

   Unless required by applicable law or agreed to in writing, software
   distributed under the License is distributed on an "AS IS" BASIS,
   WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
   See the License for the specific language governing permissions and
   limitations under the License.
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
go tool github.com/CycloneDX/cyclonedx-gomod/cmd/cyclonedx-gomod mod \
    -licenses=true -test=true -json -output sbom.json
go run ./internal/cmd/licensegate -sbom sbom.json -csv third_party_licenses.csv
```

This is not committed to the repository because it is fully derived
from `go.mod` + `go.sum` and would otherwise drift on every dependency
update. The CSV columns are `module,version,spdx_id,category`.

### 3.1 Compliance gate

CI rejects any transitive dependency whose license falls into the
`forbidden` or `restricted` categories enumerated in
[`internal/cmd/licensegate/policy.go`](internal/cmd/licensegate/policy.go) --
principally AGPL-\*, GPL-\*, LGPL-\*, SSPL, BUSL, OSL, and CC-BY-NC-\*,
plus other copyleft or commercially-restrictive terms. boxer's MIT
license is incompatible with copyleft inbound dependencies, and the
gate enforces this prospectively. See
`.github/workflows/licenses.yaml` and `scripts/ci/license_gate.sh`;
the design rationale is in
[ADR-0004](doc/adr/0004-license-gate-cyclonedx.md).

The gate does **not** fail on `unknown` classifications. A handful of
upstream modules ship their `LICENSE` in a form `cyclonedx-gomod`'s
detector cannot classify (e.g. `LICENSE.md` instead of `LICENSE`, or
an Apache header without a canonical license file). The gate surfaces
these cases as a trailing "unresolved licenses" block for periodic
manual review -- typically the upstream license is well-known and
permissive, but the detector did not classify it. If a new entry
appears in that block on a dependency bump, verify the upstream
license manually before merging.

Where an upstream is dual-licensed and the detector reports only the
copyleft branch, the elected SPDX ID is recorded in the
`moduleLicenseElection` map in `policy.go`. The current entry is
`github.com/golang/freetype → FTL` (per upstream LICENSE: the project
is offered under either the FreeType License (BSD-like) or
GPL-2.0-or-later; boxer elects FTL).

### 3.2 Apache-2.0 dependencies and downstream NOTICE propagation

A subset of `go.mod` dependencies are Apache-2.0-licensed and ship
their own `NOTICE` files (notably `github.com/apache/arrow-go/v18`,
`github.com/apache/thrift`, and `github.com/tetratelabs/wazero`; the
authoritative list is the `licensegate` CSV inventory regenerated as
shown above).

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
  is the source of truth and the `licensegate` CI gate is the guard.
