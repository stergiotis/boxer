// Package geodesy is the WGS84 datum, Earth-centred Earth-fixed vectors,
// geodetic positions with ellipsoidal heights, and local east-north-up
// tangent frames.
//
// Every metric quantity on the Earth — a distance, a closing speed, a
// radial velocity, the vertical through a point — is a few lines once a
// position is a [Vec3] in ECEF metres and a direction has a frame to be
// named in. The package provides exactly that and no projection: the
// conversions [GeodeticToEcef] and [EcefToGeodetic], a [Frame] at an
// origin, which is a translation and a rotation of ECEF and so preserves
// every length and angle, and the same rotation at any point without a
// frame ([EnuToEcefAt], [EcefToEnuAt]) for vectors reported where they
// were measured.
//
// Heights are above the WGS84 ellipsoid (ADR-0356 §SD1). A terrain model
// referenced to a geoid and a barometric altitude are not, and must be
// lifted by the geoid undulation before they become a [Geodetic]; the
// package has no geoid and takes the undulation the caller knows.
//
// Everything is a value, float64, and allocation-free (ADR-0356 §SD4): a
// NaN in gives a NaN out, as the math package does, and a caller that
// wants a refusal checks its inputs where it knows what they mean.
//
// # Usage
//
// Build a frame at the point of interest, convert positions into it, and
// do the geometry on the vectors:
//
//	f := geodesy.NewFrame(geodesy.Geodetic{LatDeg: 47.4, LonDeg: 8.5, HeightM: 450})
//	p := f.EcefToEnu(geodesy.GeodeticToEcef(aircraft))
//	slantM := p.Norm()
//
// # See also
//
//   - EXPLANATION.md in this directory for the formulas and their
//     conditioning.
//   - doc/adr/0356-geodesy-wgs84-ecef-and-local-tangent-frames-as-a-science-package.md
//     for the decision.
package geodesy
