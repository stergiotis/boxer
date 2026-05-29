package compression

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"regexp"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"

	"iter"
	"slices"

	"github.com/rs/zerolog/log"
	"github.com/stergiotis/boxer/public/identity/fibonaccicode"
	"github.com/urfave/cli/v2"
)

func splitInRunes(str string) (r []string) {
	r = make([]string, 0, 32)
	for _, t := range str {
		r = append(r, string(t))
	}
	return
}

func ngram(str string, n int) (r []string) {
	runes := splitInRunes(str)
	r = make([]string, 0, 32)
	for c := range slices.Chunk(runes, n) {
		r = append(r, strings.Join(c, ""))
	}
	return
}

func ngramWindowed(str string, n int) (r []string) {
	runes := splitInRunes(str)
	r = []string{}
	for c := range slices.Chunk(runes, n) {
		r = append(r, strings.Join(c, ""))
	}
	u := n
	if len(str) < u {
		n = len(str)
	}
	for o := 1; o < n; o++ {
		r = append(r, strings.Join(runes[:o], ""))
		for c := range slices.Chunk(runes[o:], n) {
			r = append(r, strings.Join(c, ""))
		}
	}
	return
}

type NGramHist struct {
	vs      []string
	ns      []uint32
	cws     []string
	cwNBits []int
}

func NewNGramHist(nEst int) *NGramHist {
	return &NGramHist{
		vs: make([]string, 0, nEst),
		ns: make([]uint32, 0, nEst),
	}
}

func (inst *NGramHist) Add(ngram string) {
	inst.AddN(ngram, 1)
}

func (inst *NGramHist) AddN(ngram string, n uint32) {
	i := slices.Index(inst.vs, ngram)
	if i >= 0 {
		inst.ns[i] += n
	} else {
		inst.vs = append(inst.vs, ngram)
		inst.ns = append(inst.ns, n)
	}
}

func (inst *NGramHist) Contains(ngram string) bool {
	return slices.Index(inst.vs, ngram) >= 0
}

func (inst *NGramHist) AddIfNotContained(ngram string) {
	if inst.Contains(ngram) {
		return
	}
	inst.Add(ngram)
}

func (inst *NGramHist) Len() int {
	return len(inst.vs)
}

func (inst *NGramHist) Swap(i int, j int) {
	inst.vs[j], inst.vs[i] = inst.vs[i], inst.vs[j]
	inst.ns[j], inst.ns[i] = inst.ns[i], inst.ns[j]
}

func (inst *NGramHist) Less(i int, j int) bool {
	ni := inst.ns[i]
	nj := inst.ns[j]
	if ni == nj {
		vi := inst.vs[i]
		vj := inst.vs[j]
		li := len(vi)
		lj := len(vj)
		c := strings.Compare(inst.vs[i], inst.vs[j])
		if li == lj {
			return c <= 0
		} else {
			return li > lj
		}
	}
	return ni > nj
}

func (inst *NGramHist) Sort() {
	sort.Sort(inst)
}

func (inst *NGramHist) Frequency() iter.Seq2[string, uint32] {
	return func(yield func(string, uint32) bool) {
		vs := inst.vs
		ns := inst.ns
		for i, s := range vs {
			if !yield(s, ns[i]) {
				return
			}
		}
	}
}

type EncodeFunc func(rank uint64, total uint64) (code uint64, nBits int)

func (inst *NGramHist) CalcCodewords(encode EncodeFunc) {
	inst.Sort()
	cws := make([]string, 0, len(inst.ns))
	cwsNBits := make([]int, 0, len(inst.ns))
	vs := inst.vs
	nTotal := uint64(len(vs))
	for i, _ := range vs {
		code, nBits := encode(uint64(i), nTotal)
		cws = append(cws, fmt.Sprintf("0b%064b", code) /*[:nBits+len("0b")]*/)
		cwsNBits = append(cwsNBits, nBits)
	}
	inst.cws = cws
	inst.cwNBits = cwsNBits
}

func (inst *NGramHist) Codewords() iter.Seq2[string, string] {
	return func(yield func(string, string) bool) {
		vs := inst.vs
		cws := inst.cws
		for i, s := range vs {
			if !yield(s, cws[i]) {
				return
			}
		}
	}
}

func (inst *NGramHist) EncodeNaive(str string, n int) (codewords []string, totalBits int) {
	vs := inst.vs
	codewords = make([]string, 0, len(str))
	cws := inst.cws
	cwNBits := inst.cwNBits
	for str != "" {
		for i, s := range vs {
			if strings.HasPrefix(str, s) {
				cw := cws[i]
				codewords = append(codewords, cw)
				str = strings.TrimPrefix(str, s)
				totalBits += cwNBits[i]
			}
		}
	}
	return
}

func (inst *NGramHist) EncodeGreedy(str string, n int) (codewords []string, totalBits int) {
	vs := inst.vs
	l := len(str)
	codewords = make([]string, l, l)
	neededBits := make([]int, l, l)
	cws := inst.cws
	cwNBits := inst.cwNBits
	replaced := 0
	for replaced < l {
		for i, s := range vs {
			for {
				idx := strings.Index(str, s)
				if idx >= 0 {
					codewords[idx] = cws[i]
					neededBits[idx] = cwNBits[i]
					sLen := len(s)
					repl := strings.Repeat(" ", sLen)
					str = strings.Replace(str, s, repl, 1)
					replaced += sLen
				} else {
					// next codeword
					break
				}
			}
		}
	}
	for _, t := range neededBits {
		totalBits += t
	}
	return
}

var _ sort.Interface = (*NGramHist)(nil)

func NewDictCommand() *cli.Command {
	return &cli.Command{
		Name: "dict",
		Flags: []cli.Flag{
			&cli.UintFlag{
				Name:  "nGramN",
				Value: 3,
			},
			&cli.StringFlag{
				Name:  "encoding",
				Value: "fibonacci",
			},
			&cli.StringFlag{
				Name:  "output",
				Value: "stat",
			},
			&cli.BoolFlag{
				Name:  "lowercase",
				Value: false,
			},
		},
		Action: func(context *cli.Context) error {
			corpus, err := io.ReadAll(bufio.NewReader(os.Stdin))
			if err != nil {
				return err
			}
			lowercase := context.Bool("lowercase")
			var corpusStr string
			if lowercase {
				corpusStr = strings.ToLower(string(corpus))
			} else {
				corpusStr = string(corpus)
			}
			words := strings.Split(corpusStr, "\n")
			acceptanceRgx := regexp.MustCompile("^[a-zA-Z0-9_.-]+$")
			numericRgx := regexp.MustCompile("^[0-9]+$")
			var _ = numericRgx

			cleaned := make(map[string]struct{}, len(words)*4)
			var underscodes uint32
			var hyphen uint32
			var dots uint32
			for _, w := range words {
				if acceptanceRgx.MatchString(w) {
					us := splitCamelCase(w)
					for _, u := range us {
						underscodes += uint32(strings.Count(u, "_"))
						hyphen += uint32(strings.Count(u, "-"))
						dots += uint32(strings.Count(u, "."))
						vs := strings.Split(strings.ReplaceAll(strings.ReplaceAll(u, ".", "_"), "-", "_"), "_")
						for _, v := range vs {
							//if !numericRgx.MatchString(v) {
							cleaned[v] = struct{}{}
							//}
						}
					}
				}
			}
			var fCleaned *os.File
			fCleaned, err = os.Create("cleaned.txt")
			if fCleaned != nil {
				defer fCleaned.Close()
			}
			if err != nil {
				return err
			}
			nGramN := int(context.Uint("nGramN"))
			h := NewNGramHist(len(cleaned) * 6)
			for k, _ := range cleaned {
				_, _ = fCleaned.WriteString(k)
				_, _ = fCleaned.WriteString("\n")
				ts := ngramWindowed(k, nGramN)
				for _, t := range ts {
					h.Add(t)
				}
			}
			for _, c := range "abcdefghijklmnopqrstuvwxyz" {
				h.AddIfNotContained(string(c))
				if !lowercase {
					h.AddIfNotContained(strings.ToUpper(string(c)))
				}
			}
			for _, c := range "0123456789._-" {
				h.AddIfNotContained(string(c))
			}
			h.AddN("_", underscodes)
			h.AddN("-", hyphen)
			h.AddN(".", dots)

			encoding := context.String("encoding")
			switch encoding {
			case "fibonacci":
				h.CalcCodewords(func(rank uint64, total uint64) (code uint64, nBits int) {
					code, nBits = fibonaccicode.EncodeFibonacciCode(rank)
					return
				})
				break
			case "binary":
				h.CalcCodewords(func(rank uint64, total uint64) (code uint64, nBits int) {
					nBits = int(math.Ceil(math.Log2(float64(total))))
					code = rank
					return
				})
				break
			case "unary":
				h.CalcCodewords(func(rank uint64, total uint64) (code uint64, nBits int) {
					nBits = int(rank)
					code = (uint64(1) << (rank + 1)) - 1
					return
				})
				break
			default:
				return errors.New("unkown encoding")
			}

			output := context.String("output")
			switch output {
			case "dict":
				for s, n := range h.Codewords() {
					_, _ = fmt.Fprintf(os.Stdout, "%s\t%s\n", s, n)
				}
				break
			case "freq":
				for s, n := range h.Frequency() {
					_, _ = fmt.Fprintf(os.Stdout, "%s\t%d\n", s, n)
				}
				break
			case "stat":
				{
					totalBits := 0
					totalCharacters := 0
					alphabetSize := 26 + 10 + 3
					if !lowercase {
						alphabetSize += 26
					}
					for _, w := range words {
						if acceptanceRgx.MatchString(w) {
							cws, bits := h.EncodeGreedy(w, nGramN)
							var _ = cws
							//_, _ = fmt.Fprintf(os.Stdout, "%d\t%s\t%v\n", bits, w, cws)
							_, _ = fmt.Fprintf(os.Stdout, "%d\t%s\n", bits, w)
							totalBits += bits
							totalCharacters += len(w)
						}
					}
					binaryEncodingBitsPerCharacter := math.Log2(float64(alphabetSize))
					meanBitsPerCharacter := float64(totalBits) / float64(totalCharacters)
					log.Info().Float64("62BitReach", 62.0/meanBitsPerCharacter).Bool("lowercase", lowercase).Str("encoding", encoding).Int("nGramN", nGramN).Float64("binaryEncodingBitsPerCharacter", binaryEncodingBitsPerCharacter).Int("alphabetSize", alphabetSize).Int("totalBits", totalBits).Int("totalCharacters", totalCharacters).Float64("meanBitsPerCharacter", meanBitsPerCharacter).Msg("statistics")
				}
				break
			}

			return nil
		},
	}
}

// splitCamelCase splits src at transitions between rune classes
// (lower / upper / digit / other), folding the trailing rune of an all-caps run
// onto a following lower-case run so e.g. "PDFLoader" -> ["PDF", "Loader"].
// Inlined from github.com/fatih/camelcase (MIT) to drop the third-party dep;
// see THIRD_PARTY_NOTICES.
func splitCamelCase(src string) (entries []string) {
	if !utf8.ValidString(src) {
		return []string{src}
	}
	entries = make([]string, 0, 4)
	var runs [][]rune
	lastClass := 0
	for _, r := range src {
		var class int
		switch {
		case unicode.IsLower(r):
			class = 1
		case unicode.IsUpper(r):
			class = 2
		case unicode.IsDigit(r):
			class = 3
		default:
			class = 4
		}
		if class == lastClass {
			runs[len(runs)-1] = append(runs[len(runs)-1], r)
		} else {
			runs = append(runs, []rune{r})
		}
		lastClass = class
	}
	// Move a trailing upper-case rune onto the next run when it begins a
	// lower-case word (acronym boundary).
	for i := 0; i < len(runs)-1; i++ {
		if unicode.IsUpper(runs[i][0]) && unicode.IsLower(runs[i+1][0]) {
			last := len(runs[i]) - 1
			runs[i+1] = append([]rune{runs[i][last]}, runs[i+1]...)
			runs[i] = runs[i][:last]
		}
	}
	for _, s := range runs {
		if len(s) > 0 {
			entries = append(entries, string(s))
		}
	}
	return
}
