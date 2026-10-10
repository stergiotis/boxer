// sqlapplet_origin.go loads one applet document a browser tab fetches from its
// page's origin at start (ADR-0299): a document that is not in any
// committed book, admitted through the same parser the books and the runtime
// store go through, and only when its buffer is read-class.

package sqlapplet

import (
	"context"
	"io"
	"net/http"
	"net/url"
	"path"
	"strings"

	"github.com/stergiotis/boxer/public/config/env"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass/analysis"
	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
)

// TabDoc names the applet document a tab fetches from its page's origin and
// mounts in place of its default app (ADR-0299 §SD1).
var TabDoc = env.NewString(env.Spec{
	Name:        "BOXER_SQLAPPLET_TAB_DOC",
	Description: "a browser tab fetches this applet document from its page's origin and mounts it (ADR-0299): a path relative to the page, such as applets/x.md, or absolute on its origin, such as /applets/x.md; its base name is the slug, and only a read-class buffer is admitted",
	Category:    env.CategoryE("boxer-sqlapplet"),
})

// originBookID is the book id an origin-loaded definition carries, which the
// definition drawer and the logs show as its provenance.
const originBookID = "origin"

// maxOriginDocBytes bounds the document a tab reads; the committed books'
// largest is a few kilobytes.
const maxOriginDocBytes = 1 << 20

// LoadTabApplet fetches the document [TabDoc] names, resolved against base —
// the URL of the directory the page is served from — mints it into
// the default registry and returns its app id; empty when [TabDoc] is unset.
// It is a tab binary's prepare step (tabhost.Options.Prepare), run after the
// tab's HTTP transport is installed and after [MintManifests], so a committed
// applet of the same slug is already registered and wins.
func LoadTabApplet(ctx context.Context, base string) (id app.AppIdT, err error) {
	p := TabDoc.Get()
	if p == "" {
		return
	}
	return loadOriginApplet(ctx, app.DefaultRegistry, http.DefaultClient, base, p)
}

// loadOriginApplet is LoadTabApplet against an explicit registry and client.
func loadOriginApplet(ctx context.Context, reg *app.Registry, client *http.Client, base string, docPath string) (id app.AppIdT, err error) {
	target, err := originDocURL(base, docPath)
	if err != nil {
		return
	}
	src, err := fetchOriginDoc(ctx, client, target)
	if err != nil {
		return
	}
	def, err := ParseDocSource(originBookID, path.Base(target.Path), src)
	if err != nil {
		return
	}
	if def == nil {
		err = eb.Build().Str("url", target.String()).Errorf("sqlapplet: the document carries no sql fence — not an applet")
		return
	}
	// §SD3: no review stands behind this document, so nothing reaching past
	// the endpoint or writing to it is admitted, whether or not it would run.
	if def.Class != analysis.QuerySecurityRead {
		err = eb.Build().Str("url", target.String()).Str("class", def.Class.String()).Errorf("sqlapplet: a document loaded from the origin must be read-class")
		return
	}
	if _, taken := reg.LookupManifest(app.AppIdT(appletIdPrefix + def.Slug)); taken {
		err = eb.Build().Str("slug", def.Slug).Errorf("sqlapplet: the slug is a committed applet's; rename the document")
		return
	}
	// ADR-0158 §SD7: a document outside any book inherits the store's default
	// unless it classified itself.
	if len(def.Topics) == 0 {
		def.Topics = storeDefaultTopics
	}
	m := manifestFor(def, nil)
	if err = m.Validate(); err != nil {
		err = eb.Build().Str("slug", def.Slug).Errorf("sqlapplet: manifest: %w", err)
		return
	}
	if err = reg.RegisterFactory(m, func() (a app.AppI, ctorErr error) {
		a = &appletApp{def: def, m: m}
		return
	}); err != nil {
		err = eb.Build().Str("slug", def.Slug).Errorf("sqlapplet: register: %w", err)
		return
	}
	id = m.Id
	return
}

// originDocURL resolves docPath against the page's base URL and refuses
// anything that would leave the base's origin (§SD2): docPath is a path —
// relative to the page or absolute on its origin — never a URL or a
// scheme-relative reference; it carries no query or fragment, and ends in .md.
func originDocURL(pageBase string, docPath string) (target *url.URL, err error) {
	base, err := url.Parse(pageBase)
	if err != nil || (base.Scheme != "http" && base.Scheme != "https") || base.Host == "" {
		err = eb.Build().Str("base", pageBase).Errorf("sqlapplet: the page base is not an http(s) URL")
		return
	}
	if docPath == "" || strings.HasPrefix(docPath, "//") || strings.Contains(docPath, `\`) {
		err = eb.Build().Str("path", docPath).Errorf("sqlapplet: the document must be named by a path on the page's origin")
		return
	}
	ref, err := url.Parse(docPath)
	if err != nil {
		err = eb.Build().Str("path", docPath).Errorf("sqlapplet: the document path does not parse: %w", err)
		return
	}
	if ref.Scheme != "" || ref.Host != "" || ref.Opaque != "" {
		err = eb.Build().Str("path", docPath).Errorf("sqlapplet: the document must be named by a path, not a URL")
		return
	}
	if ref.RawQuery != "" || ref.Fragment != "" {
		err = eb.Build().Str("path", docPath).Errorf("sqlapplet: the document path carries a query or a fragment")
		return
	}
	target = base.ResolveReference(ref)
	if target.Scheme != base.Scheme || target.Host != base.Host {
		err = eb.Build().Str("path", docPath).Errorf("sqlapplet: the document path leaves the page's origin")
		return
	}
	if !strings.HasSuffix(target.Path, ".md") {
		err = eb.Build().Str("path", docPath).Errorf("sqlapplet: the document must be a .md file")
		return
	}
	return
}

func fetchOriginDoc(ctx context.Context, client *http.Client, target *url.URL) (src []byte, err error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target.String(), nil)
	if err != nil {
		err = eh.Errorf("sqlapplet: origin document request: %w", err)
		return
	}
	resp, err := client.Do(req)
	if err != nil {
		err = eb.Build().Str("url", target.String()).Errorf("sqlapplet: fetch the origin document: %w", err)
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		err = eb.Build().Str("url", target.String()).Int("status", resp.StatusCode).Errorf("sqlapplet: the origin document was not served")
		return
	}
	src, err = io.ReadAll(io.LimitReader(resp.Body, maxOriginDocBytes+1))
	if err != nil {
		err = eb.Build().Str("url", target.String()).Errorf("sqlapplet: read the origin document: %w", err)
		return
	}
	if len(src) > maxOriginDocBytes {
		err = eb.Build().Str("url", target.String()).Int("limit", maxOriginDocBytes).Errorf("sqlapplet: the origin document exceeds the size limit")
		src = nil
	}
	return
}
