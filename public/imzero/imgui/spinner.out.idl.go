//go:build fffi_idl_code

package imgui

func SpinnerRainbow(label string, radius float32, thickness float32, color uint32, speed float32) {
	_ = `ImSpinner::SpinnerRainbow(label, radius, thickness, color, speed)`
}
func SpinnerRainbowV(label string, radius float32, thickness float32, color uint32, speed float32, ang_min float32 /* = 0.f*/, ang_max float32 /* = PI_2*/, arcs int /* = 1*/) {
	_ = `ImSpinner::SpinnerRainbow(label, radius, thickness, color, speed, ang_min, ang_max, arcs)`
}
func SpinnerRainbowMix(label string, radius float32, thickness float32, color uint32, speed float32) {
	_ = `ImSpinner::SpinnerRainbowMix(label, radius, thickness, color, speed)`
}
func SpinnerRainbowMixV(label string, radius float32, thickness float32, color uint32, speed float32, ang_min float32 /* = 0.f*/, ang_max float32 /* = PI_2*/, arcs int /* = 1*/, mode int /* = 0*/) {
	_ = `ImSpinner::SpinnerRainbowMix(label, radius, thickness, color, speed, ang_min, ang_max, arcs, mode)`
}
func SpinnerRotatingHeart(label string, radius float32, thickness float32, color uint32, speed float32) {
	_ = `ImSpinner::SpinnerRotatingHeart(label, radius, thickness, color, speed)`
}
func SpinnerRotatingHeartV(label string, radius float32, thickness float32, color uint32, speed float32, ang_min float32 /* = 0.f*/) {
	_ = `ImSpinner::SpinnerRotatingHeart(label, radius, thickness, color, speed, ang_min)`
}
func SpinnerAng(label string, radius float32, thickness float32) {
	_ = `ImSpinner::SpinnerAng(label, radius, thickness)`
}
func SpinnerAngV(label string, radius float32, thickness float32, color uint32 /* = white*/, bg uint32 /* = white*/, speed float32 /* = 2.8f*/, angle float32 /* = IM_PI*/, mode int /* = 0*/) {
	_ = `ImSpinner::SpinnerAng(label, radius, thickness, color, bg, speed, angle, mode)`
}
func SpinnerAngMix(label string, radius float32, thickness float32) {
	_ = `ImSpinner::SpinnerAngMix(label, radius, thickness)`
}
func SpinnerAngMixV(label string, radius float32, thickness float32, color uint32 /* = white*/, speed float32 /* = 2.8f*/, angle float32 /* = IM_PI*/, arcs int /* = 4*/, mode int /* = 0*/) {
	_ = `ImSpinner::SpinnerAngMix(label, radius, thickness, color, speed, angle, arcs, mode)`
}
func SpinnerLoadingRing(label string, radius float32, thickness float32) {
	_ = `ImSpinner::SpinnerLoadingRing(label, radius, thickness)`
}
func SpinnerLoadingRingV(label string, radius float32, thickness float32, color uint32 /* = white*/, bg uint32 /* = half_white*/, speed float32 /* = 2.8f*/, segments int /* = 5*/) {
	_ = `ImSpinner::SpinnerLoadingRing(label, radius, thickness, color, bg, speed, segments)`
}
func SpinnerClock(label string, radius float32, thickness float32) {
	_ = `ImSpinner::SpinnerClock(label, radius, thickness)`
}
func SpinnerClockV(label string, radius float32, thickness float32, color uint32 /* = white*/, bg uint32 /* = half_white*/, speed float32 /* = 2.8f*/) {
	_ = `ImSpinner::SpinnerClock(label, radius, thickness, color, bg, speed)`
}
func SpinnerPulsar(label string, radius float32, thickness float32) {
	_ = `ImSpinner::SpinnerPulsar(label, radius, thickness)`
}
func SpinnerPulsarV(label string, radius float32, thickness float32, bg uint32 /* = half_white*/, speed float32 /* = 2.8f*/, sequence bool /* = true*/) {
	_ = `ImSpinner::SpinnerPulsar(label, radius, thickness, bg, speed, sequence)`
}
func SpinnerTwinPulsar(label string, radius float32, thickness float32) {
	_ = `ImSpinner::SpinnerTwinPulsar(label, radius, thickness)`
}
func SpinnerTwinPulsarV(label string, radius float32, thickness float32, color uint32 /* = white*/, speed float32 /* = 2.8f*/, rings int /* = 2*/) {
	_ = `ImSpinner::SpinnerTwinPulsar(label, radius, thickness, color, speed, rings)`
}
func SpinnerFadePulsar(label string, radius float32) {
	_ = `ImSpinner::SpinnerFadePulsar(label, radius)`
}
func SpinnerFadePulsarV(label string, radius float32, color uint32 /* = white*/, speed float32 /* = 2.8f*/, rings int /* = 2*/) {
	_ = `ImSpinner::SpinnerFadePulsar(label, radius, color, speed, rings)`
}
func SpinnerCircularLines(label string, radius float32) {
	_ = `ImSpinner::SpinnerCircularLines(label, radius)`
}
func SpinnerCircularLinesV(label string, radius float32, color uint32 /* = white*/, speed float32 /* = 1.8f*/, lines int /* = 8*/) {
	_ = `ImSpinner::SpinnerCircularLines(label, radius, color, speed, lines)`
}
func SpinnerVDots(label string, radius float32, thickness float32) {
	_ = `ImSpinner::SpinnerVDots(label, radius, thickness)`
}
func SpinnerVDotsV(label string, radius float32, thickness float32, color uint32 /* = white*/, bgcolor uint32 /* = white*/, speed float32 /* = 2.8f*/, dots Size_t /* = 12*/, mdots Size_t /* = 6*/) {
	_ = `ImSpinner::SpinnerVDots(label, radius, thickness, color, bgcolor, speed, dots, mdots)`
}
func SpinnerBounceDots(label string, radius float32, thickness float32) {
	_ = `ImSpinner::SpinnerBounceDots(label, radius, thickness)`
}
func SpinnerBounceDotsV(label string, radius float32, thickness float32, color uint32 /* = white*/, speed float32 /* = 2.8f*/, dots Size_t /* = 3*/, mode int /* = 0*/) {
	_ = `ImSpinner::SpinnerBounceDots(label, radius, thickness, color, speed, dots, mode)`
}
func SpinnerZipDots(label string, radius float32, thickness float32) {
	_ = `ImSpinner::SpinnerZipDots(label, radius, thickness)`
}
func SpinnerZipDotsV(label string, radius float32, thickness float32, color uint32 /* = white*/, speed float32 /* = 2.8f*/, dots Size_t /* = 5*/) {
	_ = `ImSpinner::SpinnerZipDots(label, radius, thickness, color, speed, dots)`
}
func SpinnerDotsToPoints(label string, radius float32, thickness float32, offset_k float32) {
	_ = `ImSpinner::SpinnerDotsToPoints(label, radius, thickness, offset_k)`
}
func SpinnerDotsToPointsV(label string, radius float32, thickness float32, offset_k float32, color uint32 /* = white*/, speed float32 /* = 1.8f*/, dots Size_t /* = 5*/) {
	_ = `ImSpinner::SpinnerDotsToPoints(label, radius, thickness, offset_k, color, speed, dots)`
}
func SpinnerDotsToBar(label string, radius float32, thickness float32, offset_k float32) {
	_ = `ImSpinner::SpinnerDotsToBar(label, radius, thickness, offset_k)`
}
func SpinnerDotsToBarV(label string, radius float32, thickness float32, offset_k float32, color uint32 /* = white*/, speed float32 /* = 2.8f*/, dots Size_t /* = 5*/) {
	_ = `ImSpinner::SpinnerDotsToBar(label, radius, thickness, offset_k, color, speed, dots)`
}
func SpinnerWaveDots(label string, radius float32, thickness float32) {
	_ = `ImSpinner::SpinnerWaveDots(label, radius, thickness)`
}
func SpinnerWaveDotsV(label string, radius float32, thickness float32, color uint32 /* = white*/, speed float32 /* = 2.8f*/, lt int /* = 8*/) {
	_ = `ImSpinner::SpinnerWaveDots(label, radius, thickness, color, speed, lt)`
}
func SpinnerFadeDots(label string, radius float32, thickness float32) {
	_ = `ImSpinner::SpinnerFadeDots(label, radius, thickness)`
}
func SpinnerFadeDotsV(label string, radius float32, thickness float32, color uint32 /* = white*/, speed float32 /* = 2.8f*/, lt int /* = 8*/, mode int /* = 0*/) {
	_ = `ImSpinner::SpinnerFadeDots(label, radius, thickness, color, speed, lt, mode)`
}
func SpinnerThreeDots(label string, radius float32, thickness float32) {
	_ = `ImSpinner::SpinnerThreeDots(label, radius, thickness)`
}
func SpinnerThreeDotsV(label string, radius float32, thickness float32, color uint32 /* = white*/, speed float32 /* = 2.8f*/, lt int /* = 8*/) {
	_ = `ImSpinner::SpinnerThreeDots(label, radius, thickness, color, speed, lt)`
}
func SpinnerFiveDots(label string, radius float32, thickness float32) {
	_ = `ImSpinner::SpinnerFiveDots(label, radius, thickness)`
}
func SpinnerFiveDotsV(label string, radius float32, thickness float32, color uint32 /* = 0xffffffff*/, speed float32 /* = 2.8f*/, lt int /* = 8*/) {
	_ = `ImSpinner::SpinnerFiveDots(label, radius, thickness, color, speed, lt)`
}
func Spinner4Caleidospcope(label string, radius float32, thickness float32) {
	_ = `ImSpinner::Spinner4Caleidospcope(label, radius, thickness)`
}
func Spinner4CaleidospcopeV(label string, radius float32, thickness float32, color uint32 /* = 0xffffffff*/, speed float32 /* = 2.8f*/, lt int /* = 8*/) {
	_ = `ImSpinner::Spinner4Caleidospcope(label, radius, thickness, color, speed, lt)`
}
func SpinnerMultiFadeDots(label string, radius float32, thickness float32) {
	_ = `ImSpinner::SpinnerMultiFadeDots(label, radius, thickness)`
}
func SpinnerMultiFadeDotsV(label string, radius float32, thickness float32, color uint32 /* = white*/, speed float32 /* = 2.8f*/, lt int /* = 8*/) {
	_ = `ImSpinner::SpinnerMultiFadeDots(label, radius, thickness, color, speed, lt)`
}
func SpinnerScaleDots(label string, radius float32, thickness float32) {
	_ = `ImSpinner::SpinnerScaleDots(label, radius, thickness)`
}
func SpinnerScaleDotsV(label string, radius float32, thickness float32, color uint32 /* = white*/, speed float32 /* = 2.8f*/, lt int /* = 8*/) {
	_ = `ImSpinner::SpinnerScaleDots(label, radius, thickness, color, speed, lt)`
}
func SpinnerSquareSpins(label string, radius float32, thickness float32) {
	_ = `ImSpinner::SpinnerSquareSpins(label, radius, thickness)`
}
func SpinnerSquareSpinsV(label string, radius float32, thickness float32, color uint32 /* = white*/, speed float32 /* = 2.8f*/) {
	_ = `ImSpinner::SpinnerSquareSpins(label, radius, thickness, color, speed)`
}
func SpinnerMovingDots(label string, radius float32, thickness float32) {
	_ = `ImSpinner::SpinnerMovingDots(label, radius, thickness)`
}
func SpinnerMovingDotsV(label string, radius float32, thickness float32, color uint32 /* = white*/, speed float32 /* = 2.8f*/, dots Size_t /* = 3*/) {
	_ = `ImSpinner::SpinnerMovingDots(label, radius, thickness, color, speed, dots)`
}
func SpinnerRotateDots(label string, radius float32, thickness float32) {
	_ = `ImSpinner::SpinnerRotateDots(label, radius, thickness)`
}
func SpinnerRotateDotsV(label string, radius float32, thickness float32, color uint32 /* = white*/, speed float32 /* = 2.8f*/, dots int /* = 2*/, mode int /* = 0*/) {
	_ = `ImSpinner::SpinnerRotateDots(label, radius, thickness, color, speed, dots, mode)`
}
func SpinnerOrionDots(label string, radius float32, thickness float32) {
	_ = `ImSpinner::SpinnerOrionDots(label, radius, thickness)`
}
func SpinnerOrionDotsV(label string, radius float32, thickness float32, color uint32 /* = white*/, speed float32 /* = 2.8f*/, arcs int /* = 4*/) {
	_ = `ImSpinner::SpinnerOrionDots(label, radius, thickness, color, speed, arcs)`
}
func SpinnerGalaxyDots(label string, radius float32, thickness float32) {
	_ = `ImSpinner::SpinnerGalaxyDots(label, radius, thickness)`
}
func SpinnerGalaxyDotsV(label string, radius float32, thickness float32, color uint32 /* = white*/, speed float32 /* = 2.8f*/, arcs int /* = 4*/) {
	_ = `ImSpinner::SpinnerGalaxyDots(label, radius, thickness, color, speed, arcs)`
}
func SpinnerTwinAng(label string, radius1 float32, radius2 float32, thickness float32) {
	_ = `ImSpinner::SpinnerTwinAng(label, radius1, radius2, thickness)`
}
func SpinnerTwinAngV(label string, radius1 float32, radius2 float32, thickness float32, color1 uint32 /* = white*/, color2 uint32 /* = red*/, speed float32 /* = 2.8f*/, angle float32 /* = IM_PI*/) {
	_ = `ImSpinner::SpinnerTwinAng(label, radius1, radius2, thickness, color1, color2, speed, angle)`
}
func SpinnerFilling(label string, radius float32, thickness float32) {
	_ = `ImSpinner::SpinnerFilling(label, radius, thickness)`
}
func SpinnerFillingV(label string, radius float32, thickness float32, color1 uint32 /* = white*/, color2 uint32 /* = red*/, speed float32 /* = 2.8f*/) {
	_ = `ImSpinner::SpinnerFilling(label, radius, thickness, color1, color2, speed)`
}
func SpinnerTopup(label string, radius1 float32, radius2 float32) {
	_ = `ImSpinner::SpinnerTopup(label, radius1, radius2)`
}
func SpinnerTopupV(label string, radius1 float32, radius2 float32, color uint32 /* = red*/, fg uint32 /* = white*/, bg uint32 /* = white*/, speed float32 /* = 2.8f*/) {
	_ = `ImSpinner::SpinnerTopup(label, radius1, radius2, color, fg, bg, speed)`
}
func SpinnerTwinAng180(label string, radius1 float32, radius2 float32, thickness float32) {
	_ = `ImSpinner::SpinnerTwinAng180(label, radius1, radius2, thickness)`
}
func SpinnerTwinAng180V(label string, radius1 float32, radius2 float32, thickness float32, color1 uint32 /* = white*/, color2 uint32 /* = red*/, speed float32 /* = 2.8f*/) {
	_ = `ImSpinner::SpinnerTwinAng180(label, radius1, radius2, thickness, color1, color2, speed)`
}
func SpinnerTwinAng360(label string, radius1 float32, radius2 float32, thickness float32) {
	_ = `ImSpinner::SpinnerTwinAng360(label, radius1, radius2, thickness)`
}
func SpinnerTwinAng360V(label string, radius1 float32, radius2 float32, thickness float32, color1 uint32 /* = white*/, color2 uint32 /* = red*/, speed1 float32 /* = 2.8f*/, speed2 float32 /* = 2.5f*/, mode int /* = 0*/) {
	_ = `ImSpinner::SpinnerTwinAng360(label, radius1, radius2, thickness, color1, color2, speed1, speed2, mode)`
}
func SpinnerIncDots(label string, radius float32, thickness float32) {
	_ = `ImSpinner::SpinnerIncDots(label, radius, thickness)`
}
func SpinnerIncDotsV(label string, radius float32, thickness float32, color uint32 /* = white*/, speed float32 /* = 2.8f*/, dots Size_t /* = 6*/) {
	_ = `ImSpinner::SpinnerIncDots(label, radius, thickness, color, speed, dots)`
}
func SpinnerIncFullDots(label string, radius float32, thickness float32) {
	_ = `ImSpinner::SpinnerIncFullDots(label, radius, thickness)`
}
func SpinnerIncFullDotsV(label string, radius float32, thickness float32, color uint32 /* = white*/, speed float32 /* = 2.8f*/, dots Size_t /* = 4*/) {
	_ = `ImSpinner::SpinnerIncFullDots(label, radius, thickness, color, speed, dots)`
}
func SpinnerFadeBars(label string, w float32) {
	_ = `ImSpinner::SpinnerFadeBars(label, w)`
}
func SpinnerFadeBarsV(label string, w float32, color uint32 /* = white*/, speed float32 /* = 2.8f*/, bars Size_t /* = 3*/, scale bool /* = false*/) {
	_ = `ImSpinner::SpinnerFadeBars(label, w, color, speed, bars, scale)`
}
func SpinnerFadeTris(label string, radius float32) {
	_ = `ImSpinner::SpinnerFadeTris(label, radius)`
}
func SpinnerFadeTrisV(label string, radius float32, color uint32 /* = white*/, speed float32 /* = 2.8f*/, dim Size_t /* = 2*/, scale bool /* = false*/) {
	_ = `ImSpinner::SpinnerFadeTris(label, radius, color, speed, dim, scale)`
}
func SpinnerBarsRotateFade(label string, rmin float32, rmax float32, thickness float32) {
	_ = `ImSpinner::SpinnerBarsRotateFade(label, rmin, rmax, thickness)`
}
func SpinnerBarsRotateFadeV(label string, rmin float32, rmax float32, thickness float32, color uint32 /* = white*/, speed float32 /* = 2.8f*/, bars Size_t /* = 6*/) {
	_ = `ImSpinner::SpinnerBarsRotateFade(label, rmin, rmax, thickness, color, speed, bars)`
}
func SpinnerBarsScaleMiddle(label string, w float32) {
	_ = `ImSpinner::SpinnerBarsScaleMiddle(label, w)`
}
func SpinnerBarsScaleMiddleV(label string, w float32, color uint32 /* = white*/, speed float32 /* = 2.8f*/, bars Size_t /* = 3*/) {
	_ = `ImSpinner::SpinnerBarsScaleMiddle(label, w, color, speed, bars)`
}
func SpinnerAngTwin(label string, radius1 float32, radius2 float32, thickness float32) {
	_ = `ImSpinner::SpinnerAngTwin(label, radius1, radius2, thickness)`
}
func SpinnerAngTwinV(label string, radius1 float32, radius2 float32, thickness float32, color uint32 /* = white*/, bg uint32 /* = half_white*/, speed float32 /* = 2.8f*/, angle float32 /* = IM_PI*/, arcs Size_t /* = 1*/, mode int /* = 0*/) {
	_ = `ImSpinner::SpinnerAngTwin(label, radius1, radius2, thickness, color, bg, speed, angle, arcs, mode)`
}
func SpinnerArcRotation(label string, radius float32, thickness float32) {
	_ = `ImSpinner::SpinnerArcRotation(label, radius, thickness)`
}
func SpinnerArcRotationV(label string, radius float32, thickness float32, color uint32 /* = white*/, speed float32 /* = 2.8f*/, arcs Size_t /* = 4*/, mode int /* = 0*/) {
	_ = `ImSpinner::SpinnerArcRotation(label, radius, thickness, color, speed, arcs, mode)`
}
func SpinnerArcFade(label string, radius float32, thickness float32) {
	_ = `ImSpinner::SpinnerArcFade(label, radius, thickness)`
}
func SpinnerArcFadeV(label string, radius float32, thickness float32, color uint32 /* = white*/, speed float32 /* = 2.8f*/, arcs Size_t /* = 4*/) {
	_ = `ImSpinner::SpinnerArcFade(label, radius, thickness, color, speed, arcs)`
}
func SpinnerSimpleArcFade(label string, radius float32, thickness float32) {
	_ = `ImSpinner::SpinnerSimpleArcFade(label, radius, thickness)`
}
func SpinnerSimpleArcFadeV(label string, radius float32, thickness float32, color uint32 /* = white*/, speed float32 /* = 2.8f*/) {
	_ = `ImSpinner::SpinnerSimpleArcFade(label, radius, thickness, color, speed)`
}
func SpinnerSquareStrokeFade(label string, radius float32, thickness float32) {
	_ = `ImSpinner::SpinnerSquareStrokeFade(label, radius, thickness)`
}
func SpinnerSquareStrokeFadeV(label string, radius float32, thickness float32, color uint32 /* = white*/, speed float32 /* = 2.8f*/) {
	_ = `ImSpinner::SpinnerSquareStrokeFade(label, radius, thickness, color, speed)`
}
func SpinnerAsciiSymbolPoints(label string, text string, radius float32, thickness float32) {
	_ = `ImSpinner::SpinnerAsciiSymbolPoints(label, text, radius, thickness)`
}
func SpinnerAsciiSymbolPointsV(label string, text string, radius float32, thickness float32, color uint32 /* = white*/, speed float32 /* = 2.8f*/) {
	_ = `ImSpinner::SpinnerAsciiSymbolPoints(label, text, radius, thickness, color, speed)`
}
func SpinnerTextFading(label string, text string, radius float32, fsize float32) {
	_ = `ImSpinner::SpinnerTextFading(label, text, radius, fsize)`
}
func SpinnerTextFadingV(label string, text string, radius float32, fsize float32, color uint32 /* = white*/, speed float32 /* = 2.8f*/) {
	_ = `ImSpinner::SpinnerTextFading(label, text, radius, fsize, color, speed)`
}
func SpinnerSevenSegments(label string, text string, radius float32, thickness float32) {
	_ = `ImSpinner::SpinnerSevenSegments(label, text, radius, thickness)`
}
func SpinnerSevenSegmentsV(label string, text string, radius float32, thickness float32, color uint32 /* = white*/, speed float32 /* = 2.8f*/) {
	_ = `ImSpinner::SpinnerSevenSegments(label, text, radius, thickness, color, speed)`
}
func SpinnerSquareStrokeFill(label string, radius float32, thickness float32) {
	_ = `ImSpinner::SpinnerSquareStrokeFill(label, radius, thickness)`
}
func SpinnerSquareStrokeFillV(label string, radius float32, thickness float32, color uint32 /* = white*/, speed float32 /* = 2.8f*/) {
	_ = `ImSpinner::SpinnerSquareStrokeFill(label, radius, thickness, color, speed)`
}
func SpinnerSquareStrokeLoading(label string, radius float32, thickness float32) {
	_ = `ImSpinner::SpinnerSquareStrokeLoading(label, radius, thickness)`
}
func SpinnerSquareStrokeLoadingV(label string, radius float32, thickness float32, color uint32 /* = white*/, speed float32 /* = 2.8f*/) {
	_ = `ImSpinner::SpinnerSquareStrokeLoading(label, radius, thickness, color, speed)`
}
func SpinnerSquareLoading(label string, radius float32, thickness float32) {
	_ = `ImSpinner::SpinnerSquareLoading(label, radius, thickness)`
}
func SpinnerSquareLoadingV(label string, radius float32, thickness float32, color uint32 /* = white*/, speed float32 /* = 2.8f*/) {
	_ = `ImSpinner::SpinnerSquareLoading(label, radius, thickness, color, speed)`
}
func SpinnerFilledArcFade(label string, radius float32) {
	_ = `ImSpinner::SpinnerFilledArcFade(label, radius)`
}
func SpinnerFilledArcFadeV(label string, radius float32, color uint32 /* = white*/, speed float32 /* = 2.8f*/, arcs Size_t /* = 4*/, mode int /* = 0*/) {
	_ = `ImSpinner::SpinnerFilledArcFade(label, radius, color, speed, arcs, mode)`
}
func SpinnerPointsArcBounce(label string, radius float32, thickness float32) {
	_ = `ImSpinner::SpinnerPointsArcBounce(label, radius, thickness)`
}
func SpinnerPointsArcBounceV(label string, radius float32, thickness float32, color uint32 /* = white*/, speed float32 /* = 2.8f*/, points Size_t /* = 4*/, circles int /* = 2*/, rspeed float32 /* = 0.f*/) {
	_ = `ImSpinner::SpinnerPointsArcBounce(label, radius, thickness, color, speed, points, circles, rspeed)`
}
func SpinnerFilledArcColor(label string, radius float32) {
	_ = `ImSpinner::SpinnerFilledArcColor(label, radius)`
}
func SpinnerFilledArcColorV(label string, radius float32, color uint32 /* = red*/, bg uint32 /* = white*/, speed float32 /* = 2.8f*/, arcs Size_t /* = 4*/) {
	_ = `ImSpinner::SpinnerFilledArcColor(label, radius, color, bg, speed, arcs)`
}
func SpinnerFilledArcRing(label string, radius float32, thickness float32) {
	_ = `ImSpinner::SpinnerFilledArcRing(label, radius, thickness)`
}
func SpinnerFilledArcRingV(label string, radius float32, thickness float32, color uint32 /* = red*/, bg uint32 /* = white*/, speed float32 /* = 2.8f*/, arcs Size_t /* = 4*/) {
	_ = `ImSpinner::SpinnerFilledArcRing(label, radius, thickness, color, bg, speed, arcs)`
}
func SpinnerArcWedges(label string, radius float32) {
	_ = `ImSpinner::SpinnerArcWedges(label, radius)`
}
func SpinnerArcWedgesV(label string, radius float32, color uint32 /* = red*/, speed float32 /* = 2.8f*/, arcs Size_t /* = 4*/) {
	_ = `ImSpinner::SpinnerArcWedges(label, radius, color, speed, arcs)`
}
func SpinnerTwinBall(label string, radius1 float32, radius2 float32, thickness float32, b_thickness float32) {
	_ = `ImSpinner::SpinnerTwinBall(label, radius1, radius2, thickness, b_thickness)`
}
func SpinnerTwinBallV(label string, radius1 float32, radius2 float32, thickness float32, b_thickness float32, ball uint32 /* = white*/, bg uint32 /* = half_white*/, speed float32 /* = 2.8f*/, balls Size_t /* = 2*/) {
	_ = `ImSpinner::SpinnerTwinBall(label, radius1, radius2, thickness, b_thickness, ball, bg, speed, balls)`
}
func SpinnerSolarBalls(label string, radius float32, thickness float32) {
	_ = `ImSpinner::SpinnerSolarBalls(label, radius, thickness)`
}
func SpinnerSolarBallsV(label string, radius float32, thickness float32, ball uint32 /* = white*/, bg uint32 /* = half_white*/, speed float32 /* = 2.8f*/, balls Size_t /* = 4*/) {
	_ = `ImSpinner::SpinnerSolarBalls(label, radius, thickness, ball, bg, speed, balls)`
}
func SpinnerSolarScaleBalls(label string, radius float32, thickness float32) {
	_ = `ImSpinner::SpinnerSolarScaleBalls(label, radius, thickness)`
}
func SpinnerSolarScaleBallsV(label string, radius float32, thickness float32, ball uint32 /* = white*/, speed float32 /* = 2.8f*/, balls Size_t /* = 4*/) {
	_ = `ImSpinner::SpinnerSolarScaleBalls(label, radius, thickness, ball, speed, balls)`
}
func SpinnerSolarArcs(label string, radius float32, thickness float32) {
	_ = `ImSpinner::SpinnerSolarArcs(label, radius, thickness)`
}
func SpinnerSolarArcsV(label string, radius float32, thickness float32, ball uint32 /* = white*/, bg uint32 /* = half_white*/, speed float32 /* = 2.8f*/, balls Size_t /* = 4*/) {
	_ = `ImSpinner::SpinnerSolarArcs(label, radius, thickness, ball, bg, speed, balls)`
}
func SpinnerMovingArcs(label string, radius float32, thickness float32) {
	_ = `ImSpinner::SpinnerMovingArcs(label, radius, thickness)`
}
func SpinnerMovingArcsV(label string, radius float32, thickness float32, color uint32 /* = white*/, speed float32 /* = 2.8f*/, arcs Size_t /* = 4*/) {
	_ = `ImSpinner::SpinnerMovingArcs(label, radius, thickness, color, speed, arcs)`
}
func SpinnerRainbowCircle(label string, radius float32, thickness float32) {
	_ = `ImSpinner::SpinnerRainbowCircle(label, radius, thickness)`
}
func SpinnerRainbowCircleV(label string, radius float32, thickness float32, color uint32 /* = white*/, speed float32 /* = 2.8f*/, arcs Size_t /* = 4*/, mode float32 /* = 1*/) {
	_ = `ImSpinner::SpinnerRainbowCircle(label, radius, thickness, color, speed, arcs, mode)`
}
func SpinnerBounceBall(label string, radius float32, thickness float32) {
	_ = `ImSpinner::SpinnerBounceBall(label, radius, thickness)`
}
func SpinnerBounceBallV(label string, radius float32, thickness float32, color uint32 /* = white*/, speed float32 /* = 2.8f*/, dots int /* = 1*/, shadow bool /* = false*/) {
	_ = `ImSpinner::SpinnerBounceBall(label, radius, thickness, color, speed, dots, shadow)`
}
func SpinnerPulsarBall(label string, radius float32, thickness float32) {
	_ = `ImSpinner::SpinnerPulsarBall(label, radius, thickness)`
}
func SpinnerPulsarBallV(label string, radius float32, thickness float32, color uint32 /* = white*/, speed float32 /* = 2.8f*/, shadow bool /* = false*/, mode int /* = 0*/) {
	_ = `ImSpinner::SpinnerPulsarBall(label, radius, thickness, color, speed, shadow, mode)`
}
func SpinnerIncScaleDots(label string, radius float32, thickness float32) {
	_ = `ImSpinner::SpinnerIncScaleDots(label, radius, thickness)`
}
func SpinnerIncScaleDotsV(label string, radius float32, thickness float32, color uint32 /* = white*/, speed float32 /* = 2.8f*/, dots Size_t /* = 6*/) {
	_ = `ImSpinner::SpinnerIncScaleDots(label, radius, thickness, color, speed, dots)`
}
func SpinnerSomeScaleDots(label string, radius float32, thickness float32) {
	_ = `ImSpinner::SpinnerSomeScaleDots(label, radius, thickness)`
}
func SpinnerSomeScaleDotsV(label string, radius float32, thickness float32, color uint32 /* = white*/, speed float32 /* = 2.8f*/, dots Size_t /* = 6*/, mode int /* = 0*/) {
	_ = `ImSpinner::SpinnerSomeScaleDots(label, radius, thickness, color, speed, dots, mode)`
}
func SpinnerAngTriple(label string, radius1 float32, radius2 float32, radius3 float32, thickness float32) {
	_ = `ImSpinner::SpinnerAngTriple(label, radius1, radius2, radius3, thickness)`
}
func SpinnerAngTripleV(label string, radius1 float32, radius2 float32, radius3 float32, thickness float32, c1 uint32 /* = white*/, c2 uint32 /* = half_white*/, c3 uint32 /* = white*/, speed float32 /* = 2.8f*/, angle float32 /* = IM_PI*/) {
	_ = `ImSpinner::SpinnerAngTriple(label, radius1, radius2, radius3, thickness, c1, c2, c3, speed, angle)`
}
func SpinnerAngEclipse(label string, radius float32, thickness float32) {
	_ = `ImSpinner::SpinnerAngEclipse(label, radius, thickness)`
}
func SpinnerAngEclipseV(label string, radius float32, thickness float32, color uint32 /* = white*/, speed float32 /* = 2.8f*/, angle float32 /* = IM_PI*/) {
	_ = `ImSpinner::SpinnerAngEclipse(label, radius, thickness, color, speed, angle)`
}
func SpinnerIngYang(label string, radius float32, thickness float32, reverse bool, yang_detlta_r float32) {
	_ = `ImSpinner::SpinnerIngYang(label, radius, thickness, reverse, yang_detlta_r)`
}
func SpinnerIngYangV(label string, radius float32, thickness float32, reverse bool, yang_detlta_r float32, colorI uint32 /* = white*/, colorY uint32 /* = white*/, speed float32 /* = 2.8f*/, angle float32 /* = IM_PI *0.7f*/) {
	_ = `ImSpinner::SpinnerIngYang(label, radius, thickness, reverse, yang_detlta_r, colorI, colorY, speed, angle)`
}
func SpinnerGooeyBalls(label string, radius float32, color uint32, speed float32) {
	_ = `ImSpinner::SpinnerGooeyBalls(label, radius, color, speed)`
}
func SpinnerGooeyBallsV(label string, radius float32, color uint32, speed float32, mode int /* = 0*/) {
	_ = `ImSpinner::SpinnerGooeyBalls(label, radius, color, speed, mode)`
}
func SpinnerDotsLoading(label string, radius float32, thickness float32, color uint32, bg uint32, speed float32) {
	_ = `ImSpinner::SpinnerDotsLoading(label, radius, thickness, color, bg, speed)`
}
func SpinnerRotateGooeyBalls(label string, radius float32, thickness float32, color uint32, speed float32, balls int) {
	_ = `ImSpinner::SpinnerRotateGooeyBalls(label, radius, thickness, color, speed, balls)`
}
func SpinnerHerbertBalls(label string, radius float32, thickness float32, color uint32, speed float32, balls int) {
	_ = `ImSpinner::SpinnerHerbertBalls(label, radius, thickness, color, speed, balls)`
}
func SpinnerHerbertBalls3D(label string, radius float32, thickness float32, color uint32, speed float32) {
	_ = `ImSpinner::SpinnerHerbertBalls3D(label, radius, thickness, color, speed)`
}
func SpinnerRotateTriangles(label string, radius float32, thickness float32, color uint32, speed float32, tris int) {
	_ = `ImSpinner::SpinnerRotateTriangles(label, radius, thickness, color, speed, tris)`
}
func SpinnerRotateShapes(label string, radius float32, thickness float32, color uint32, speed float32, shapes int, pnt int) {
	_ = `ImSpinner::SpinnerRotateShapes(label, radius, thickness, color, speed, shapes, pnt)`
}
func SpinnerSinSquares(label string, radius float32, thickness float32, color uint32, speed float32) {
	_ = `ImSpinner::SpinnerSinSquares(label, radius, thickness, color, speed)`
}
func SpinnerMoonLine(label string, radius float32, thickness float32) {
	_ = `ImSpinner::SpinnerMoonLine(label, radius, thickness)`
}
func SpinnerMoonLineV(label string, radius float32, thickness float32, color uint32 /* = white*/, bg uint32 /* = red*/, speed float32 /* = 2.8f*/, angle float32 /* = IM_PI*/) {
	_ = `ImSpinner::SpinnerMoonLine(label, radius, thickness, color, bg, speed, angle)`
}
func SpinnerCircleDrop(label string, radius float32, thickness float32, thickness_drop float32) {
	_ = `ImSpinner::SpinnerCircleDrop(label, radius, thickness, thickness_drop)`
}
func SpinnerCircleDropV(label string, radius float32, thickness float32, thickness_drop float32, color uint32 /* = white*/, bg uint32 /* = half_white*/, speed float32 /* = 2.8f*/, angle float32 /* = IM_PI*/) {
	_ = `ImSpinner::SpinnerCircleDrop(label, radius, thickness, thickness_drop, color, bg, speed, angle)`
}
func SpinnerSurroundedIndicator(label string, radius float32, thickness float32) {
	_ = `ImSpinner::SpinnerSurroundedIndicator(label, radius, thickness)`
}
func SpinnerSurroundedIndicatorV(label string, radius float32, thickness float32, color uint32 /* = white*/, bg uint32 /* = half_white*/, speed float32 /* = 2.8f*/) {
	_ = `ImSpinner::SpinnerSurroundedIndicator(label, radius, thickness, color, bg, speed)`
}
func SpinnerWifiIndicator(label string, radius float32, thickness float32) {
	_ = `ImSpinner::SpinnerWifiIndicator(label, radius, thickness)`
}
func SpinnerWifiIndicatorV(label string, radius float32, thickness float32, color uint32 /* = red*/, bg uint32 /* = half_white*/, speed float32 /* = 2.8f*/, cangle float32 /* = 0.f*/, dots int /* = 3*/) {
	_ = `ImSpinner::SpinnerWifiIndicator(label, radius, thickness, color, bg, speed, cangle, dots)`
}
func SpinnerTrianglesSelector(label string, radius float32, thickness float32) {
	_ = `ImSpinner::SpinnerTrianglesSelector(label, radius, thickness)`
}
func SpinnerTrianglesSelectorV(label string, radius float32, thickness float32, color uint32 /* = white*/, bg uint32 /* = half_white*/, speed float32 /* = 2.8f*/, bars Size_t /* = 8*/) {
	_ = `ImSpinner::SpinnerTrianglesSelector(label, radius, thickness, color, bg, speed, bars)`
}
func SpinnerFlowingGradient(label string, radius float32, thickness float32) {
	_ = `ImSpinner::SpinnerFlowingGradient(label, radius, thickness)`
}
func SpinnerFlowingGradientV(label string, radius float32, thickness float32, color uint32 /* = white*/, bg uint32 /* = red*/, speed float32 /* = 2.8f*/, angle float32 /* = IM_PI*/) {
	_ = `ImSpinner::SpinnerFlowingGradient(label, radius, thickness, color, bg, speed, angle)`
}
func SpinnerRotateSegments(label string, radius float32, thickness float32) {
	_ = `ImSpinner::SpinnerRotateSegments(label, radius, thickness)`
}
func SpinnerRotateSegmentsV(label string, radius float32, thickness float32, color uint32 /* = white*/, speed float32 /* = 2.8f*/, arcs Size_t /* = 4*/, layers Size_t /* = 1*/) {
	_ = `ImSpinner::SpinnerRotateSegments(label, radius, thickness, color, speed, arcs, layers)`
}
func SpinnerLemniscate(label string, radius float32, thickness float32) {
	_ = `ImSpinner::SpinnerLemniscate(label, radius, thickness)`
}
func SpinnerLemniscateV(label string, radius float32, thickness float32, color uint32 /* = white*/, speed float32 /* = 2.8f*/, angle float32 /* = IM_PI/2.0f*/) {
	_ = `ImSpinner::SpinnerLemniscate(label, radius, thickness, color, speed, angle)`
}
func SpinnerRotateGear(label string, radius float32, thickness float32) {
	_ = `ImSpinner::SpinnerRotateGear(label, radius, thickness)`
}
func SpinnerRotateGearV(label string, radius float32, thickness float32, color uint32 /* = white*/, speed float32 /* = 2.8f*/, pins Size_t /* = 12*/) {
	_ = `ImSpinner::SpinnerRotateGear(label, radius, thickness, color, speed, pins)`
}
func SpinnerRotateWheel(label string, radius float32, thickness float32) {
	_ = `ImSpinner::SpinnerRotateWheel(label, radius, thickness)`
}
func SpinnerRotateWheelV(label string, radius float32, thickness float32, bg_color uint32 /* = white*/, color uint32 /* = white*/, speed float32 /* = 2.8f*/, pins Size_t /* = 12*/) {
	_ = `ImSpinner::SpinnerRotateWheel(label, radius, thickness, bg_color, color, speed, pins)`
}
func SpinnerAtom(label string, radius float32, thickness float32) {
	_ = `ImSpinner::SpinnerAtom(label, radius, thickness)`
}
func SpinnerAtomV(label string, radius float32, thickness float32, color uint32 /* = white*/, speed float32 /* = 2.8f*/, elipses int /* = 3*/) {
	_ = `ImSpinner::SpinnerAtom(label, radius, thickness, color, speed, elipses)`
}
func SpinnerPatternRings(label string, radius float32, thickness float32) {
	_ = `ImSpinner::SpinnerPatternRings(label, radius, thickness)`
}
func SpinnerPatternRingsV(label string, radius float32, thickness float32, color uint32 /* = white*/, speed float32 /* = 2.8f*/, elipses int /* = 3*/) {
	_ = `ImSpinner::SpinnerPatternRings(label, radius, thickness, color, speed, elipses)`
}
func SpinnerPatternEclipse(label string, radius float32, thickness float32) {
	_ = `ImSpinner::SpinnerPatternEclipse(label, radius, thickness)`
}
func SpinnerPatternEclipseV(label string, radius float32, thickness float32, color uint32 /* = white*/, speed float32 /* = 2.8f*/, elipses int /* = 3*/, delta_a float32 /* = 2.f*/, delta_y float32 /* = 0.f*/) {
	_ = `ImSpinner::SpinnerPatternEclipse(label, radius, thickness, color, speed, elipses, delta_a, delta_y)`
}
func SpinnerPatternSphere(label string, radius float32, thickness float32) {
	_ = `ImSpinner::SpinnerPatternSphere(label, radius, thickness)`
}
func SpinnerPatternSphereV(label string, radius float32, thickness float32, color uint32 /* = white*/, speed float32 /* = 2.8f*/, elipses int /* = 3*/) {
	_ = `ImSpinner::SpinnerPatternSphere(label, radius, thickness, color, speed, elipses)`
}
func SpinnerRingSynchronous(label string, radius float32, thickness float32) {
	_ = `ImSpinner::SpinnerRingSynchronous(label, radius, thickness)`
}
func SpinnerRingSynchronousV(label string, radius float32, thickness float32, color uint32 /* = white*/, speed float32 /* = 2.8f*/, elipses int /* = 3*/) {
	_ = `ImSpinner::SpinnerRingSynchronous(label, radius, thickness, color, speed, elipses)`
}
func SpinnerRingWatermarks(label string, radius float32, thickness float32) {
	_ = `ImSpinner::SpinnerRingWatermarks(label, radius, thickness)`
}
func SpinnerRingWatermarksV(label string, radius float32, thickness float32, color uint32 /* = white*/, speed float32 /* = 2.8f*/, elipses int /* = 3*/) {
	_ = `ImSpinner::SpinnerRingWatermarks(label, radius, thickness, color, speed, elipses)`
}
func SpinnerRotatedAtom(label string, radius float32, thickness float32) {
	_ = `ImSpinner::SpinnerRotatedAtom(label, radius, thickness)`
}
func SpinnerRotatedAtomV(label string, radius float32, thickness float32, color uint32 /* = white*/, speed float32 /* = 2.8f*/, elipses int /* = 3*/) {
	_ = `ImSpinner::SpinnerRotatedAtom(label, radius, thickness, color, speed, elipses)`
}
func SpinnerRainbowBalls(label string, radius float32, thickness float32, color uint32, speed float32) {
	_ = `ImSpinner::SpinnerRainbowBalls(label, radius, thickness, color, speed)`
}
func SpinnerRainbowBallsV(label string, radius float32, thickness float32, color uint32, speed float32, balls int /* = 5*/) {
	_ = `ImSpinner::SpinnerRainbowBalls(label, radius, thickness, color, speed, balls)`
}
func SpinnerRainbowShot(label string, radius float32, thickness float32, color uint32, speed float32) {
	_ = `ImSpinner::SpinnerRainbowShot(label, radius, thickness, color, speed)`
}
func SpinnerRainbowShotV(label string, radius float32, thickness float32, color uint32, speed float32, balls int /* = 5*/) {
	_ = `ImSpinner::SpinnerRainbowShot(label, radius, thickness, color, speed, balls)`
}
func SpinnerSpiral(label string, radius float32, thickness float32) {
	_ = `ImSpinner::SpinnerSpiral(label, radius, thickness)`
}
func SpinnerSpiralV(label string, radius float32, thickness float32, color uint32 /* = white*/, speed float32 /* = 2.8f*/, arcs Size_t /* = 4*/) {
	_ = `ImSpinner::SpinnerSpiral(label, radius, thickness, color, speed, arcs)`
}
func SpinnerSpiralEye(label string, radius float32, thickness float32) {
	_ = `ImSpinner::SpinnerSpiralEye(label, radius, thickness)`
}
func SpinnerSpiralEyeV(label string, radius float32, thickness float32, color uint32 /* = white*/, speed float32 /* = 2.8f*/) {
	_ = `ImSpinner::SpinnerSpiralEye(label, radius, thickness, color, speed)`
}
func SpinnerBarChartSine(label string, radius float32, thickness float32, color uint32, speed float32) {
	_ = `ImSpinner::SpinnerBarChartSine(label, radius, thickness, color, speed)`
}
func SpinnerBarChartSineV(label string, radius float32, thickness float32, color uint32, speed float32, bars int /* = 5*/, mode int /* = 0*/) {
	_ = `ImSpinner::SpinnerBarChartSine(label, radius, thickness, color, speed, bars, mode)`
}
func SpinnerBarChartAdvSine(label string, radius float32, thickness float32, color uint32, speed float32) {
	_ = `ImSpinner::SpinnerBarChartAdvSine(label, radius, thickness, color, speed)`
}
func SpinnerBarChartAdvSineV(label string, radius float32, thickness float32, color uint32, speed float32, mode int /* = 0*/) {
	_ = `ImSpinner::SpinnerBarChartAdvSine(label, radius, thickness, color, speed, mode)`
}
func SpinnerBarChartAdvSineFade(label string, radius float32, thickness float32, color uint32, speed float32) {
	_ = `ImSpinner::SpinnerBarChartAdvSineFade(label, radius, thickness, color, speed)`
}
func SpinnerBarChartAdvSineFadeV(label string, radius float32, thickness float32, color uint32, speed float32, mode int /* = 0*/) {
	_ = `ImSpinner::SpinnerBarChartAdvSineFade(label, radius, thickness, color, speed, mode)`
}
func SpinnerBarChartRainbow(label string, radius float32, thickness float32, color uint32, speed float32) {
	_ = `ImSpinner::SpinnerBarChartRainbow(label, radius, thickness, color, speed)`
}
func SpinnerBarChartRainbowV(label string, radius float32, thickness float32, color uint32, speed float32, bars int /* = 5*/) {
	_ = `ImSpinner::SpinnerBarChartRainbow(label, radius, thickness, color, speed, bars)`
}
func SpinnerBlocks(label string, radius float32, thickness float32, bg uint32, color uint32, speed float32) {
	_ = `ImSpinner::SpinnerBlocks(label, radius, thickness, bg, color, speed)`
}
func SpinnerTwinBlocks(label string, radius float32, thickness float32, bg uint32, color uint32, speed float32) {
	_ = `ImSpinner::SpinnerTwinBlocks(label, radius, thickness, bg, color, speed)`
}
func SpinnerSquareRandomDots(label string, radius float32, thickness float32, bg uint32, color uint32, speed float32) {
	_ = `ImSpinner::SpinnerSquareRandomDots(label, radius, thickness, bg, color, speed)`
}
func SpinnerScaleBlocks(label string, radius float32, thickness float32, color uint32, speed float32) {
	_ = `ImSpinner::SpinnerScaleBlocks(label, radius, thickness, color, speed)`
}
func SpinnerScaleBlocksV(label string, radius float32, thickness float32, color uint32, speed float32, mode int /* = 0*/) {
	_ = `ImSpinner::SpinnerScaleBlocks(label, radius, thickness, color, speed, mode)`
}
func SpinnerScaleSquares(label string, radius float32, thikness float32, color uint32, speed float32) {
	_ = `ImSpinner::SpinnerScaleSquares(label, radius, thikness, color, speed)`
}
func SpinnerSquishSquare(label string, radius float32, color uint32, speed float32) {
	_ = `ImSpinner::SpinnerSquishSquare(label, radius, color, speed)`
}
func SpinnerFluid(label string, radius float32, color uint32, speed float32) {
	_ = `ImSpinner::SpinnerFluid(label, radius, color, speed)`
}
func SpinnerFluidV(label string, radius float32, color uint32, speed float32, bars int /* = 3*/) {
	_ = `ImSpinner::SpinnerFluid(label, radius, color, speed, bars)`
}
func SpinnerFluidPoints(label string, radius float32, thickness float32, color uint32, speed float32) {
	_ = `ImSpinner::SpinnerFluidPoints(label, radius, thickness, color, speed)`
}
func SpinnerFluidPointsV(label string, radius float32, thickness float32, color uint32, speed float32, dots Size_t /* = 6*/, delta float32 /* = 0.35f*/) {
	_ = `ImSpinner::SpinnerFluidPoints(label, radius, thickness, color, speed, dots, delta)`
}
func SpinnerArcPolarFade(label string, radius float32) {
	_ = `ImSpinner::SpinnerArcPolarFade(label, radius)`
}
func SpinnerArcPolarFadeV(label string, radius float32, color uint32 /* = white*/, speed float32 /* = 2.8f*/, arcs Size_t /* = 4*/) {
	_ = `ImSpinner::SpinnerArcPolarFade(label, radius, color, speed, arcs)`
}
func SpinnerArcPolarRadius(label string, radius float32) {
	_ = `ImSpinner::SpinnerArcPolarRadius(label, radius)`
}
func SpinnerArcPolarRadiusV(label string, radius float32, color uint32 /* = white*/, speed float32 /* = 2.8f*/, arcs Size_t /* = 4*/) {
	_ = `ImSpinner::SpinnerArcPolarRadius(label, radius, color, speed, arcs)`
}
func SpinnerCaleidoscope(label string, radius float32, thickness float32) {
	_ = `ImSpinner::SpinnerCaleidoscope(label, radius, thickness)`
}
func SpinnerCaleidoscopeV(label string, radius float32, thickness float32, color uint32 /* = white*/, speed float32 /* = 2.8f*/, arcs Size_t /* = 6*/, mode int /* = 0*/) {
	_ = `ImSpinner::SpinnerCaleidoscope(label, radius, thickness, color, speed, arcs, mode)`
}
func SpinnerHboDots(label string, radius float32, thickness float32) {
	_ = `ImSpinner::SpinnerHboDots(label, radius, thickness)`
}
func SpinnerHboDotsV(label string, radius float32, thickness float32, color uint32 /* = white*/, minfade float32 /* = 0.0f*/, ryk float32 /* = 0.f*/, speed float32 /* = 1.1f*/, dots Size_t /* = 6*/) {
	_ = `ImSpinner::SpinnerHboDots(label, radius, thickness, color, minfade, ryk, speed, dots)`
}
func SpinnerMoonDots(label string, radius float32, thickness float32, first uint32, second uint32) {
	_ = `ImSpinner::SpinnerMoonDots(label, radius, thickness, first, second)`
}
func SpinnerMoonDotsV(label string, radius float32, thickness float32, first uint32, second uint32, speed float32 /* = 1.1f*/) {
	_ = `ImSpinner::SpinnerMoonDots(label, radius, thickness, first, second, speed)`
}
func SpinnerTwinHboDots(label string, radius float32, thickness float32) {
	_ = `ImSpinner::SpinnerTwinHboDots(label, radius, thickness)`
}
func SpinnerTwinHboDotsV(label string, radius float32, thickness float32, color uint32 /* = white*/, minfade float32 /* = 0.0f*/, ryk float32 /* = 0.f*/, speed float32 /* = 1.1f*/, dots Size_t /* = 6*/, delta float32 /* = 0.f*/) {
	_ = `ImSpinner::SpinnerTwinHboDots(label, radius, thickness, color, minfade, ryk, speed, dots, delta)`
}
func SpinnerThreeDotsStar(label string, radius float32, thickness float32) {
	_ = `ImSpinner::SpinnerThreeDotsStar(label, radius, thickness)`
}
func SpinnerThreeDotsStarV(label string, radius float32, thickness float32, color uint32 /* = white*/, minfade float32 /* = 0.0f*/, ryk float32 /* = 0.f*/, speed float32 /* = 1.1f*/, delta float32 /* = 0.f*/) {
	_ = `ImSpinner::SpinnerThreeDotsStar(label, radius, thickness, color, minfade, ryk, speed, delta)`
}
func SpinnerSineArcs(label string, radius float32, thickness float32) {
	_ = `ImSpinner::SpinnerSineArcs(label, radius, thickness)`
}
func SpinnerSineArcsV(label string, radius float32, thickness float32, color uint32 /* = white*/, speed float32 /* = 2.8f*/) {
	_ = `ImSpinner::SpinnerSineArcs(label, radius, thickness, color, speed)`
}
func SpinnerTrianglesShift(label string, radius float32, thickness float32) {
	_ = `ImSpinner::SpinnerTrianglesShift(label, radius, thickness)`
}
func SpinnerTrianglesShiftV(label string, radius float32, thickness float32, color uint32 /* = white*/, bg uint32 /* = half_white*/, speed float32 /* = 2.8f*/, bars Size_t /* = 8*/) {
	_ = `ImSpinner::SpinnerTrianglesShift(label, radius, thickness, color, bg, speed, bars)`
}
func SpinnerPointsShift(label string, radius float32, thickness float32) {
	_ = `ImSpinner::SpinnerPointsShift(label, radius, thickness)`
}
func SpinnerPointsShiftV(label string, radius float32, thickness float32, color uint32 /* = white*/, bg uint32 /* = half_white*/, speed float32 /* = 2.8f*/, bars Size_t /* = 8*/) {
	_ = `ImSpinner::SpinnerPointsShift(label, radius, thickness, color, bg, speed, bars)`
}
func SpinnerSwingDots(label string, radius float32, thickness float32) {
	_ = `ImSpinner::SpinnerSwingDots(label, radius, thickness)`
}
func SpinnerSwingDotsV(label string, radius float32, thickness float32, color uint32 /* = white*/, speed float32 /* = 2.8f*/) {
	_ = `ImSpinner::SpinnerSwingDots(label, radius, thickness, color, speed)`
}
func SpinnerCircularPoints(label string, radius float32, thickness float32) {
	_ = `ImSpinner::SpinnerCircularPoints(label, radius, thickness)`
}
func SpinnerCircularPointsV(label string, radius float32, thickness float32, color uint32 /* = white*/, speed float32 /* = 1.8f*/, lines int /* = 8*/) {
	_ = `ImSpinner::SpinnerCircularPoints(label, radius, thickness, color, speed, lines)`
}
func SpinnerCurvedCircle(label string, radius float32, thickness float32) {
	_ = `ImSpinner::SpinnerCurvedCircle(label, radius, thickness)`
}
func SpinnerCurvedCircleV(label string, radius float32, thickness float32, color uint32 /* = white*/, speed float32 /* = 2.8f*/, circles Size_t /* = 1*/) {
	_ = `ImSpinner::SpinnerCurvedCircle(label, radius, thickness, color, speed, circles)`
}
func SpinnerModCircle(label string, radius float32, thickness float32) {
	_ = `ImSpinner::SpinnerModCircle(label, radius, thickness)`
}
func SpinnerModCircleV(label string, radius float32, thickness float32, color uint32 /* = white*/, ang_min float32 /* = 1.f*/, ang_max float32 /* = 1.f*/, speed float32 /* = 2.8f*/) {
	_ = `ImSpinner::SpinnerModCircle(label, radius, thickness, color, ang_min, ang_max, speed)`
}
func SpinnerDnaDots(label string, radius float32, thickness float32) {
	_ = `ImSpinner::SpinnerDnaDots(label, radius, thickness)`
}
func SpinnerDnaDotsV(label string, radius float32, thickness float32, color uint32 /* = white*/, speed float32 /* = 2.8f*/, lt int /* = 8*/, delta float32 /* = 0.5f*/, mode bool /* = 0*/) {
	_ = `ImSpinner::SpinnerDnaDots(label, radius, thickness, color, speed, lt, delta, mode)`
}
func Spinner3SmuggleDots(label string, radius float32, thickness float32) {
	_ = `ImSpinner::Spinner3SmuggleDots(label, radius, thickness)`
}
func Spinner3SmuggleDotsV(label string, radius float32, thickness float32, color uint32 /* = white*/, speed float32 /* = 4.8f*/, lt int /* = 8*/, delta float32 /* = 0.5f*/, mode bool /* = 0*/) {
	_ = `ImSpinner::Spinner3SmuggleDots(label, radius, thickness, color, speed, lt, delta, mode)`
}
func SpinnerRotateSegmentsPulsar(label string, radius float32, thickness float32) {
	_ = `ImSpinner::SpinnerRotateSegmentsPulsar(label, radius, thickness)`
}
func SpinnerRotateSegmentsPulsarV(label string, radius float32, thickness float32, color uint32 /* = white*/, speed float32 /* = 2.8f*/, arcs Size_t /* = 4*/, layers Size_t /* = 1*/) {
	_ = `ImSpinner::SpinnerRotateSegmentsPulsar(label, radius, thickness, color, speed, arcs, layers)`
}
