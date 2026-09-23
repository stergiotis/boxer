// Package lander consumes stevedore items from a topic and hands them to an
// application's [stevedore.SinkI] (ADR-0252 §SD5): it decodes each message's
// envelope, lands each item, and commits the batch's offset only after the
// sink's flush returned. A split body's parts land as they are, one item
// each, and a sink that wants the body whole assembles its durable part
// rows on read; holding parts in memory across an acknowledgement would
// lose them on a restart (ADR-0252, Updates). A permanent failure becomes a dead-letter row (ADR-0252 §SD3);
// a transient one is retried in place, then stops the lander with nothing
// acknowledged, so a restart redelivers.
//
// Delivery is therefore at-least-once. A sink keys its rows by the item's
// reference and ordinal, and a reader collapses on that key.
package lander
