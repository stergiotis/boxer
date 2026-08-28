package cli

import (
	"io"
	"os"
	"slices"

	"github.com/rs/zerolog/log"
	"github.com/stergiotis/boxer/public/config"
	cli2 "github.com/stergiotis/boxer/public/hmi/cli"
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/semistructured/leeway/canonwire"
	cwruntime "github.com/stergiotis/boxer/public/semistructured/leeway/canonwire/runtime"
	"github.com/stergiotis/boxer/public/semistructured/leeway/common"
	"github.com/stergiotis/boxer/public/semistructured/leeway/ddl"
	"github.com/stergiotis/boxer/public/semistructured/leeway/ddl/clickhouse"
	"github.com/stergiotis/boxer/public/semistructured/leeway/gocodegen"
	"github.com/stergiotis/boxer/public/semistructured/leeway/naming"
	"github.com/urfave/cli/v2"
)

// leeway canonwire (ADR-0207 §SD6): the canonical-wire generator's CLI, plus
// the two things an operator needs when the bytes and the table disagree.
//
// `table generate go` is the generator, mirroring `leeway dml table generate
// go`. The other two exist because the wire form is keyed on canonical type
// signatures and nothing else: `table slots` shows which signature keys which
// section — and which signatures more than one section claims, the SD5
// ambiguity sets a table needs a dispatcher for — and `verify` checks bytes
// against the form without any table at all, which is what tells a producer's
// bug apart from a consumer's.

// NewCliCommandCanonWire builds the `leeway canonwire` command group.
func NewCliCommandCanonWire() *cli.Command {
	var universal *cli2.UniversalCliFormatter
	{
		var err error
		universal, err = cli2.NewUniversalCliFormatter(config.IdentityNameTransf)
		if err != nil {
			log.Panic().Err(err).Msg("unable to create universal formatter")
		}
	}
	universalFlags := universal.ToCliFlags()
	return &cli.Command{
		Name:  "canonwire",
		Usage: "generate the canonical-wire codecs for a table, inspect its slot table, and verify wire bytes (ADR-0207)",
		Subcommands: []*cli.Command{
			{
				Name: "table",
				Subcommands: []*cli.Command{
					{
						Name: "generate",
						Subcommands: []*cli.Command{
							{
								Name:  "go",
								Usage: "read a CBOR table description from stdin, write the table's canonical-wire Go classes to stdout",
								Flags: []cli.Flag{
									&cli.StringFlag{
										Name:     "tableName",
										Required: true,
									},
									&cli.StringFlag{
										Name:     "packageName",
										Required: true,
									},
								},
								Action: func(context *cli.Context) error {
									tblDesc, err := decodeCanonWireTableDesc(os.Stdin)
									if err != nil {
										return err
									}

									var conv *ddl.HumanReadableNamingConvention
									conv, err = ddl.NewHumanReadableNamingConvention(":")
									if err != nil {
										return eh.Errorf("unable to create human readable convention: %w", err)
									}
									chTech := clickhouse.NewTechnologySpecificCodeGenerator()
									driver := canonwire.NewGoCodeGeneratorDriver(conv, chTech)

									tableRowConfig := common.TableRowConfigMultiAttributesPerRow
									tableName := context.String("tableName")
									packageName := context.String("packageName")
									var wellFormed bool
									var sourceCode []byte
									namingStyle := gocodegen.NewDefaultGoClassNamer()
									sourceCode, wellFormed, err = driver.GenerateGoClasses(packageName, naming.MustBeValidStylableName(tableName), tblDesc, tableRowConfig, namingStyle)
									if err != nil {
										return eh.Errorf("unable to generate go classes: %w", err)
									}
									if !wellFormed {
										log.Warn().Msg("output is not well-formed go code")
									}

									_, err = os.Stdout.Write(sourceCode)
									if err != nil {
										return eh.Errorf("unable to write to stdout: %w", err)
									}
									return nil
								},
							},
						},
					},
					{
						Name:  "slots",
						Usage: "read a CBOR table description from stdin and report the wire slots it keys (ADR-0207 §SD2/§SD5)",
						Flags: slices.Concat([]cli.Flag{
							&cli.StringFlag{
								Name:  "tableName",
								Usage: "label for the report; defaults to the description's own dictionary entry name",
							},
						}, universalFlags),
						Action: func(context *cli.Context) error {
							rep, err := canonWireSlots(os.Stdin, context.String("tableName"))
							if err != nil {
								return err
							}
							return universal.FormatValue(context, rep)
						},
					},
				},
			},
			{
				Name:  "verify",
				Usage: "check wire bytes against the canonical form — no table description is read or needed",
				Flags: slices.Concat([]cli.Flag{
					&cli.BoolFlag{
						Name:  "sequence",
						Usage: "the input is a CBOR sequence of entities (RFC 8742) rather than a single entity item",
					},
					&cli.StringFlag{
						Name:  "file",
						Usage: "read the bytes from this path instead of stdin",
					},
				}, universalFlags),
				Action: func(context *cli.Context) error {
					b, err := readCanonWireBytes(context.String("file"))
					if err != nil {
						return err
					}
					var rep canonWireVerifyReport
					rep, err = canonWireVerify(b, context.Bool("sequence"))
					if err != nil {
						// Returned, not printed: a verification failure must
						// leave a non-zero exit status behind, which is the
						// only part of this a script reads.
						return err
					}
					return universal.FormatValue(context, rep)
				},
			},
		},
	}
}

// decodeCanonWireTableDesc reads the CBOR table description every canonwire
// subcommand that needs one takes on stdin, in the encoding `leeway ddl`
// writes.
func decodeCanonWireTableDesc(r io.Reader) (tblDesc common.TableDesc, err error) {
	var marshaller *common.TableMarshaller
	marshaller, err = common.NewTableMarshaller()
	if err != nil {
		err = eh.Errorf("unable to create table marshaller: %w", err)
		return
	}
	var dto common.TableDescDto
	err = marshaller.DecodeDtoCbor(r, &dto)
	if err != nil {
		err = eh.Errorf("unable to decode table description dto encoded in CBOR: %w", err)
		return
	}
	err = tblDesc.LoadFrom(&dto)
	if err != nil {
		err = eh.Errorf("unable to load table from dto: %w", err)
		return
	}
	return
}

// readCanonWireBytes reads the wire bytes to check: a file when one is named,
// stdin otherwise.
func readCanonWireBytes(path string) (b []byte, err error) {
	if path != "" {
		b, err = os.ReadFile(path)
		if err != nil {
			err = eh.Errorf("unable to read the wire bytes from the file: %w", err)
		}
		return
	}
	b, err = io.ReadAll(os.Stdin)
	if err != nil {
		err = eh.Errorf("unable to read the wire bytes from stdin: %w", err)
	}
	return
}

// canonWireSlotReport is one wire slot: the signature that keys it, the
// sections behind it in signature order, and whether that signature is shared
// — a shared one is an SD5 ambiguity set and needs a dispatcher.
type canonWireSlotReport struct {
	Ordinal   int
	Signature string
	Sections  []string
	Ambiguous bool
}

// canonWirePlainReport is one plain slot. It is keyed on the wire by its item
// type (ADR-0207 §SD2, fork 1); the group is carried for the decoder's
// construction-time equality check and never travels.
type canonWirePlainReport struct {
	ItemType string
	Group    string
}

// canonWireSlotsReport is a table description as the wire form sees it.
type canonWireSlotsReport struct {
	TableName string
	Slots     []canonWireSlotReport
	Plains    []canonWirePlainReport
	// Ambiguous are the signatures carried by two or more slots, sorted. A
	// table with any of these cannot construct a decoder without a
	// DispatcherI, so this list is the answer to "does this table need a
	// dispatch plugin".
	Ambiguous []string
}

// canonWireSlots reduces a CBOR table description to its wire slots.
//
// Separated from the urfave plumbing so the reduction is testable without a
// process — the same split `leeway ddl compose` and `leeway sqlsurface` use.
func canonWireSlots(r io.Reader, tableName string) (rep canonWireSlotsReport, err error) {
	var tblDesc common.TableDesc
	tblDesc, err = decodeCanonWireTableDesc(r)
	if err != nil {
		return
	}
	var tbl canonwire.SlotTable
	tbl, err = canonwire.BuildSlotTable(&tblDesc)
	if err != nil {
		err = eh.Errorf("unable to build the slot table: %w", err)
		return
	}

	rep.TableName = tableName
	if rep.TableName == "" {
		rep.TableName = string(tblDesc.DictionaryEntry.Name)
	}
	rep.Slots = make([]canonWireSlotReport, 0, len(tbl.Slots))
	for i := range tbl.Slots {
		slot := &tbl.Slots[i]
		names := make([]string, 0, len(slot.Sections))
		for _, sec := range slot.Sections {
			names = append(names, string(sec.Name))
		}
		rep.Slots = append(rep.Slots, canonWireSlotReport{
			Ordinal:   i,
			Signature: slot.Signature,
			Sections:  names,
			Ambiguous: len(tbl.BySignature[slot.Signature]) > 1,
		})
	}
	rep.Plains = make([]canonWirePlainReport, 0, len(tbl.Plains))
	for _, plain := range tbl.Plains {
		rep.Plains = append(rep.Plains, canonWirePlainReport{
			ItemType: plain.ItemType.String(),
			Group:    plain.Group,
		})
	}
	rep.Ambiguous = tbl.Ambiguous()
	return
}

// canonWireVerifyReport is what a passing check has to say: how much was read,
// and how many entities came out of it.
type canonWireVerifyReport struct {
	Entities int
	Bytes    int
}

// canonWireVerify runs the table-free canonical check over one entity item, or
// over a CBOR sequence of them.
//
// The failing entity's index rides in the runtime's own error for the sequence
// form, so nothing is reported here on failure: the report describes bytes that
// passed.
func canonWireVerify(b []byte, sequence bool) (rep canonWireVerifyReport, err error) {
	var n int
	if sequence {
		n, err = cwruntime.VerifyCanonicalSequence(b)
		if err != nil {
			err = eh.Errorf("the byte sequence is not canonical: %w", err)
			return
		}
	} else {
		err = cwruntime.VerifyCanonical(b)
		if err != nil {
			err = eh.Errorf("the entity item is not canonical: %w", err)
			return
		}
		n = 1
	}
	rep.Entities = n
	rep.Bytes = len(b)
	return
}
