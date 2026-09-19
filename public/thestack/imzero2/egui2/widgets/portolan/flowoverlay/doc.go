// Package flowoverlay draws a gridded vector field — wind, a current — on a
// portolan map as particles drifting along it, each trailing a short fading
// stroke (ADR-0249). It is a guest in the map's overlay callback like
// h3overlay and landoverlay: the map does not import it, it takes no pointer
// and no key, and whether it lies under or over another guest is the caller's
// call order.
//
//	layer := flowoverlay.New(src, flowoverlay.Options{})
//	defer layer.Close()
//	// every frame:
//	m.Render(w, h, func(p portolan.Projector) { layer.Draw(p) })
//
// What the picture does not say, first.
//
// The animation shows direction and relative speed, not transport. A
// particle's pace is a clamped linear function of the field's magnitude in
// screen pixels, the same at every zoom and latitude, because a physically
// scaled particle would not visibly move at world zoom and would be a smear
// at street zoom. It does not cover the ground the wind would. Colour, which
// follows the scalar mean of the magnitude, is the honest channel for speed.
//
// A trail is not a trajectory. A particle crosses the screen while the
// field's clock stands still, so a trail is a streamlet of the field as it
// is at the display time: where the flow points now, not where the air will
// be.
//
// Between two time steps the field is blended linearly, and that is wrong for
// anything that moves: a front between two steps fades out in one place and
// in at the other instead of travelling. At hourly steps it does not show; at
// six-hourly steps a fast system is a double image.
//
// At a low zoom a sample stands for a large area, and the particles follow
// the vector mean over it, which is short where directions disagree inside
// the sample. Small vortices are absent from a coarse level because they are
// smaller than its samples.
//
// A window showing an animating layer never idles; Options.Paused stops the
// repaint requests and leaves the last trails on screen.
//
// How it works. The layer asks its source for a window a little larger than
// the view, off the render thread, at one sample per few screen pixels, and
// keeps the last good one on screen meanwhile. The window is resampled once
// into a grid regular in the view's projected coordinates, with each vector
// carried through the projection — so direction is right in any CRS portolan
// has, and the screen's downward y is in that map and not in a sign someone
// remembers — after which a tick is arithmetic: no trigonometry per particle.
// Particles live in projected world coordinates in float64, so a pan moves
// nothing and a high zoom does not quantise them. The simulation advances on
// a fixed tick and a trail gains a point per tick, so pace and trail length
// do not depend on the display's refresh rate. The integrator is the
// midpoint rule: forward Euler lengthens the radius on every step round a
// closed circulation and so draws a cyclone as a source. A particle starts
// at a random age, is re-seeded each tick with a probability that rises with
// its speed, and has a hard maximum age; it is re-seeded uniformly over the
// viewport, which keeps the density uniform on screen.
package flowoverlay
