package example

import (
	"bufio"
	"bytes"
	"encoding/json/jsontext"
	"errors"
	"hash"
	"io"
	"os"
	"slices"
	"strconv"

	"math"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/ipc"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/rs/zerolog/log"
	"github.com/stergiotis/boxer/public/containers"
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
	"github.com/stergiotis/boxer/public/semistructured/leeway/dml"
	"github.com/stergiotis/boxer/public/semistructured/leeway/membership"
	"github.com/urfave/cli/v2"
	"lukechampine.com/blake3"
)

// splitPointer splits the JSON Pointer of the value dec has just read into the
// two halves of a leeway attribute locator: the low-cardinality path, with each
// array position replaced by "_", and the high-cardinality parameters carrying
// the elided indices ([membership.AppendParams]).
//
//	/did                    ->  "/did"                    ""
//	/commit/record/langs/1  ->  "/commit/record/langs/_"  "0001"
//	/a/12/b/3               ->  "/a/_/b/_"                "000c.0003"
//
// Whether a pointer token is an array index is read off the decoder's own
// stack rather than inferred from the token's spelling: pointer token j
// addresses the container at stack level j, and [jsontext.Decoder.StackIndex]
// reports whether that container is an object or an array. Inferring it from
// "the token parses as a number" mis-shreds an object whose key happens to be
// all digits — {"0": …} would land under "/_" with a bogus index parameter.
func splitPointer(dec *jsontext.Decoder, ptr jsontext.Pointer, lc *bytes.Buffer, hc *bytes.Buffer) (lowCard []byte, highCard []byte, err error) {
	lc.Reset()
	hc.Reset()
	level := 0
	for ptk := range ptr.Tokens() {
		level++
		if kind, _ := dec.StackIndex(level); kind != '[' {
			lc.WriteRune('/')
			lc.WriteString(ptk)
			continue
		}
		var u uint64
		u, err = strconv.ParseUint(ptk, 10, 64)
		if err != nil {
			err = eb.Build().Str("pointer", string(ptr)).Str("token", ptk).Int("level", level).Errorf("unable to parse array index: %w", err)
			return
		}
		var enc [membership.ParamsIndexWidth]byte
		var p []byte
		p, err = membership.AppendParams(enc[:0], u)
		if err != nil {
			err = eb.Build().Str("pointer", string(ptr)).Int("level", level).Errorf("unable to encode array index: %w", err)
			return
		}
		if hc.Len() > 0 {
			hc.WriteByte(membership.ParamsSeparator)
		}
		hc.Write(p)
		lc.WriteString("/_")
	}
	lowCard = lc.Bytes()
	highCard = hc.Bytes()
	return
}
func populateJsonEntity(dec *jsontext.Decoder, ent *InEntityJson, hasher hash.Hash, lc *bytes.Buffer, hc *bytes.Buffer, seenPtr *containers.HashSet[string]) (err error) {
	hasher.Reset()
	lc.Reset()
	hc.Reset()
	seenPtr.Clear()

	ent.BeginEntity()
	boolSec := ent.GetSectionBool()
	float64Sec := ent.GetSectionFloat64()
	int64Sec := ent.GetSectionInt64()
	nullSec := ent.GetSectionNull()
	stringSec := ent.GetSectionString()
	undefinedSec := ent.GetSectionUndefined()
	symbolSec := ent.GetSectionSymbol()
	var _ = int64Sec
	var _ = undefinedSec
	var _ = symbolSec
	stack := 0
	for {
		var token jsontext.Token
		token, err = dec.ReadToken()
		if err != nil {
			// EOF inside the loop body always means CommitEntity has not
			// run yet (it lives after the loop), so the builder is in
			// EntityStateInEntity. Roll back so the next BeginEntity on
			// the same builder is a valid state transition.
			_ = ent.RollbackEntity()
			if errors.Is(err, io.EOF) {
				return
			}
			err = eb.Build().Int("stackDepth", stack).Int64("offset", dec.InputOffset()).Errorf("unable to parse json input stream: %w", err)
			return
		}
		ptr := dec.StackPointer()
		isKey := false
		isSymbol := false
		{
			k, _ := dec.StackIndex(dec.StackDepth())
			if k == '{' {
				h := seenPtr.Has(string(ptr))
				if !h {
					seenPtr.Add(string(ptr))
					// dictionary key
					isKey = true
				}
			}
		}
		var lowCardPtr, highCardPtr []byte
		kind := token.Kind()
		switch kind {
		case '{', '[':
			stack++
		case '}', ']':
			stack--
		default:
			if !isKey {
				lowCardPtr, highCardPtr, err = splitPointer(dec, ptr, lc, hc)
				if err != nil {
					err = eb.Build().Str("pointer", string(ptr)).Errorf("unable to split json pointer in low- and high-card part")
					return
				}

				switch string(lowCardPtr) {
				case "/kind", "/commit/operation", "/commit/collection":
					isSymbol = true
				}
				//log.Info().Str("lc", string(lowCardPtr)).Str("hc", string(highCardPtr)).Str("token", string(kind)).Msg("got one")
			}
		}
		if !isKey {
			switch kind {
			case 'n':
				nullSec.BeginAttribute().AddMembershipMixedLowCardVerbatim(lowCardPtr, highCardPtr).EndAttribute()
			case 'f':
				boolSec.BeginAttribute(false).AddMembershipMixedLowCardVerbatim(lowCardPtr, highCardPtr).EndAttribute()
			case 't':
				boolSec.BeginAttribute(true).AddMembershipMixedLowCardVerbatim(lowCardPtr, highCardPtr).EndAttribute()
			case '"':
				if isSymbol {
					symbolSec.BeginAttribute(token.String()).AddMembershipMixedLowCardVerbatim(lowCardPtr, highCardPtr).EndAttribute()
				} else {
					stringSec.BeginAttribute(token.String()).AddMembershipMixedLowCardVerbatim(lowCardPtr, highCardPtr).EndAttribute()
				}
			case '0':
				{
					var v float64
					v, err = token.Float()
					if err != nil {
						err = eb.Build().Str("pointer", string(ptr)).Errorf("unable to read number token as a float64")
						return
					}
					if !math.IsNaN(v) && !math.IsInf(v, -1) && !math.IsInf(v, 1) && math.Floor(v) == math.Ceil(v) {
						var i int64
						i, err = token.Int()
						if err != nil {
							err = eb.Build().Str("pointer", string(ptr)).Errorf("unable to read number token as an int64")
							return
						}
						int64Sec.BeginAttribute(i).AddMembershipMixedLowCardVerbatim(lowCardPtr, highCardPtr).EndAttribute()
					} else {
						float64Sec.BeginAttribute(v).AddMembershipMixedLowCardVerbatim(lowCardPtr, highCardPtr).EndAttribute()
					}
				}
			case '{', '[', '}', ']':
				break
			default:
				return eb.Build().Str("kind", string(token.Kind())).Errorf("unhandled token kind")
			}
		}
		if stack == 0 {
			break
		}
	}
	ent.SetId(hasher.Sum(nil))
	err = ent.CommitEntity()
	if err != nil {
		err = eh.Errorf("unable to commit entity: %w", err)
		return
	}
	return
}

const (
	compressionUncompressed string = "uncompressed"
	compressionZstd         string = "zstd"
)

var allCompressions = []string{compressionUncompressed, compressionZstd}
var compressionFlag = &cli.StringFlag{
	Name:  "compression",
	Value: compressionUncompressed,
	Action: func(context *cli.Context, s string) error {
		if slices.Index(allCompressions, s) < 0 {
			return eb.Build().Str("compression", s).Strs("possibleValues", allCompressions).Errorf("unknown compression flag")
		}
		return nil
	},
}

func NewCliCommand() *cli.Command {
	return &cli.Command{
		Name: "example",
		Subcommands: []*cli.Command{
			{
				Name:  "ndjson",
				Usage: "a poor mans ndjson cleanup routine",
				Subcommands: []*cli.Command{
					{
						Name: "cleanup",
						Flags: []cli.Flag{
							&cli.UintFlag{
								Name:  "maxInputJsonSize",
								Value: 32 * 1024 * 1024,
							},
							&cli.UintFlag{
								Name:  "maxOutputJsonSize",
								Value: 32 * 1024 * 1024,
							},
						},
						Action: func(context *cli.Context) error {
							sc := bufio.NewScanner(os.Stdin)
							sc.Split(bufio.ScanLines)
							maxInputJsonSize := context.Uint("maxInputJsonSize")
							maxOutputJsonSize := context.Uint("maxOutputJsonSize")
							sc.Buffer(make([]byte, 0, maxInputJsonSize), int(maxInputJsonSize))
							dec := jsontext.NewDecoder(io.MultiReader())
							out := bufio.NewWriter(os.Stdout)
							defer out.Flush()
							skipped := 0
							passthrough := 0
							for sc.Scan() {
								line := sc.Bytes()
								if len(line) > int(maxOutputJsonSize) {
									skipped++
									log.Info().Int("skipped", skipped).Int("lineLength", len(line)).Uint("maxOutputJsonSize", maxOutputJsonSize).Msg("line exceeds maxOutputJsonSize limit, skipping")
									continue
								}
								dec.Reset(bytes.NewReader(line))
								ok := true
								for {
									_, err := dec.ReadToken()
									if err != nil {
										if errors.Is(err, io.EOF) {
											break
										} else {
											dec = jsontext.NewDecoder(io.MultiReader())
											skipped++
											log.Warn().Err(err).Int("skipped", skipped).Int("messageLength", len(line)).Msg("skipping invalid json")
											err = nil
											ok = false
										}
									}
								}
								if ok {
									passthrough++
									_, err := out.Write(line)
									if err != nil {
										return err
									}
									_, err = out.WriteRune('\n')
									if err != nil {
										return err
									}
								}
							}
							err := sc.Err()
							if errors.Is(err, io.EOF) {
								err = nil
							} else if err != nil {
								err = eh.Errorf("error while reading input: %w", err)
								return err
							}
							log.Info().Int("skipped", skipped).Int("passthrough", passthrough).Msg("cleanup statistics")
							return nil
						},
					},
				},
			},
			{
				Name: "convert",
				Flags: []cli.Flag{
					compressionFlag,
					&cli.UintFlag{
						Name:  "maxInputJsonSize",
						Value: 32 * 1024 * 1024,
					},
					&cli.UintFlag{
						Name:  "maxOutputJsonSize",
						Value: 32 * 1024 * 1024,
					},
				},
				Action: func(context *cli.Context) (err error) {
					var schema *arrow.Schema
					var w *ipc.FileWriter
					stdoutBuf := bufio.NewWriter(os.Stdout)
					defer stdoutBuf.Flush()
					allocator := memory.DefaultAllocator
					const batchSize = 4096
					ent := NewInEntityJson(allocator, 1)
					schema = ent.GetSchema()
					compression := context.String(compressionFlag.Name)
					maxInputJsonSize := context.Uint("maxInputJsonSize")
					maxOutputJsonSize := context.Uint("maxOutputJsonSize")

					opts := make([]ipc.Option, 0, 8)
					opts = append(opts, ipc.WithAllocator(allocator))
					opts = append(opts, ipc.WithSchema(schema))
					opts = append(opts, ipc.WithDictionaryDeltas(true))
					switch compression {
					case compressionZstd:
						opts = append(opts, ipc.WithZstd())
					case compressionUncompressed:
						break
					}
					log.Info().Str("compression", compression).Msg("using apache arrow IPC output format")
					w, err = ipc.NewFileWriter(stdoutBuf, opts...)
					if err != nil {
						err = eh.Errorf("unable to create arrow ipc file writer: %w", err)
						return
					}
					defer w.Close()
					records := make([]arrow.RecordBatch, 0, 1)
					lc := bytes.NewBuffer(make([]byte, 0, 4*4096))
					hc := bytes.NewBuffer(make([]byte, 0, 4*4096))
					tmp := containers.NewHashSet[string](2048)
					{
						dec := jsontext.NewDecoder(os.Stdin)
						hasher := blake3.New(256/8, nil)
						i := 0
						if true {
							sc := bufio.NewScanner(os.Stdin)
							sc.Buffer(make([]byte, 0, maxInputJsonSize), int(maxInputJsonSize))
							sc.Split(bufio.ScanLines)
							for sc.Scan() {
								line := sc.Bytes()
								if len(line) > int(maxOutputJsonSize) {
									log.Info().Int("lineLength", len(line)).Uint("maxOutputJsonSize", maxOutputJsonSize).Msg("line exceeds maxOutputJsonSize limit, skipping")
									continue
								}
								dec.Reset(bytes.NewReader(line))
								err = populateJsonEntity(dec, ent, hasher, lc, hc, tmp)
								if err != nil {
									dec = jsontext.NewDecoder(io.MultiReader())
									if true {
										log.Warn().Err(err).Msg("skipping invalid json")
										err = nil
										continue
									}
									err = eh.Errorf("unable to populate json entity: %w", err)
									return
								}
								if i > 0 && i%batchSize == 0 {
									records, err = dml.WriteArrowRecords(ent, records, w)
									if err != nil {
										err = eh.Errorf("unable to write arrow records: %w", err)
										return
									}
								}
								i++
							}
							err = sc.Err()
							if errors.Is(err, io.EOF) {
								err = nil
							} else if err != nil {
								err = eh.Errorf("error while reading input: %w", err)
								return
							}
						} else {
							for {
								err = populateJsonEntity(dec, ent, hasher, lc, hc, tmp)
								if err != nil {
									if errors.Is(err, io.EOF) {
										err = nil
										break
									}
									err = eh.Errorf("unable to populate json entity: %w", err)
									return
								}
								if i > 0 && i%batchSize == 0 {
									records, err = dml.WriteArrowRecords(ent, records, w)
									if err != nil {
										err = eh.Errorf("unable to write arrow records: %w", err)
										return
									}
								}
								i++
							}
						}
						_, err = dml.WriteArrowRecords(ent, records, w)
						if err != nil {
							err = eh.Errorf("unable to write arrow records: %w", err)
							return
						}
					}

					return
				},
			},
		},
	}
}
