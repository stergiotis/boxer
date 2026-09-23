// Package stevedorefacts is the facts-bound record store for what a stevedore
// lander writes on its own account (ADR-0252 §SD3, §SD5): the dead-letter
// kind, one row per message given up on. Every other row a lander produces is
// the application sink's.
//
// The store is generated over
// [github.com/stergiotis/boxer/public/streaming/stevedore/stevedorevocab] by
// the gen-test beside this file; it verifies the facts schema and never
// creates it (ADR-0184 §SD2).
package stevedorefacts
