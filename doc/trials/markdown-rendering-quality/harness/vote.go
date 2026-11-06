//go:build integration

package mdquality

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math/big"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
)

// voteArms are the arms the person judges blind, pairwise, per document
// (README §4): the native widget against each stylesheet under one engine,
// and the two reference stylesheets against each other.
var votePairs = [][2]string{
	{"native", "ids-chromium"},
	{"native", "obsidian-chromium"},
	{"native", "github-chromium"},
	{"ids-chromium", "github-chromium"},
	// The typst arms, when the run made them: the native arm against the
	// same type spec set by typst, and against typst's book typography.
	{"native", "typst-ids"},
	{"native", "typst-book"},
}

// voteKey is what vote/key.json holds: the salt, and which picture is which
// arm. The voter does not open it; tally does.
type voteKey struct {
	Salt   string               `json:"salt"`
	Images map[string]voteImage `json:"images"`
}

type voteImage struct {
	Doc string `json:"doc"`
	Arm string `json:"arm"`
}

type votePair struct {
	P   int    `json:"p"`
	Doc string `json:"doc"`
	A   string `json:"a"`
	B   string `json:"b"`
}

// vote builds work/vote/: the pictures under opaque names, key.json, and
// vote.html, a page that shows one pair at a time and exports votes.json.
func vote(work string) (err error) {
	dir := filepath.Join(work, "vote")
	if err = os.MkdirAll(filepath.Join(dir, "img"), 0o755); err != nil {
		return eb.Build().Str("dir", dir).Errorf("unable to create the vote directory: %w", err)
	}
	saltB := make([]byte, 8)
	if _, err = rand.Read(saltB); err != nil {
		return eh.Errorf("unable to draw a salt: %w", err)
	}
	key := voteKey{Salt: hex.EncodeToString(saltB), Images: map[string]voteImage{}}
	id := func(doc, arm string) (s string) {
		h := sha256.Sum256([]byte(key.Salt + "/" + doc + "/" + arm))
		s = hex.EncodeToString(h[:])[:12]
		if _, done := key.Images[s]; done {
			return
		}
		key.Images[s] = voteImage{Doc: doc, Arm: arm}
		return
	}
	var pairs []votePair
	for _, e := range corpus {
		for _, p := range votePairs {
			if _, statErr := os.Stat(filepath.Join(work, "captures", e.Slug+"-"+p[1]+".crop.png")); statErr != nil {
				continue // an arm this run did not capture
			}
			a, b := id(e.Slug, p[0]), id(e.Slug, p[1])
			if flip, _ := rand.Int(rand.Reader, big.NewInt(2)); flip.Int64() == 1 {
				a, b = b, a
			}
			pairs = append(pairs, votePair{Doc: e.Slug, A: a, B: b})
		}
	}
	// shuffle the pairs so neighbours do not share a document
	for i := len(pairs) - 1; i > 0; i-- {
		j, _ := rand.Int(rand.Reader, big.NewInt(int64(i+1)))
		pairs[i], pairs[j.Int64()] = pairs[j.Int64()], pairs[i]
	}
	for i := range pairs {
		pairs[i].P = i + 1
	}
	for s, im := range key.Images {
		src := filepath.Join(work, "captures", im.Doc+"-"+im.Arm+".crop.png")
		var b []byte
		if b, err = os.ReadFile(src); err != nil {
			return eb.Build().Str("path", src).Errorf("unable to read a cropped capture: %w", err)
		}
		if err = os.WriteFile(filepath.Join(dir, "img", s+".png"), b, 0o644); err != nil {
			return eh.Errorf("unable to write a vote picture: %w", err)
		}
	}
	var kb []byte
	if kb, err = json.MarshalIndent(key, "", "  "); err != nil {
		return eh.Errorf("unable to encode the vote key: %w", err)
	}
	if err = os.WriteFile(filepath.Join(dir, "key.json"), kb, 0o600); err != nil {
		return eh.Errorf("unable to write the vote key: %w", err)
	}
	var pb []byte
	if pb, err = json.Marshal(pairs); err != nil {
		return eh.Errorf("unable to encode the pairs: %w", err)
	}
	html := strings.Replace(votePage, "/*PAIRS*/", string(pb), 1)
	html = strings.Replace(html, "/*SALT*/", key.Salt, 1)
	if err = os.WriteFile(filepath.Join(dir, "vote.html"), []byte(html), 0o644); err != nil {
		return eh.Errorf("unable to write the vote page: %w", err)
	}
	return
}

// ballot is one line of votes.json as the page exports it.
type ballot struct {
	P      int    `json:"p"`
	Doc    string `json:"doc"`
	A      string `json:"a"`
	B      string `json:"b"`
	Choice string `json:"choice"` // "a", "b" or "tie"
	Note   string `json:"note"`
}

// tally joins work/vote/votes.json with key.json and writes runDir/votes.md
// and runDir/votes.json with the arms revealed.
func tally(work, runDir string) (err error) {
	dir := filepath.Join(work, "vote")
	var kb, vb []byte
	if kb, err = os.ReadFile(filepath.Join(dir, "key.json")); err != nil {
		return eh.Errorf("unable to read the vote key: %w", err)
	}
	if vb, err = os.ReadFile(filepath.Join(dir, "votes.json")); err != nil {
		return eh.Errorf("unable to read votes.json (export it from vote.html into the vote directory): %w", err)
	}
	var key voteKey
	var votes []ballot
	if err = json.Unmarshal(kb, &key); err != nil {
		return eh.Errorf("unable to decode the vote key: %w", err)
	}
	if err = json.Unmarshal(vb, &votes); err != nil {
		return eh.Errorf("unable to decode votes.json: %w", err)
	}
	type resolved struct {
		P      int    `json:"p"`
		Doc    string `json:"doc"`
		ArmA   string `json:"armA"`
		ArmB   string `json:"armB"`
		Winner string `json:"winner"`
		Note   string `json:"note,omitempty"`
	}
	var out []resolved
	wins := map[string]int{}
	losses := map[string]int{}
	ties := map[string]int{}
	head := map[string]map[string][3]int{} // native vs arm: wins, ties, losses for native
	for _, v := range votes {
		a, b := key.Images[v.A], key.Images[v.B]
		r := resolved{P: v.P, Doc: v.Doc, ArmA: a.Arm, ArmB: b.Arm, Note: v.Note}
		switch v.Choice {
		case "a":
			r.Winner = a.Arm
			wins[a.Arm]++
			losses[b.Arm]++
		case "b":
			r.Winner = b.Arm
			wins[b.Arm]++
			losses[a.Arm]++
		default:
			r.Winner = "tie"
			ties[a.Arm]++
			ties[b.Arm]++
		}
		pairKey, other := a.Arm, b.Arm
		if b.Arm < a.Arm {
			pairKey, other = b.Arm, a.Arm
		}
		if head[pairKey] == nil {
			head[pairKey] = map[string][3]int{}
		}
		c := head[pairKey][other]
		switch r.Winner {
		case pairKey:
			c[0]++
		case "tie":
			c[1]++
		default:
			c[2]++
		}
		head[pairKey][other] = c
		out = append(out, r)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].P < out[j].P })
	var ob []byte
	if ob, err = json.MarshalIndent(out, "", "  "); err != nil {
		return eh.Errorf("unable to encode the resolved votes: %w", err)
	}
	if err = os.WriteFile(filepath.Join(runDir, "votes.json"), ob, 0o644); err != nil {
		return eh.Errorf("unable to write votes.json: %w", err)
	}
	var b strings.Builder
	b.WriteString("---\ntype: reference\naudience: package maintainer\nstatus: draft\n---\n\n")
	b.WriteString("> **Status: draft — pre-human-review.** One voter, one run; the citable claim is the trial README's §0.\n\n")
	b.WriteString("# Blind pairwise votes\n\n")
	fmt.Fprintf(&b, "%d pairs voted. Each pair showed the same document at the same viewport, left/right drawn at random, arms hidden.\n\n", len(out))
	b.WriteString("## Head to head\n\n| pair | first wins | ties | second wins |\n| --- | ---: | ---: | ---: |\n")
	var keys []string
	for k := range head {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		var others []string
		for o := range head[k] {
			others = append(others, o)
		}
		sort.Strings(others)
		for _, o := range others {
			c := head[k][o]
			fmt.Fprintf(&b, "| %s vs %s | %d | %d | %d |\n", k, o, c[0], c[1], c[2])
		}
	}
	b.WriteString("\n## Per arm\n\n| arm | wins | ties | losses |\n| --- | ---: | ---: | ---: |\n")
	var arms []string
	seen := map[string]bool{}
	for _, im := range key.Images {
		if !seen[im.Arm] {
			seen[im.Arm] = true
			arms = append(arms, im.Arm)
		}
	}
	sort.Strings(arms)
	for _, a := range arms {
		fmt.Fprintf(&b, "| %s | %d | %d | %d |\n", a, wins[a], ties[a], losses[a])
	}
	b.WriteString("\n## Ballots\n\n| # | document | A | B | winner | note |\n| ---: | --- | --- | --- | --- | --- |\n")
	for _, r := range out {
		fmt.Fprintf(&b, "| %d | %s | %s | %s | %s | %s |\n", r.P, r.Doc, r.ArmA, r.ArmB, r.Winner, strings.ReplaceAll(r.Note, "|", "\\|"))
	}
	if err = os.WriteFile(filepath.Join(runDir, "votes.md"), []byte(b.String()), 0o644); err != nil {
		return eh.Errorf("unable to write votes.md: %w", err)
	}
	return
}

const votePage = `<!DOCTYPE html>
<html lang="en"><head><meta charset="utf-8"><title>Blind pairwise vote</title>
<style>
body { margin: 0; font: 14px/1.4 system-ui, sans-serif; background: #202020; color: #ddd; }
header { padding: 10px 16px; display: flex; gap: 16px; align-items: center; flex-wrap: wrap; }
header b { color: #fff; }
.pair { display: flex; gap: 8px; padding: 0 8px; }
.pair figure { margin: 0; flex: 1 1 0; min-width: 0; background: #111; }
.pair figcaption { padding: 4px 8px; color: #aaa; }
.pair img { width: 100%; height: auto; display: block; cursor: zoom-in; }
.pair img.full { width: auto; max-width: none; cursor: zoom-out; }
.controls { padding: 10px 16px; display: flex; gap: 10px; align-items: center; flex-wrap: wrap; }
button { font: inherit; padding: 8px 14px; background: #3a3a3a; color: #fff; border: 1px solid #666; border-radius: 4px; cursor: pointer; }
button.on { background: #2a6; border-color: #2a6; }
textarea { font: inherit; width: 40em; max-width: 100%; height: 2.6em; background: #111; color: #ddd; border: 1px solid #555; }
a { color: #8cf; }
</style></head><body>
<header>
  <b>Which would you rather read?</b>
  <span>Same document, same window width. Keys: <kbd>A</kbd> left, <kbd>D</kbd> right, <kbd>S</kbd> tie, <kbd>←</kbd> <kbd>→</kbd> move.</span>
  <span id="pos"></span>
  <a id="dl" href="#" download="votes.json">Download votes.json</a>
</header>
<div class="controls">
  <button id="va">Left (A)</button>
  <button id="vs">Tie (S)</button>
  <button id="vb">Right (D)</button>
  <textarea id="note" placeholder="What decided it? (optional)"></textarea>
  <button id="prev">←</button><button id="next">→</button>
</div>
<div class="pair">
  <figure><figcaption>Left</figcaption><img id="ia" alt="left"></figure>
  <figure><figcaption>Right</figcaption><img id="ib" alt="right"></figure>
</div>
<script>
var pairs = /*PAIRS*/;
var salt = "/*SALT*/";
var storeKey = "mdq-votes-" + salt;
var votes = {};
try { votes = JSON.parse(localStorage.getItem(storeKey) || "{}"); } catch (e) { votes = {}; }
var i = 0;
function save() { try { localStorage.setItem(storeKey, JSON.stringify(votes)); } catch (e) {} refreshLink(); }
function show() {
  var p = pairs[i];
  document.getElementById("ia").src = "img/" + p.a + ".png";
  document.getElementById("ib").src = "img/" + p.b + ".png";
  document.getElementById("ia").className = ""; document.getElementById("ib").className = "";
  var v = votes[p.p] || {};
  document.getElementById("note").value = v.note || "";
  ["va","vs","vb"].forEach(function (id) { document.getElementById(id).className = ""; });
  if (v.choice === "a") document.getElementById("va").className = "on";
  if (v.choice === "b") document.getElementById("vb").className = "on";
  if (v.choice === "tie") document.getElementById("vs").className = "on";
  var done = Object.keys(votes).length;
  document.getElementById("pos").textContent = "pair " + (i + 1) + " of " + pairs.length + " — " + p.doc + " — " + done + " voted";
  window.scrollTo(0, 0);
}
function cast(choice) {
  var p = pairs[i];
  votes[p.p] = { p: p.p, doc: p.doc, a: p.a, b: p.b, choice: choice, note: document.getElementById("note").value };
  save();
  if (i + 1 < pairs.length) { i++; show(); } else { show(); }
}
function refreshLink() {
  var list = Object.keys(votes).map(function (k) { return votes[k]; }).sort(function (x, y) { return x.p - y.p; });
  var blob = new Blob([JSON.stringify(list, null, 2)], { type: "application/json" });
  document.getElementById("dl").href = URL.createObjectURL(blob);
}
document.getElementById("va").onclick = function () { cast("a"); };
document.getElementById("vb").onclick = function () { cast("b"); };
document.getElementById("vs").onclick = function () { cast("tie"); };
document.getElementById("prev").onclick = function () { if (i > 0) { i--; show(); } };
document.getElementById("next").onclick = function () { if (i + 1 < pairs.length) { i++; show(); } };
document.getElementById("note").onchange = function () { var p = pairs[i]; if (votes[p.p]) { votes[p.p].note = this.value; save(); } };
["ia","ib"].forEach(function (id) { document.getElementById(id).onclick = function () { this.className = this.className ? "" : "full"; }; });
document.addEventListener("keydown", function (e) {
  if (e.target.tagName === "TEXTAREA") return;
  if (e.key === "a" || e.key === "A") cast("a");
  else if (e.key === "d" || e.key === "D") cast("b");
  else if (e.key === "s" || e.key === "S") cast("tie");
  else if (e.key === "ArrowLeft") { if (i > 0) { i--; show(); } }
  else if (e.key === "ArrowRight") { if (i + 1 < pairs.length) { i++; show(); } }
});
refreshLink();
show();
</script>
</body></html>
`
