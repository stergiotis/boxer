// Package harness holds the composition shapes an agent harness's seams
// declare (ADR-0310 §SD1, ADR-0310 §SD11 step 5): a chain runs its
// elements in declared order, never registration order; a first claim stops
// at the first element that answers; a guard combines its elements'
// decisions by maximum, so no element can lower another's and an error is
// the strongest refusal; a slot holds exactly one provider or a declared
// default; a registry refuses a duplicate id. Each shape carries its law as
// a test. The shapes are generic over the state they act on and carry no
// policy of their own: the chat's turn loop supplies the elements, and a
// profile (ADR-0310 §SD11 step 11) names which run.
//
// Design note: a chain is the one shape whose result depends on order, so
// an element declares its Order and the chain sorts by it at construction,
// refusing two elements at one position — the counter-example the design
// records is a hook table whose behaviour depended on load order without
// saying so. The commuting shapes need no order and accept elements in any.
package harness
