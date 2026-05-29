---
type: reference
audience: agent reading this skill
status: draft
# reviewed-by: "@<handle>"     # fill in and uncomment when flipping to stable
# reviewed-date: YYYY-MM-DD    # fill in and uncomment when flipping to stable
---

> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.
# SKILL: FFFI2 (Frame-based Foreign Function Interface 2)

## 1. Architectural Overview & Topology

FFFI2 is a high-performance, out-of-process bridge designed to connect **Go (Client/Logic)** and **Rust (Server/Interpreter)** without the severe context-switching overhead of traditional CGO.

### Core Tenets
1. **Out-of-Process IPC:** FFFI2 does not use shared memory (yet) or CGO. Rust runs as a completely separate OS process. Communication happens via 100% reliable OS Pipes.
2. **Bytecode VM Paradigm:** Go compiles API calls into length-prefixed binary opcodes. Rust runs an optimized `match` loop to interpret these opcodes.
3. **Go Owns the State (IDs):** The Rust process is logically stateless. Go generates all unique IDs (`ids.PrepareSeq(...)`). Rust attaches these Go-provided IDs to any return data.
4. **Pipelining & 1-Frame Latency:** Almost all commands are fire-and-forget. Batched state is retrieved at the end of a frame, meaning application logic typically operates with a 1-frame delay.
5. **Buffer Splicing:** Complex nested arguments are marshalled into byte buffer pools and "spliced" into a single, contiguous length-prefixed IPC message to avoid fragmentation.
6. **Error Handling:** If Rust encounters an error, it skips the remainder of the malformed length-prefixed message and attempts to recover at the next boundary. Severe errors result in a Rust process panic, which must be handled at the Go application level (e.g., rebooting the Rust child process).

---

## 2. The Three-Phase Pipeline

Using FFFI2 requires defining the API boundary (IDL), generating the stubs, and consuming the runtime API. We will use an **alternative use-case: A 3D Scene Graph Engine** (Go controls logic, Rust executes `wgpu` rendering) to illustrate.

### Phase 1: IDL (Interface Definition Language)
You define the API boundary in Go using AST nodes. There are three primary node types:
*   `BuilderFactoryNode`: Represents an object constructed via the Builder Pattern.
*   `ProceduralNode`: Represents a standalone function execution.
*   `FetcherNode`: Defines batched state transfer from Rust to Go.

**Illustrative Code: Defining a 3D Rendering API**
```go
package main

import (
	"fffi2/ir"
	"fffi2/ir/idl"
	"fffi2/canonicaltypes/ctabb"
)

func define3DEngineAPI() []ir.NodeI {
	var nodes[]ir.NodeI

	// 1. A BuilderFactoryNode for a Material (Builder Pattern)
	nodes = append(nodes, idl.NewBuilderFactoryNode("material").
		AddArguments(idl.NewArgumentsBuilder().PlainArg("name", ctabb.S).Build()).
		AddMethods(idl.NewMethodBuilder().
			BeginMethod("albedo").Arg("color", ctabb.U32).
				CodeClientRust(ir.StringVerbatimCode{VerbatimCode: "{{Instance}} = {{Instance}}.albedo(color);\n"}).
			EndMethod().
			BeginMethod("metallic").Arg("val", ctabb.F32).
				CodeClientRust(ir.StringVerbatimCode{VerbatimCode: "{{Instance}} = {{Instance}}.metallic(val);\n"}).
			EndMethod().
			Build()...).
		WithConstructionCodeClientRust(ir.StringVerbatimCode{VerbatimCode: "engine::MaterialBuilder::new(name);\n"}).
		WithSettingImmediate(true). // Fire-and-forget execution
		Build())

	// 2. A BuilderFactoryNode with a BLOCK ITERATOR (Maps Rust Closure -> Go Iterator)
	nodes = append(nodes, idl.NewBuilderFactoryNode("renderPass").
		AddArguments(idl.NewArgumentsBuilder().PlainArg("clearColor", ctabb.U32).Build()).
		WithSettingBlockIterator(true). // Critical: signifies this wraps a closure
		WithConstructionCodeClientRust(ir.StringVerbatimCode{VerbatimCode: "engine::RenderPass::new(clear_color);\n"}).
		Build())

	// 3. A FetcherNode to grab batched collision events from the previous frame
	nodes = append(nodes, idl.NewFetcherNode("collisions").
		AddReturnValue("entityIdA", ctabb.U64).
		AddReturnValue("entityIdB", ctabb.U64).
		WithApplyCodeClientRust(ir.StringVerbatimCode{VerbatimCode: `
			let len = self.physics_collisions_a.len();
			self.io.write_plain_u64h(len, self.physics_collisions_a.drain(..)).expect("io err");
			self.io.write_plain_u64h(len, self.physics_collisions_b.drain(..)).expect("io err");
		`}).
		Build())

	return nodes
}
```

### Phase 2: Compile-Time Code Generation
Pass the defined AST nodes into the FFFI2 compile-time generators.

```go
import (
    "fffi2/compiletime"
    "fffi2/compiletime/goserver"
    "fffi2/compiletime/rustclient"
)

// Generate Go Server (Client/Logic) bindings
goWriters := goserver.WriterHolder{ /* ... initialize byte buffers ... */ }
goTracker := compiletime.NewStateAndErrTracker[goserver.GeneratorStateE](1, "Go Err")
goserver.GenerateCode(goWriters, define3DEngineAPI(), goTracker)

// Generate Rust Client (Server/Interpreter) bindings
rustWriters := rustclient.WriterHolder{ /* ... initialize byte buffers ... */ }
rustTracker := compiletime.NewStateAndErrTracker[rustclient.GeneratorStateE](1, "Rust Err")
rustclient.GenerateCode(rustWriters, define3DEngineAPI(), rustTracker)
```

### Phase 3: Runtime Usage (Go Logic Side)
The generated Go code provides a highly ergonomic API that masks the underlying IPC byte streams.

*   `.Send()` triggers immediate execution of the opcode.
*   `.Keep()` marshals the arguments into a byte buffer pool to be spliced into a parent command.
*   `.KeepIter()` leverages Go 1.23 `iter.Seq` to simulate closures.

**Illustrative Code: Using the generated API**
```go
// Render Loop
for {
    // 1. Fetch batched state from the PREVIOUS frame (1-frame latency pipeline)
    collisions := engine.FetchCollisions()
    for collisions.Next() {
        handleCollision(collisions.EntityIdA(), collisions.EntityIdB())
    }

    // 2. Define the current frame using Iterators (Closure mapping)
    // The Go 'for range' triggers a "Start Scope" opcode. 
    // In Rust, this opens `pass.execute(|ctx| { ... self.interpret_outer(...) })`
    for range engine.RenderPass(0x000000FF /* black clear color */).KeepIter() {
        
        // Inside the pass, send immediate procedural commands
        engine.Material("ship_mat").Albedo(0xFFFFFF).Metallic(0.8).Send()
        engine.DrawMesh(shipMeshId).Send()
        
    } // Exiting the loop triggers an "End Scope" opcode, closing the Rust closure.

    engine.EndFrame()
}
```

---

## 3. Key Mental Models for LLMs

When generating code or architecture using FFFI2, an LLM must adhere to the following mapping rules:

### A. The Builder VM Translation
When FFFI2 translates `engine.Material("mat").Albedo(0xFF).Metallic(0.8).Send()`, it does **not** make 3 FFI calls.
1. It writes a `FuncProcId::CreateMaterial` opcode + `"mat"` string.
2. It loops through the builder methods, writing `MethodId::Albedo` + `0xFF`, then `MethodId::Metallic` + `0.8`.
3. It writes a `MethodId::Build` opcode.
   *Rust parses this sequentially in a single `loop { match ... }` block.*

### B. Block Iterators = Rust Closures
Because an OS pipe cannot transfer a function pointer, closures are represented chronologically.
*   **Go:** `for range Obj().KeepIter() { ...body... }`
*   **Rust:** The interpreter reads `Obj`, instantiates it, and calls `obj.show(|inner_ctx| { self.interpret_inner(inner_ctx) })`. The Rust interpreter recursively reads the OS pipe inside the closure until it sees the termination opcode emitted when Go's `for` loop ended.

### C. Retained State & Zero-Allocation Iterators
To achieve maximum speed, Go must avoid allocating slices when reading massive arrays of state (e.g., 10,000 particle positions) from the Rust IPC pipe.
*   FFFI2 uses Go generics (`T ~uint64`) and iterators (`iter.Seq`).
*   Instead of `ReadUint64Slice()[]uint64`, it uses `IterateUint64SliceRetr() iter.Seq[uint64]`. This yields directly from the `bufio.Reader` connected to the OS Pipe, bypassing the Go Garbage Collector entirely.
*   **Rule:** Always prefer `FetcherNode` for state retrieval, and utilize the generated iterator methods to update Go's logic state at the top of the frame loop.