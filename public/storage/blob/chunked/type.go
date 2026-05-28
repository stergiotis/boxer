package chunked

import (
	"github.com/stergiotis/boxer/public/identity/identifier"
)

// ChunkedWriterI id will always be the same across all interrelated chunks
type ChunkedWriterI interface {
	// WriteFirstChunk May never be called, see WriterFirstAndLastChunk
	WriteFirstChunk(id identifier.TaggedId, p []byte) (n int, err error)
	// WriteIntermediateChunk sizeSoFar includes the current chunk
	WriteIntermediateChunk(id identifier.TaggedId, p []byte, chunkIndex uint32, sizeSoFar int64) (n int, err error)
	// WriteLastChunk Will never called with an empty payload. totalChunks as well as totalSize include the last chunk
	WriteLastChunk(id identifier.TaggedId, p []byte, totalChunks uint32, totalSize int64) (n int, err error)
	// WriteFirstAndLastChunk Will never called with and empty payload
	WriteFirstAndLastChunk(id identifier.TaggedId, p []byte) (n int, err error)
}
