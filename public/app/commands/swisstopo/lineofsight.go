package swisstopo

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/rs/zerolog/log"
	"github.com/stergiotis/boxer/public/config/env"
	"github.com/stergiotis/boxer/public/observability/eh"
	swisstopoLib "github.com/stergiotis/boxer/public/science/geo/swisstopo"
	cli "github.com/urfave/cli/v2"
)

func newLineOfSightCommand() (cmd *cli.Command) {
	cmd = &cli.Command{
		Name:  "line-of-sight",
		Usage: "compute line-of-sight elevation analysis between two WGS84 points",
		Flags: []cli.Flag{
			&cli.Float64Flag{
				Name:     "from-lat",
				Usage:    "latitude of the observer (WGS84 decimal degrees)",
				Required: true,
			},
			&cli.Float64Flag{
				Name:     "from-lon",
				Usage:    "longitude of the observer (WGS84 decimal degrees)",
				Required: true,
			},
			&cli.Float64Flag{
				Name:     "to-lat",
				Usage:    "latitude of the target (WGS84 decimal degrees)",
				Required: true,
			},
			&cli.Float64Flag{
				Name:     "to-lon",
				Usage:    "longitude of the target (WGS84 decimal degrees)",
				Required: true,
			},
			&cli.Float64Flag{
				Name:  "from-height",
				Value: 1.7,
				Usage: "observer height above terrain in meters (eye height)",
			},
			&cli.Float64Flag{
				Name:  "to-height",
				Value: 0.0,
				Usage: "target height above terrain in meters",
			},
			&cli.StringFlag{
				Name:  "tiles-dir",
				Value: filepath.Join(env.Home.Get(), "data", "swisstopo"),
				Usage: "directory containing swissALTI3D 2m COG tiles",
			},
			&cli.Float64Flag{
				Name:  "step",
				Value: 2.0,
				Usage: "sampling interval in meters",
			},
			&cli.StringFlag{
				Name:  "csv",
				Usage: "write profile CSV to this file (default: stdout)",
			},
		},
		Action: lineOfSightAction,
	}
	return
}

func lineOfSightAction(c *cli.Context) (err error) {
	fromWGS := swisstopoLib.WGS84Coord{
		Lat: c.Float64("from-lat"),
		Lon: c.Float64("from-lon"),
	}
	toWGS := swisstopoLib.WGS84Coord{
		Lat: c.Float64("to-lat"),
		Lon: c.Float64("to-lon"),
	}
	fromHeight := c.Float64("from-height")
	toHeight := c.Float64("to-height")
	tilesDir := c.String("tiles-dir")

	fromLV := swisstopoLib.WGS84ToLV95(fromWGS)
	toLV := swisstopoLib.WGS84ToLV95(toWGS)

	log.Info().
		Str("from_wgs", fromWGS.String()).
		Str("from_lv95", fromLV.String()).
		Str("to_wgs", toWGS.String()).
		Str("to_lv95", toLV.String()).
		Float64("from_height", fromHeight).
		Float64("to_height", toHeight).
		Msg("line-of-sight analysis")

	var sampler *swisstopoLib.ElevationSampler
	sampler, err = swisstopoLib.NewElevationSampler(c.Context, tilesDir)
	if err != nil {
		err = eh.Errorf("unable to create elevation sampler: %w", err)
		return
	}

	var result swisstopoLib.LOSResult
	result, err = sampler.LineOfSight(fromLV, fromHeight, toLV, toHeight)
	if err != nil {
		err = eh.Errorf("line-of-sight computation failed: %w", err)
		return
	}

	// print summary
	{ // summary output
		fmt.Println("=== Line-of-Sight Analysis ===")
		fmt.Printf("From:           %s  (%s)\n", fromWGS, fromLV)
		fmt.Printf("To:             %s  (%s)\n", toWGS, toLV)
		fmt.Printf("From terrain:   %.1f m\n", result.FromElev)
		fmt.Printf("To terrain:     %.1f m\n", result.ToElev)
		fmt.Printf("From height:    %.1f m above terrain\n", fromHeight)
		fmt.Printf("To height:      %.1f m above terrain\n", toHeight)
		if len(result.ProfileDist) > 0 {
			fmt.Printf("Distance:       %.1f m\n", result.ProfileDist[len(result.ProfileDist)-1])
		}
		fmt.Printf("Profile points: %d\n", len(result.ProfileDist))

		if result.Visible {
			fmt.Println("Result:         VISIBLE")
		} else {
			obstructWGS := swisstopoLib.LV95ToWGS84(result.ObstructionCoord)
			fmt.Println("Result:         OBSTRUCTED")
			fmt.Printf("Obstruction at: %.1f m from observer\n", result.ObstructionDist)
			fmt.Printf("Obstruction el: %.1f m\n", result.ObstructionElev)
			fmt.Printf("Obstruction at: %s  (%s)\n", obstructWGS, result.ObstructionCoord)
		}
	}

	// write CSV
	{ // CSV output
		var w *os.File
		csvPath := c.String("csv")
		if csvPath != "" {
			w, err = os.Create(csvPath)
			if err != nil {
				err = eh.Errorf("unable to create CSV file %s: %w", csvPath, err)
				return
			}
			defer func() {
				closeErr := w.Close()
				if closeErr != nil && err == nil {
					err = eh.Errorf("unable to close CSV file: %w", closeErr)
				}
			}()
		} else {
			w = os.Stdout
		}

		_, err = fmt.Fprintln(w, "distance_m,terrain_elev_m,los_elev_m")
		if err != nil {
			err = eh.Errorf("unable to write CSV header: %w", err)
			return
		}
		for i := 0; i < len(result.ProfileDist); i++ {
			_, err = fmt.Fprintf(w, "%.1f,%.2f,%.2f\n", result.ProfileDist[i], result.ProfileElev[i], result.LOSElev[i])
			if err != nil {
				err = eh.Errorf("unable to write CSV row: %w", err)
				return
			}
		}

		if csvPath != "" {
			log.Info().Str("path", csvPath).Int("rows", len(result.ProfileDist)).Msg("wrote profile CSV")
		}
	}

	return
}
