// Package lander consumes stevedore items from a topic and hands them to an
// application's [stevedore.SinkI] (ADR-0252 §SD5): it decodes each message's
// envelope, reassembles bodies the pipeline split (ADR-0252 §SD4), lands
// each item, and commits the batch's offset only after the sink's flush
// returned. A permanent failure becomes a dead-letter row (ADR-0252 §SD3);
// a transient one is retried in place, then stops the lander with nothing
// acknowledged, so a restart redelivers.
//
// Delivery is therefore at-least-once. A sink keys its rows by the item's
// reference and ordinal, and a reader collapses on that key.
package lander
