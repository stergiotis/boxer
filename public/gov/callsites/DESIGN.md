# Resources
## Devirtualization
* Implementation: https://go.dev/src/cmd/compile/internal/devirtualize/devirtualize.go
* Overview article: https://www.polarsignals.com/blog/posts/2023/11/24/go-interface-devirtualization-and-pgo

## Monomorphization
* Definition: https://en.wikipedia.org/wiki/Monomorphization
* Good article: https://planetscale.com/blog/generics-can-make-your-go-code-slower

> DO try to de-duplicate identical methods that take a string and a []byte using a ByteSeq constraint. 
> The generated shape instantiation is very close to manually writing two almost-identical functions.
 
> DO use generics in data structures. This is by far their best use case: Generic data structures that were previously
> implemented using interface{} are complex and un-ergonomic. Removing the type assertions and storing types unboxed
> in a type-safe way makes these data structures both easier to use and more performant.

> DO attempt to parametrize functional helpers by their callback types. In some cases, it may allow the Go compiler to flatten them.

> DO NOT attempt to use Generics to de-virtualize or inline method calls. It doesn’t work because there’s a single shape 
> for all pointer types that can be passed to the generic function; the associated method information lives in a runtime dictionary.

> DO NOT pass an interface to a generic function, under any circumstances. Because of the way shape instantiation works for interfaces,
> instead of de-virtualizing, you’re adding another virtualization layer that involves a global hash table lookup for every method call. When dealing with Generics in a performance-sensitive context, use only pointers instead of interfaces.
> **NOTE: THIS MAY BE OUTDATED (was true in go 1.18)???**

> DO NOT rewrite interface-based APIs to use Generics. Given the current constraints of the implementation, any code that currently
> uses non-empty interfaces will behave more predictably, and will be simpler, if it continues using interfaces. When it comes to method calls,
> Generics devolve pointers into twice-indirect interfaces, and interfaces into… well, something quite horrifying, if I’m being honest.
> **NOTE: THIS MAY BE OUTDATED (was true in go 1.18)???**

* Monomorphizer in CockroachDB https://github.com/cockroachdb/cockroach/blob/master/pkg/sql/colexec/execgen/execgen.go
