// Package jpeg2000 decodes the JPEG 2000 Part 1 profile that GRIB2 template
// 5.40 producers emit (ADR-0292 §R6), from ITU-T T.800 alone: one
// tile, one component, the reversible 5/3 wavelet without quantization,
// one quality layer, any precinct partition under a resolution-major
// progression, and code-block style without arithmetic-coder bypass or
// per-pass termination. Every
// codestream in the survey behind the ADR — written by JasPer 1.7 to 2.0
// and by OpenJPEG 2.5 — fits it.
//
// The pieces are the ones the standard names: the main and tile-part
// headers of Annex A; the packet headers of Annex B with their tag trees;
// the MQ arithmetic decoder of Annex C; the three coding passes and their
// context formation of Annex D; the reconstruction of Annex E; the
// 2D_SR procedure of Annex F with periodic symmetric extension; and the
// DC level shift of Annex G. What lies outside the profile — the 9/7
// wavelet, several layers or tiles, the bypass and termination modes, regions of interest, progression changes, packed
// packet headers, multiple components — is [ErrUnsupported] with the
// feature's name; a codestream that contradicts itself is [ErrCorrupt].
package jpeg2000
