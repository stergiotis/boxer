// Package workstatelet holds the workstatelet document (ADR-0369, proposed):
// one markdown file that lists windows to open and, optionally, the desktop
// layout they take.
//
// The package is the document's parser and writer, the registry of books a
// package contributes documents through, the catalog of documents minted into
// launcher entries, and the store that keeps the documents a person captured
// from the desktop. It opens nothing: the window host reads a document out of
// the catalog and opens its windows (windowhost.OpenWorkstatelet), and writes
// one from the open windows (windowhost.CaptureWorkstatelet).
//
// A window's launch arguments are the launch config's facts-CBOR bytes in
// base64 (§SD2). This package decodes the base64 and bounds its size; whether
// the bytes are the claimed kind is the host's check at the open, where every
// launch config is checked.
package workstatelet
