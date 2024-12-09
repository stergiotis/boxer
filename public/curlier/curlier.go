package curlier

import (
	"bytes"
	"crypto/tls"
	"errors"
	"io"
	"math"
	"net"
	"net/http"
	"strings"
	"time"

	backoff "github.com/cenkalti/backoff/v4"
	"github.com/rs/zerolog/log"
	cli "github.com/urfave/cli/v2"

	"github.com/stergiotis/boxer/public/config"
	"github.com/stergiotis/boxer/public/observability/eh"
)

type CurlierConfig struct {
	Request string   `json:"request"`
	Url     string   `json:"url"`
	Headers []string `json:"headers"`

	Insecure bool `json:"insecure"`

	User  string `json:"user"`
	Basic bool   `json:"basic"`

	ConnectTimeout float64 `json:"connectionTimeout"`
	MaxTime        float64 `json:"maxTime"`

	MaxRedirs    int     `json:"maxRedirs"`
	RetryDelay   float64 `json:"retryDelay"`
	RetryMaxTime float64 `json:"retryMaxTime"`

	Retry int `json:"retry"`

	header     *http.Header
	headerKeys []string

	validated           bool
	nValidationMessages int
}

func (inst *CurlierConfig) ToCliFlags(nameTransf config.NameTransformFunc, envVarNameTransf config.NameTransformFunc) []cli.Flag {
	return []cli.Flag{
		&cli.StringFlag{
			Name:     nameTransf("request"),
			Aliases:  []string{nameTransf("X")},
			Value:    inst.Request,
			Category: "Curlier::http",
			Required: inst.Request == "",
		},
		&cli.StringSliceFlag{
			Name:     nameTransf("header"),
			Aliases:  []string{nameTransf("H")},
			Value:    cli.NewStringSlice(inst.Headers...),
			Category: "Curlier::http",
		},
		&cli.StringFlag{
			Name:     nameTransf("url"),
			Value:    inst.Url,
			Category: "Curlier::http",
			Required: inst.Url == "",
		},

		&cli.BoolFlag{
			Name:     nameTransf("insecure"),
			Aliases:  []string{nameTransf("k")},
			Value:    inst.Insecure,
			Category: "Curlier::tls",
		},

		&cli.StringFlag{
			Name:     nameTransf("user"),
			Aliases:  []string{nameTransf("u")},
			Value:    inst.User,
			Category: "Curlier::auth",
		},
		&cli.BoolFlag{
			Name:     nameTransf("basic"),
			Value:    inst.Basic,
			Category: "Curlier::auth",
		},

		&cli.Float64Flag{
			Name:     nameTransf("connectTimeout"),
			Aliases:  []string{nameTransf("connect-timeout")},
			Value:    inst.ConnectTimeout,
			Category: "Curlier::timeout",
		},
		&cli.Float64Flag{
			Name:     nameTransf("maxTime"),
			Aliases:  []string{nameTransf("max-time")},
			Value:    inst.MaxTime,
			Category: "Curlier::timeout",
		},
		&cli.IntFlag{
			Name:     nameTransf("maxRedirs"),
			Aliases:  []string{nameTransf("max-redirs")},
			Value:    inst.MaxRedirs,
			Category: "Curlier::redirection",
		},

		&cli.IntFlag{
			Name:     nameTransf("retry"),
			Value:    inst.Retry,
			Category: "Curlier::retry",
		},
		&cli.Float64Flag{
			Name:     nameTransf("retryDelay"),
			Aliases:  []string{nameTransf("retry-delay")},
			Value:    inst.RetryDelay,
			Category: "Curlier::retry",
		},
		&cli.Float64Flag{
			Name:     nameTransf("retryMaxTime"),
			Aliases:  []string{nameTransf("retry-max-time")},
			Value:    inst.RetryMaxTime,
			Category: "Curlier::retry",
		},
	}
}

func (inst *CurlierConfig) FromContext(nameTransf config.NameTransformFunc, ctx *cli.Context) (nMessages int) {
	inst.Request = ctx.String(nameTransf("request"))
	inst.Url = ctx.String(nameTransf("url"))
	inst.Headers = ctx.StringSlice(nameTransf("headers"))
	inst.Insecure = ctx.Bool(nameTransf("insecure"))
	inst.User = ctx.String(nameTransf("user"))
	inst.Basic = ctx.Bool(nameTransf("basic"))
	inst.ConnectTimeout = ctx.Float64(nameTransf("connectionTimeout"))
	inst.MaxTime = ctx.Float64(nameTransf("maxTime"))
	inst.MaxRedirs = ctx.Int(nameTransf("maxRedirs"))
	inst.RetryDelay = ctx.Float64(nameTransf("retryDelay"))
	inst.Retry = ctx.Int(nameTransf("retry"))
	inst.RetryMaxTime = ctx.Float64(nameTransf("retryMaxTime"))

	return inst.Validate(true)
}

func (inst *CurlierConfig) Validate(force bool) (nMessages int) {
	if inst.validated && !force {
		return inst.nValidationMessages
	}
	switch inst.Request {
	case http.MethodGet, http.MethodDelete, http.MethodPost, http.MethodPut, http.MethodHead, http.MethodConnect, http.MethodOptions, http.MethodPatch, http.MethodTrace:
		break
	default:
		log.Error().Str("request", inst.Request).Msg("unhandled or unsupported request method")
		nMessages++
	}
	header := &http.Header{}
	keys := make([]string, 0, len(inst.Headers))
	for _, h := range inst.Headers {
		k, err := ParseHeaderArgument(header, h)
		if err != nil {
			log.Error().Err(err).Str("header", h).Msg("unable to parse header")
			nMessages++
			continue
		} else {
			keys = append(keys, k)
		}
	}
	inst.header = header
	inst.headerKeys = keys

	if inst.User != "" && !strings.ContainsRune(inst.User, ':') {
		log.Error().Msg("user option does not contain colon (:) character. expecting <user>:<password>")
		nMessages++
	}

	inst.nValidationMessages = nMessages
	inst.validated = true
	return
}

func (inst *CurlierConfig) GetHeaders() (keys []string, header *http.Header, err error) {
	if inst.validated && inst.nValidationMessages == 0 {
		return inst.headerKeys, inst.header, nil
	} else {
		return nil, nil, errors.New("config is not validated")
	}
}

var _ config.Configer = (*CurlierConfig)(nil)

type Curlier struct {
	config     *CurlierConfig
	transport  *http.Transport
	dialer     *net.Dialer
	client     *http.Client
	bo         backoff.BackOff
	bodyBuffer *bytes.Buffer
}

func (inst *Curlier) Run(reqBody io.Reader) (resp *http.Response, err error) {
	cfg := inst.config

	var req *http.Request
	req, err = http.NewRequest(cfg.Request, cfg.Url, reqBody)
	if err != nil {
		err = eh.Errorf("unable to create http request: %w", err)
		return
	}
	if cfg.Basic {
		user, pw, ok := strings.Cut(cfg.User, ":")
		if !ok {
			err = errors.New("user option does not contain colon (:) character. expecting <user>:<password>")
			return
		}
		req.SetBasicAuth(user, pw)
	}

	var headers *http.Header
	var keys []string
	keys, headers, err = cfg.GetHeaders()
	if err != nil {
		err = eh.Errorf("unable to get headers from config: %w", err)
		return
	}

	chunked := false
	for _, k := range keys {
		vs := headers.Values(k)
		if k == "Transfer-Encoding" {
			for _, v := range vs {
				chunked = chunked || v == "chunked"
			}
		}
		for _, v := range vs {
			req.Header.Add(k, v)
		}
	}
	if !chunked {
		b := inst.bodyBuffer
		var n int64
		n, err = b.ReadFrom(reqBody)
		if err != nil {
			err = eh.Errorf("error reading body: %w", err)
			return
		}

		req.Body = io.NopCloser(b)
		req.ContentLength = n
	}

	resp, err = inst.client.Do(req)
	if err != nil {
		if resp != nil && resp.Body != nil {
			_ = resp.Body.Close()
		}
		err = eh.Errorf("unable to perform http request: %w", err)
		return
	}
	if cfg.Retry > 0 {
		t := 1
		bo := inst.bo
		bo.Reset()
		for IsTransientError(resp.StatusCode) && t < cfg.Retry {
			t++
			time.Sleep(bo.NextBackOff())
			resp, err = inst.client.Do(req)
			if err != nil {
				if resp != nil && resp.Body != nil {
					_ = resp.Body.Close()
				}
				err = eh.Errorf("unable to perform http request: %w", err)
				return
			} else {
				return
			}
		}
	}

	return
}

func NewCurlier(cfg *CurlierConfig) (*Curlier, error) {
	nMessages := cfg.Validate(false)
	if nMessages > 0 {
		return nil, eh.Errorf("validation of config failed with %d messages", nMessages)
	}
	tr := &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: cfg.Insecure},
	}
	dialer := &net.Dialer{}
	if cfg.ConnectTimeout > 0.0 && !math.IsInf(cfg.ConnectTimeout, 0) {
		dialer.Timeout = time.Millisecond * time.Duration(cfg.ConnectTimeout*1000.0)
	}
	tr.DialContext = dialer.DialContext
	client := &http.Client{Transport: tr}
	if cfg.MaxTime > 0.0 && !math.IsInf(cfg.MaxTime, 0) {
		client.Timeout = time.Millisecond * time.Duration(cfg.MaxTime*1000.0)
	}
	var bo backoff.BackOff
	if cfg.RetryDelay > 0.0 && !math.IsInf(cfg.RetryDelay, 0) {
		bo = &backoff.ConstantBackOff{
			Interval: time.Millisecond * time.Duration(cfg.RetryDelay*1000.0),
		}
	} else {
		bo = &backoff.ExponentialBackOff{
			InitialInterval:     time.Second,
			RandomizationFactor: 0.03,
			Multiplier:          2.0,
			MaxInterval:         time.Minute * 10,
			MaxElapsedTime:      time.Millisecond * time.Duration(cfg.RetryMaxTime*1000.0),
		}
	}
	return &Curlier{
		config:     cfg,
		transport:  tr,
		dialer:     dialer,
		client:     client,
		bo:         bo,
		bodyBuffer: bytes.NewBuffer(make([]byte, 0, 4096)),
	}, nil
}
