package serve

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path"
	"strings"
	"sync/atomic"
	"time"

	"github.com/rs/zerolog/log"
	"github.com/stergiotis/boxer/public/config"
	"github.com/stergiotis/boxer/public/keelson/runtime/loopback"
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
	cli "github.com/urfave/cli/v3"
)

type FileDumpService struct {
	config *Config
	n      atomic.Uint64
}

func (inst *FileDumpService) nextFilePath() string {
	v := inst.n.Add(1)
	return path.Join(inst.config.OutputDirectory, fmt.Sprintf(inst.config.FilePattern, v))
}

func (inst *FileDumpService) skipExistingFiles() error {
	// 0o700: a directory needs its execute bit to be entered; 0o600 made
	// every file under a freshly created directory uncreatable.
	err := os.MkdirAll(inst.config.OutputDirectory, 0o700)
	if err != nil {
		return eh.Errorf("unable to create output directory: %w", err)
	}
	for {
		p := inst.nextFilePath()
		_, err = os.Stat(p)
		if err != nil && !os.IsNotExist(err) {
			return eh.Errorf("unable to stat output file: %w", err)
		}
		if os.IsNotExist(err) {
			// no concurrency here
			inst.n.Add(^uint64(0)) // -1
			if inst.n.Load() > 0 {
				log.Info().Str("initialFilePath", p).Msg("found existing files")
			}
			return nil
		}
	}
}

func (inst *FileDumpService) ServeHTTP(writer http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodPost {
		writer.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	if request.Body == nil {
		writer.WriteHeader(http.StatusBadRequest)
		return
	}
	maxBytes := inst.config.MaxBodyBytes
	if maxBytes <= 0 {
		maxBytes = DefaultMaxBodyBytes
	}
	body := http.MaxBytesReader(writer, request.Body, maxBytes)

	p := inst.nextFilePath()
	fd, err := os.Create(p)
	if err != nil {
		_ = body.Close()
		log.Error().Err(err).Msg("unable to open file, skipping")
		writer.WriteHeader(http.StatusInternalServerError)
		return
	}
	var n int64
	n, err = fd.ReadFrom(body)
	if err != nil {
		_ = fd.Close()
		_ = body.Close()
		// A truncated body is not kept: the file holds a request or nothing.
		_ = os.Remove(p)
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			log.Warn().Int64("limit", tooLarge.Limit).Msg("request body over the size limit, skipping")
			writer.WriteHeader(http.StatusRequestEntityTooLarge)
			return
		}
		log.Error().Err(err).Msg("unable to write to file, skipping")
		writer.WriteHeader(http.StatusInternalServerError)
		return
	}
	_ = body.Close()
	_ = fd.Close()
	writer.WriteHeader(http.StatusAccepted)
	log.Debug().Int64("len", n).Str("file", p).Msg("successfully wrote request body to file")
}

// Listen binds the configured address and serves until the listener fails.
// The service has no authentication and writes every body it accepts to
// disk; whatever stands in front of the address decides who may post.
func (inst *FileDumpService) Listen() (err error) {
	err = inst.skipExistingFiles()
	if err != nil {
		return eh.Errorf("unable to skip already existing files: %w", err)
	}
	ln, err := loopback.Gate{Name: "http serve"}.Listen(context.Background(), inst.config.Listen)
	if err != nil {
		return
	}
	srv := &http.Server{Handler: inst, ReadHeaderTimeout: 10 * time.Second}
	err = srv.Serve(ln)
	if err != nil {
		return eh.Errorf("unable to serve http: %w", err)
	}
	return
}

func NewFileDumpService(cfg *Config) *FileDumpService {
	return &FileDumpService{
		config: cfg,
	}
}

// DefaultMaxBodyBytes bounds one request body when Config.MaxBodyBytes is
// not set.
const DefaultMaxBodyBytes int64 = 64 << 20

type Config struct {
	Listen          string `toml:"listen"`
	OutputDirectory string `toml:"outputDirectory"`
	FilePattern     string `toml:"filePattern"`
	// MaxBodyBytes bounds one request body; zero means DefaultMaxBodyBytes.
	MaxBodyBytes        int64 `toml:"maxBodyBytes"`
	validated           bool
	nValidationMessages int
}

func (inst *Config) ToCliFlags(nameTransf config.NameTransformFunc, envVarNameTransf config.NameTransformFunc) []cli.Flag {
	return []cli.Flag{
		&cli.StringFlag{
			Name:     nameTransf("listen"),
			Required: inst.Listen == "",
			Value:    inst.Listen,
		},
		&cli.StringFlag{
			Name:     nameTransf("outputDirectory"),
			Required: inst.OutputDirectory == "",
			Value:    inst.OutputDirectory,
		},
		&cli.StringFlag{
			Name:     nameTransf("filePattern"),
			Required: inst.FilePattern == "",
			Value:    inst.FilePattern,
		},
		&cli.Int64Flag{
			Name:  nameTransf("maxBodyBytes"),
			Value: inst.MaxBodyBytes,
			Usage: "largest request body accepted; 0 means the default of 64 MiB",
		},
	}
}

func (inst *Config) FromContext(ctx context.Context, nameTransf config.NameTransformFunc, cmd *cli.Command) (nMessages int) {
	inst.Listen = cmd.String(nameTransf("listen"))
	inst.OutputDirectory = cmd.String(nameTransf("outputDirectory"))
	inst.FilePattern = cmd.String(nameTransf("filePattern"))
	inst.MaxBodyBytes = cmd.Int64(nameTransf("maxBodyBytes"))
	nMessages = inst.Validate(true)
	return
}

func (inst *Config) Validate(force bool) (nMessages int) {
	if inst.validated && !force {
		return inst.nValidationMessages
	}
	if strings.Count(inst.FilePattern, "%") != 1 {
		log.Error().Str("filePattern", inst.FilePattern).Msg("invalid file pattern, must contain exactly one instance of '%' placeholder")
		nMessages++
	}
	inst.validated = true
	inst.nValidationMessages = nMessages
	return
}

var _ config.ConfigerI = (*Config)(nil)

func NewCommand() *cli.Command {
	cfg := Config{
		Listen:          "",
		OutputDirectory: "",
	}
	return &cli.Command{
		Name:  "serve",
		Usage: "accept HTTP request bodies on --listen and write each to a file in --outputDirectory",
		Flags: cfg.ToCliFlags(config.IdentityNameTransf, config.IdentityNameTransf),
		Action: func(ctx context.Context, cmd *cli.Command) error {
			nMessages := cfg.FromContext(ctx, config.IdentityNameTransf, cmd)
			if nMessages > 0 {
				return eb.Build().Int("nMessages", nMessages).Errorf("invalid configuration: message count out of range")
			}
			f := NewFileDumpService(&cfg)
			return f.Listen()
		},
	}
}
