package judge

import (
	"bufio"
	"encoding/json/v2"
	"os"
	"strconv"
	"strings"

	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
)

// A judge sheet puts a reader — an agent or a person — where the model
// stands (ADR-0257, proposed, §SD10): the picture, the scenario's intent and
// the questions, and nothing else. The reader's replies come back as lines of
// an answers file and are checked by VerdictOf, as a model's are.

// SheetMarkdown is the sheet a reader is given. It carries what the model is
// given and no more: no expected answer, no answer SQL, no scenario prose, no
// candidate. image is the picture's file name beside the sheet.
func SheetMarkdown(sheet string, image string, context string, qs []Question) string {
	var b strings.Builder
	b.WriteString("# Sheet " + sheet + "\n\n")
	b.WriteString("![sheet " + sheet + "](" + image + ")\n\n")
	b.WriteString("The picture, `" + image + "` beside this file, is one rendering of a dataset — a table, a chart or a diagram — cropped to the rendering itself.")
	if context != "" {
		b.WriteString(" What a reader wants from it: " + context)
	}
	b.WriteString("\n\n")
	b.WriteString("Answer each question from what the picture shows and from nothing else: not from files, not from earlier work, not from what you expect the data to be. " +
		"If the picture does not let you answer — the value is cut off, hidden, unlabelled or not shown — mark it unreadable instead of guessing.\n\n")
	b.WriteString("## Questions\n\n")
	for i, q := range qs {
		b.WriteString(strconv.Itoa(i+1) + ". `" + q.ID + "` — " + q.Prompt + "\n")
	}
	b.WriteString("\n## Reply\n\n")
	b.WriteString("One JSON object per question, one per line, appended to the answers file you were given. " +
		"Put each item of the answer in the array as a string: one element for a single value or name, several for a list. " +
		"Write numbers and names exactly as the picture shows them. `reader` is the label you were given.\n\n")
	b.WriteString("```json\n")
	for _, q := range qs {
		b.WriteString(`{"sheet": "` + sheet + `", "question": "` + q.ID + `", "answer": ["..."], "unreadable": false, "reader": "<label>"}` + "\n")
	}
	b.WriteString("```\n")
	return b.String()
}

// ReaderReply is one line of an answers file.
type ReaderReply struct {
	Sheet      string   `json:"sheet"`
	Question   string   `json:"question"`
	Answer     []string `json:"answer"`
	Unreadable bool     `json:"unreadable"`
	Reader     string   `json:"reader"`
}

// ReadReplies reads an answers file. A line that does not decode, or lacks a
// sheet, a question or a reader, is refused with its line number: a reply the
// harness cannot attribute would be scored against nobody.
func ReadReplies(path string) (rs []ReaderReply, err error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, eb.Build().Str("path", path).Errorf("unable to open the answers: %w", err)
	}
	defer func() { _ = f.Close() }()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	line := 0
	for sc.Scan() {
		line++
		text := strings.TrimSpace(sc.Text())
		if text == "" || strings.HasPrefix(text, "#") {
			continue
		}
		var r ReaderReply
		if err = json.Unmarshal([]byte(text), &r, json.RejectUnknownMembers(true)); err != nil {
			return nil, eb.Build().Int("line", line).Errorf("unable to decode a reply: %w", err)
		}
		if r.Sheet == "" || r.Question == "" || r.Reader == "" {
			return nil, eb.Build().Int("line", line).Errorf("a reply needs sheet, question and reader")
		}
		rs = append(rs, r)
	}
	if err = sc.Err(); err != nil {
		return nil, eh.Errorf("unable to read the answers: %w", err)
	}
	return rs, nil
}
