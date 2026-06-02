package serve

import (
	"fmt"
	"net/http"
	"os"
	"path"
	"strings"
	"sync/atomic"

	"github.com/rs/zerolog/log"
	"github.com/stergiotis/boxer/public/config"
	"github.com/stergiotis/boxer/public/observability/eh"
	cli "github.com/urfave/cli/v2"
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
	err := os.MkdirAll(inst.config.OutputDirectory, 0o600)
	if err != nil {
		return eh.Errorf("unable to create output directory: %w", err)
	}
	for {
		p := inst.nextFilePath()
		_, err = os.Stat(p)
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
		writer.WriteHeader(http.StatusInternalServerError)
	}

	p := inst.nextFilePath()
	fd, err := os.Create(p)
	if err != nil {
		_ = request.Body.Close()
		log.Error().Err(err).Msg("unable to open file, skipping")
		writer.WriteHeader(http.StatusInternalServerError)
		return
	}
	var n int64
	n, err = fd.ReadFrom(request.Body)
	if err != nil {
		_ = fd.Close()
		_ = request.Body.Close()
		log.Error().Err(err).Msg("unable to write to file, skipping")
		writer.WriteHeader(http.StatusInternalServerError)
		return
	}
	_ = request.Body.Close()
	_ = fd.Close()
	writer.WriteHeader(http.StatusAccepted)
	log.Debug().Int64("len", n).Str("file", p).Msg("successfully wrote request body to file")
}

func (inst *FileDumpService) Listen() (err error) {
	err = inst.skipExistingFiles()
	if err != nil {
		return eh.Errorf("unable to skip already existing files: %w", err)
	}
	err = http.ListenAndServe(inst.config.Listen, inst)
	if err != nil {
		return eh.Errorf("unable start http server: %w", err)
	}
	return
}

func NewFileDumpService(cfg *Config) *FileDumpService {
	return &FileDumpService{
		config: cfg,
	}
}

type Config struct {
	Listen              string `toml:"listen"`
	OutputDirectory     string `toml:"outputDirectory"`
	FilePattern         string `toml:"filePattern"`
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
	}
}

func (inst *Config) FromContext(nameTransf config.NameTransformFunc, ctx *cli.Context) (nMessages int) {
	inst.Listen = ctx.String(nameTransf("listen"))
	inst.OutputDirectory = ctx.String(nameTransf("outputDirectory"))
	inst.FilePattern = ctx.String(nameTransf("filePattern"))
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
		Flags: cfg.ToCliFlags(config.IdentityNameTransf, config.IdentityNameTransf),
		Action: func(context *cli.Context) error {
			nMessages := cfg.FromContext(config.IdentityNameTransf, context)
			if nMessages > 0 {
				return eh.Errorf("invalid configuration: %d messages", nMessages)
			}
			f := NewFileDumpService(&cfg)
			return f.Listen()
		},
	}
}
