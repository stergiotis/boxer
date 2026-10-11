// Package tokenmeter is the one estimate of how many tokens a request
// takes (ADR-0310 §SD5): anchored on the provider's reported usage where
// a call reported one, and bytes over four with a flat allowance per
// image where none did. The chat's context bar, the moderator's context
// signal and ration's admission estimate read it, so the three agree on
// one number for one history; each keeps its own threshold.
//
// Design note: the estimator is deliberately crude. A tokenizer per
// provider would be exact for that provider and wrong for the next, and
// every reader here compares against a threshold it sets itself, so a
// consistent estimate matters more than a precise one. The anchor on
// reported usage is where precision comes from once a call has answered.
package tokenmeter
