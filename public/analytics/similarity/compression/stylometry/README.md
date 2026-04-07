# stylometry — Compression-Based Authorship Analysis

Streaming authorship analysis built on top of the `compression` similarity package. Compares a fixed reference author's text against candidate texts using NCD/CCC metrics with automatic convergence detection.

## Measurement modes

- **Instance**: computes per-text NCD/CCC against the reference, collecting streaming statistics (mean, stddev, min, max). Stops early when the convergence detector determines the metric has stabilized.
- **Profile**: concatenates candidate texts to match the reference length, then computes a single NCD/CCC value. Useful when individual texts are too short for meaningful per-text measurement.

## Usage

```go
analyzer, _ := stylometry.NewAnalyzer(referenceText, convergenceDetector, compressor)
_, _, _, meanNcd, _, _, converged, _ := analyzer.MeasureNcdInstance(candidateTexts)
```

The `Analyzer` embeds `compression.Similarity`, so all low-level measurement methods (`MeasureCompressedLength`, `MeasureJointCompressedLength`, etc.) are directly accessible.
