// geometry.js — runs inside each browser arm's page after fonts are ready and
// appends one JSON block to the body: the position of every visible glyph,
// with its font size, family, weight and colour, plus the page background.
// `--dump-dom` then carries it out; the harness reads it back by its id.
// The record per glyph is [left, top, width, height] in CSS pixels, or null
// for whitespace, so a word gap is counted the way the native SVG counts it.
// A text node inside a list item or a table cell is marked, so the line
// model can keep list and table rows out of the paragraph metrics as it
// does for the native arm's marker glyphs and split baselines.
(function () {
  function run() {
    var nodes = [];
    var it = document.createNodeIterator(document.body, NodeFilter.SHOW_TEXT);
    var n;
    var blocks = new Map();
    var r = document.createRange();
    while ((n = it.nextNode())) {
      var t = n.nodeValue;
      if (!t || !t.trim()) continue;
      var p = n.parentElement;
      if (!p || p.id === 'mdq-geom' || p.tagName === 'SCRIPT' || p.tagName === 'STYLE') continue;
      var cs = getComputedStyle(p);
      if (cs.display === 'none' || cs.visibility === 'hidden') continue;
      var chars = [];
      for (var i = 0; i < t.length; i++) {
        if (/\s/.test(t[i])) { chars.push(null); continue; }
        r.setStart(n, i);
        r.setEnd(n, i + 1);
        var b = r.getBoundingClientRect();
        if (b.width === 0 && b.height === 0) { chars.push(null); continue; }
        chars.push([+b.left.toFixed(2), +b.top.toFixed(2), +b.width.toFixed(2), +b.height.toFixed(2)]);
      }
      // The nearest block element, numbered in document order: what a
      // paragraph is, so the harness never joins two blocks' rows.
      var be = p.closest('p,li,td,th,h1,h2,h3,h4,h5,h6,blockquote,pre,dt,dd,summary,figcaption') || p;
      if (!blocks.has(be)) blocks.set(be, blocks.size);
      nodes.push({
        block: blocks.get(be),
        text: t,
        size: parseFloat(cs.fontSize),
        family: cs.fontFamily,
        weight: cs.fontWeight,
        color: cs.color,
        tag: p.tagName.toLowerCase(),
        list: !!p.closest('li'),
        cell: !!p.closest('td,th'),
        chars: chars
      });
    }
    var bg = getComputedStyle(document.body).backgroundColor;
    if (!bg || bg === 'rgba(0, 0, 0, 0)' || bg === 'transparent') {
      bg = getComputedStyle(document.documentElement).backgroundColor;
    }
    if (!bg || bg === 'rgba(0, 0, 0, 0)' || bg === 'transparent') {
      bg = 'rgb(255, 255, 255)'; // the browser's canvas, when no rule paints one
    }
    var s = document.createElement('script');
    s.type = 'application/json';
    s.id = 'mdq-geom';
    s.textContent = JSON.stringify({
      viewport: [window.innerWidth, window.innerHeight],
      docHeight: document.documentElement.scrollHeight,
      bg: bg,
      nodes: nodes
    });
    document.body.appendChild(s);
  }
  if (document.fonts && document.fonts.ready) {
    document.fonts.ready.then(run, run);
  } else {
    run();
  }
})();
