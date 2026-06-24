// Package imzhost provides reusable imzero2 host-side helpers shared by the
// carousel demo and elle's cmd host: the shared window chrome
// (DecorateRenderer / ChromeConfig), the per-app windowed renderer adapter
// (AdaptToRenderer / WindowDefaultSize), and the screenshot-tour renderers
// (AdaptBodyOnly / DecorateScreenshotRenderer / ScreenshotStageSize).
//
// These were extracted from the carousel and elle hosts so both drive the
// same chrome and adapter code rather than maintaining divergent copies.
package imzhost
