//go:build llm_generated_opus46

package swisstopo

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/rs/zerolog/log"
	"github.com/stergiotis/boxer/public/config/env"
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/hmi/progressbar"
	cli "github.com/urfave/cli/v2"
)

const stacItemsURL = "https://data.geo.admin.ch/api/stac/v0.9/collections/ch.swisstopo.swissalti3d/items"
const stacPageLimit = 100
const maxRetries = 3
const retryBaseDelay = 2 * time.Second

type tileDesc struct {
	filename    string
	href        string
	sha256Hex   string
	contentType string
}

type manifestEntry struct {
	Sha256 string `json:"sha256"`
	Bytes  int64  `json:"bytes"`
}

type manifest struct {
	Tiles       map[string]manifestEntry `json:"tiles"`
	Errors      []string                 `json:"errors"`
	TotalBytes  int64                    `json:"totalBytes"`
	LastUpdated string                   `json:"lastUpdated"`
}

func newMirrorCommand() (cmd *cli.Command) {
	cmd = &cli.Command{
		Name:  "mirror",
		Usage: "resumeably mirror swissALTI3D 2m COG tiles to local directory",
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:  "dest",
				Value: filepath.Join(env.Home.Get(), "data", "swisstopo"),
				Usage: "destination directory",
			},
			&cli.IntFlag{
				Name:  "workers",
				Value: 4,
				Usage: "number of concurrent downloads",
			},
			&cli.BoolFlag{
				Name:  "dry-run",
				Usage: "enumerate tiles without downloading",
			},
			&cli.BoolFlag{
				Name:  "verify-existing",
				Usage: "re-check sha256 of existing files (slow)",
			},
		},
		Action: mirrorAction,
	}
	return
}

func mirrorAction(c *cli.Context) (err error) {
	dest := c.String("dest")
	workers := c.Int("workers")
	dryRun := c.Bool("dry-run")
	verifyExisting := c.Bool("verify-existing")

	err = os.MkdirAll(dest, 0o755)
	if err != nil {
		err = eh.Errorf("unable to create destination directory: %w", err)
		return
	}
	log.Info().Str("dest", dest).Int("workers", workers).Msg("mirror starting")

	ctx, cancel := context.WithCancel(c.Context)
	defer cancel()

	// phase 1: enumerate
	var tiles []tileDesc
	{
		enumPb := progressbar.New(0, "tiles found")
		enumPb.Start(ctx)
		restoreLog := routeLogsThrough(enumPb.LogWriter())
		tiles, err = enumerateTiles(ctx, enumPb)
		restoreLog()
		enumPb.Stop()
	}
	if err != nil {
		err = eh.Errorf("unable to enumerate STAC tiles: %w", err)
		return
	}
	log.Info().Int("count", len(tiles)).Msg("found 2m COG tiles")

	if dryRun {
		scanPb := progressbar.New(int64(len(tiles)), "tiles scanned")
		scanPb.Start(ctx)
		restoreLog := routeLogsThrough(scanPb.LogWriter())
		var existing int64
		for _, t := range tiles {
			p := filepath.Join(dest, t.filename)
			_, statErr := os.Stat(p)
			if statErr == nil {
				existing++
			}
			scanPb.Tick()
		}
		restoreLog()
		scanPb.Stop()
		fmt.Printf("total tiles:    %d\n", len(tiles))
		fmt.Printf("already local:  %d\n", existing)
		fmt.Printf("to download:    %d\n", int64(len(tiles))-existing)
		return
	}

	// phase 2: download
	var mf manifest
	mf, err = loadManifest(dest)
	if err != nil {
		err = eh.Errorf("unable to load manifest: %w", err)
		return
	}

	var stats downloadStats
	t0 := time.Now()

	mp := newMirrorProgress(int64(len(tiles)), "tiles")
	mp.Start(ctx)
	restoreLog := routeLogsThrough(mp.LogWriter())

	tileCh := make(chan tileDesc, workers*2)
	var wg sync.WaitGroup

	{ // spawn workers
		for i := 0; i < workers; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				downloadWorker(ctx, dest, verifyExisting, tileCh, &stats, mp)
			}()
		}
	}

	{ // feed tiles
		for _, t := range tiles {
			select {
			case <-ctx.Done():
				break
			case tileCh <- t:
			}
		}
		close(tileCh)
	}

	wg.Wait()
	restoreLog()
	mp.Stop()

	{ // update manifest from stats
		for _, e := range stats.completed {
			mf.Tiles[e.filename] = manifestEntry{
				Sha256: e.sha256Hex,
				Bytes:  e.bytes,
			}
			mf.TotalBytes += e.bytes
		}
		for _, e := range stats.errors {
			mf.Errors = append(mf.Errors, e)
		}
	}

	err = saveManifest(dest, &mf)
	if err != nil {
		err = eh.Errorf("unable to save manifest: %w", err)
		return
	}

	elapsed := time.Since(t0)
	downloaded := stats.downloadedCount.Load()
	existed := stats.existedCount.Load()
	errCount := stats.errorCount.Load()
	totalBytes := stats.downloadedBytes.Load()
	log.Info().
		Dur("elapsed", elapsed).
		Int64("downloaded", downloaded).
		Int64("existed", existed).
		Int64("errors", errCount).
		Str("size", progressbar.FormatBytes(totalBytes)).
		Msg("mirror complete")

	if errCount > 0 {
		log.Warn().Int64("errors", errCount).Msg("re-run to retry failed tiles")
	}
	return
}

type completedTile struct {
	filename  string
	sha256Hex string
	bytes     int64
}

type downloadStats struct {
	downloadedCount atomic.Int64
	existedCount    atomic.Int64
	errorCount      atomic.Int64
	downloadedBytes atomic.Int64

	mu        sync.Mutex
	completed []completedTile
	errors    []string
}

func (inst *downloadStats) addCompleted(ct completedTile) {
	inst.mu.Lock()
	inst.completed = append(inst.completed, ct)
	inst.mu.Unlock()
}

func (inst *downloadStats) addError(msg string) {
	inst.mu.Lock()
	inst.errors = append(inst.errors, msg)
	inst.mu.Unlock()
}

func downloadWorker(ctx context.Context, dest string, verifyExisting bool, tiles <-chan tileDesc, stats *downloadStats, mp *mirrorProgress) {
	client := &http.Client{Timeout: 120 * time.Second}

	for t := range tiles {
		select {
		case <-ctx.Done():
			return
		default:
		}

		destPath := filepath.Join(dest, t.filename)

		{ // check existing
			_, statErr := os.Stat(destPath)
			if statErr == nil {
				if !verifyExisting {
					stats.existedCount.Add(1)
					mp.existed.Add(1)
					mp.Tick()
					continue
				}
				actual, hashErr := sha256File(destPath)
				if hashErr == nil && t.sha256Hex != "" && actual == t.sha256Hex {
					stats.existedCount.Add(1)
					mp.existed.Add(1)
					mp.Tick()
					continue
				}
			}
		}

		{ // download with retries
			var dlErr error
			var nbytes int64
			for attempt := 0; attempt < maxRetries; attempt++ {
				select {
				case <-ctx.Done():
					return
				default:
				}

				nbytes, dlErr = downloadFile(ctx, client, t.href, destPath, t.sha256Hex)
				if dlErr == nil {
					break
				}
				wait := retryBaseDelay * time.Duration(1<<uint(attempt))
				log.Warn().Err(dlErr).Str("file", t.filename).Int("attempt", attempt+1).Dur("retryIn", wait).Msg("download failed")
				select {
				case <-ctx.Done():
					return
				case <-time.After(wait):
				}
			}

			if dlErr != nil {
				stats.errorCount.Add(1)
				mp.errors.Add(1)
				stats.addError(fmt.Sprintf("%s: %v", t.filename, dlErr))
			} else {
				stats.downloadedCount.Add(1)
				stats.downloadedBytes.Add(nbytes)
				mp.downloaded.Add(1)
				mp.bytesReceived.Add(nbytes)
				stats.addCompleted(completedTile{
					filename:  t.filename,
					sha256Hex: t.sha256Hex,
					bytes:     nbytes,
				})
			}
		}

		mp.Tick()
	}
}

func downloadFile(ctx context.Context, client *http.Client, href string, destPath string, expectedSha256 string) (nbytes int64, err error) {
	tmpPath := destPath + ".part"

	var req *http.Request
	req, err = http.NewRequestWithContext(ctx, http.MethodGet, href, nil)
	if err != nil {
		err = eh.Errorf("unable to create request: %w", err)
		return
	}

	var resp *http.Response
	resp, err = client.Do(req)
	if err != nil {
		err = eh.Errorf("unable to download: %w", err)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		err = eh.Errorf("unexpected status %d for %s: %w", resp.StatusCode, href, err)
		return
	}

	var f *os.File
	f, err = os.Create(tmpPath)
	if err != nil {
		err = eh.Errorf("unable to create temp file: %w", err)
		return
	}
	defer func() {
		_ = f.Close()
		if err != nil {
			_ = os.Remove(tmpPath)
		}
	}()

	h := sha256.New()
	w := io.MultiWriter(f, h)

	nbytes, err = io.Copy(w, resp.Body)
	if err != nil {
		err = eh.Errorf("unable to write file: %w", err)
		return
	}

	err = f.Close()
	if err != nil {
		err = eh.Errorf("unable to close temp file: %w", err)
		return
	}

	{ // verify sha256
		if expectedSha256 != "" {
			actual := hex.EncodeToString(h.Sum(nil))
			if actual != expectedSha256 {
				err = eh.Errorf("sha256 mismatch: expected %s got %s: %w", expectedSha256, actual, fmt.Errorf("integrity check failed"))
				return
			}
		}
	}

	err = os.Rename(tmpPath, destPath)
	if err != nil {
		err = eh.Errorf("unable to rename temp file: %w", err)
		return
	}
	return
}

// STAC API types
type stacResponse struct {
	Features []stacFeature `json:"features"`
	Links    []stacLink    `json:"links"`
}

type stacFeature struct {
	Id     string                `json:"id"`
	Assets map[string]stacAsset `json:"assets"`
}

type stacAsset struct {
	Type              string  `json:"type"`
	Href              string  `json:"href"`
	ChecksumMultihash string  `json:"checksum:multihash"`
	Gsd               float64 `json:"eo:gsd"`
}

type stacLink struct {
	Rel  string `json:"rel"`
	Href string `json:"href"`
}

func enumerateTiles(ctx context.Context, pb *progressbar.Bar) (tiles []tileDesc, err error) {
	tiles = make([]tileDesc, 0, 50_000)
	client := &http.Client{Timeout: 30 * time.Second}

	url := fmt.Sprintf("%s?limit=%d", stacItemsURL, stacPageLimit)
	page := 0

	for url != "" {
		select {
		case <-ctx.Done():
			err = ctx.Err()
			return
		default:
		}

		var data stacResponse
		data, err = fetchStacPage(ctx, client, url)
		if err != nil {
			err = eh.Errorf("unable to fetch STAC page %d: %w", page, err)
			return
		}
		page++

		for _, feat := range data.Features {
			for key, asset := range feat.Assets {
				// filter: 2m COG only
				if asset.Gsd != 2.0 {
					continue
				}
				if !strings.Contains(asset.Type, "geotiff") {
					continue
				}

				sha256Hex := ""
				mh := asset.ChecksumMultihash
				if strings.HasPrefix(mh, "1220") && len(mh) == 68 {
					sha256Hex = strings.ToLower(mh[4:])
				}

				tiles = append(tiles, tileDesc{
					filename:    key,
					href:        asset.Href,
					sha256Hex:   sha256Hex,
					contentType: asset.Type,
				})
				pb.Tick()

			}
		}

		{ // find next page
			url = ""
			for _, link := range data.Links {
				if link.Rel == "next" {
					url = link.Href
					break
				}
			}
		}
	}
	return
}

func fetchStacPage(ctx context.Context, client *http.Client, url string) (data stacResponse, err error) {
	for attempt := 0; attempt < maxRetries; attempt++ {
		select {
		case <-ctx.Done():
			err = ctx.Err()
			return
		default:
		}

		var req *http.Request
		req, err = http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if err != nil {
			err = eh.Errorf("unable to create STAC request: %w", err)
			return
		}
		req.Header.Set("Accept", "application/json")

		var resp *http.Response
		resp, err = client.Do(req)
		if err != nil {
			if attempt < maxRetries-1 {
				wait := retryBaseDelay * time.Duration(1<<uint(attempt))
				log.Warn().Err(err).Int("attempt", attempt+1).Dur("retryIn", wait).Msg("STAC fetch failed")
				select {
				case <-ctx.Done():
					err = ctx.Err()
					return
				case <-time.After(wait):
				}
				continue
			}
			err = eh.Errorf("STAC fetch failed after retries: %w", err)
			return
		}

		var body []byte
		body, err = io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			err = eh.Errorf("unable to read STAC response: %w", err)
			return
		}

		if resp.StatusCode != http.StatusOK {
			if attempt < maxRetries-1 {
				wait := retryBaseDelay * time.Duration(1<<uint(attempt))
				log.Warn().Int("status", resp.StatusCode).Int("attempt", attempt+1).Msg("STAC non-200")
				select {
				case <-ctx.Done():
					err = ctx.Err()
					return
				case <-time.After(wait):
				}
				continue
			}
			err = eh.Errorf("STAC returned status %d: %w", resp.StatusCode, fmt.Errorf("unexpected status"))
			return
		}

		err = json.Unmarshal(body, &data)
		if err != nil {
			err = eh.Errorf("unable to parse STAC response: %w", err)
			return
		}
		return
	}
	return
}

func sha256File(path string) (hexDigest string, err error) {
	var f *os.File
	f, err = os.Open(path)
	if err != nil {
		return
	}
	defer f.Close()

	h := sha256.New()
	_, err = io.Copy(h, f)
	if err != nil {
		return
	}
	hexDigest = hex.EncodeToString(h.Sum(nil))
	return
}

func loadManifest(dest string) (mf manifest, err error) {
	mf.Tiles = make(map[string]manifestEntry, 50_000)
	p := filepath.Join(dest, "manifest.json")

	var data []byte
	data, err = os.ReadFile(p)
	if err != nil {
		if os.IsNotExist(err) {
			err = nil
		}
		return
	}

	err = json.Unmarshal(data, &mf)
	if err != nil {
		err = eh.Errorf("unable to parse manifest: %w", err)
		return
	}
	if mf.Tiles == nil {
		mf.Tiles = make(map[string]manifestEntry, 50_000)
	}
	return
}

func saveManifest(dest string, mf *manifest) (err error) {
	mf.LastUpdated = time.Now().UTC().Format(time.RFC3339)
	p := filepath.Join(dest, "manifest.json")

	var data []byte
	data, err = json.MarshalIndent(mf, "", " ")
	if err != nil {
		err = eh.Errorf("unable to marshal manifest: %w", err)
		return
	}

	err = os.WriteFile(p, data, 0o644)
	if err != nil {
		err = eh.Errorf("unable to write manifest: %w", err)
		return
	}
	return
}


