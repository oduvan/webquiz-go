package explanations

import (
	"bytes"
	"html"
	"regexp"
	"strings"
	"sync"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/extension"
	extast "github.com/yuin/goldmark/extension/ast"
	"github.com/yuin/goldmark/parser"
	"github.com/yuin/goldmark/renderer"
	gmhtml "github.com/yuin/goldmark/renderer/html"
	"github.com/yuin/goldmark/text"
	"github.com/yuin/goldmark/util"

	"github.com/oduvan/webquiz-go/internal/pyval"
)

// harmfulLink is what mistune puts in place of a URL it does not allow.
const harmfulLink = "#harmful-link"

// safeProtocols and goodDataProtocols are mistune's HTMLRenderer.SAFE_PROTOCOLS
// and GOOD_DATA_PROTOCOLS.
var (
	safeProtocols     = []string{"http:", "https:", "mailto:", "tel:", "ftp:", "ftps:", "irc:", "ircs:"}
	goodDataProtocols = []string{"data:image/gif;", "data:image/png;", "data:image/jpeg;", "data:image/webp;"}
)

// markdowns keeps one converter per images URL; goldmark converters may be
// used from many goroutines.
var markdowns sync.Map // images URL -> goldmark.Markdown

// Render turns explanation Markdown into safe HTML. Image paths that start
// with "/imgs/" start with imagesURL instead (StudentImagesURL or AdminImagesURL).
func Render(markdown, imagesURL string) string {
	converter, ok := markdowns.Load(imagesURL)
	if !ok {
		converter, _ = markdowns.LoadOrStore(imagesURL, newMarkdown(imagesURL))
	}
	var out bytes.Buffer
	if err := converter.(goldmark.Markdown).Convert([]byte(markdown), &out); err != nil {
		// goldmark only fails when the writer fails; a bytes.Buffer does not.
		return "<p>" + escapeText(markdown) + "</p>\n"
	}
	return out.String()
}

func newMarkdown(imagesURL string) goldmark.Markdown {
	return goldmark.New(
		goldmark.WithExtensions(
			extension.NewTable(extension.WithTableCellAlignMethod(extension.TableCellAlignStyle)),
			doubleTildeStrikethrough{},
		),
		goldmark.WithRendererOptions(
			gmhtml.WithXHTML(),              // "<br />", "<hr />", "<img ... />" as mistune writes them
			gmhtml.WithWriter(textWriter{}), // entities stay as written, as in mistune
			renderer.WithNodeRenderers(util.Prioritized(&nodeRenderer{imagesURL: imagesURL}, 100)),
		),
	)
}

// nodeRenderer renders the nodes where WebQuiz differs from plain Markdown:
// links, images and raw HTML. A priority below the default HTML renderer's
// (1000) makes these functions replace the default ones.
type nodeRenderer struct {
	imagesURL string
}

func (r *nodeRenderer) RegisterFuncs(reg renderer.NodeRendererFuncRegisterer) {
	reg.Register(ast.KindLink, r.renderLink)
	reg.Register(ast.KindAutoLink, r.renderAutoLink)
	reg.Register(ast.KindImage, r.renderImage)
	reg.Register(ast.KindRawHTML, r.renderRawHTML)
	reg.Register(ast.KindHTMLBlock, r.renderHTMLBlock)
}

// renderLink writes a link that opens in a new tab, or only its text when the
// URL is harmful.
func (r *nodeRenderer) renderLink(w util.BufWriter, source []byte, node ast.Node, entering bool) (ast.WalkStatus, error) {
	n := node.(*ast.Link)
	href := sanitizeURL(escapeURL(string(n.Destination)))
	if href == harmfulLink {
		return ast.WalkContinue, nil // the children (the link text) are written as usual
	}
	if !entering {
		_, _ = w.WriteString("</a>")
		return ast.WalkContinue, nil
	}
	_, _ = w.WriteString(`<a href="` + escapeAttribute(href) + `" target="_blank" rel="noopener"`)
	if title := linkTitle(n.Title); title != "" {
		_, _ = w.WriteString(` title="` + escapeAttribute(title) + `"`)
	}
	_ = w.WriteByte('>')
	return ast.WalkContinue, nil
}

// renderAutoLink writes <https://...> and <name@host> links like renderLink.
func (r *nodeRenderer) renderAutoLink(w util.BufWriter, source []byte, node ast.Node, entering bool) (ast.WalkStatus, error) {
	if !entering {
		return ast.WalkContinue, nil
	}
	n := node.(*ast.AutoLink)
	url := string(n.URL(source))
	label := escapeText(string(n.Label(source)))
	if n.AutoLinkType == ast.AutoLinkEmail && !strings.HasPrefix(strings.ToLower(url), "mailto:") {
		url = "mailto:" + url
	}
	href := sanitizeURL(escapeURL(url))
	if href == harmfulLink {
		_, _ = w.WriteString(label)
		return ast.WalkSkipChildren, nil
	}
	_, _ = w.WriteString(`<a href="` + escapeAttribute(href) + `" target="_blank" rel="noopener">` + label + "</a>")
	return ast.WalkSkipChildren, nil
}

// renderImage writes an image; "/imgs/" paths point to the page's image folder.
func (r *nodeRenderer) renderImage(w util.BufWriter, source []byte, node ast.Node, entering bool) (ast.WalkStatus, error) {
	if !entering {
		return ast.WalkContinue, nil
	}
	n := node.(*ast.Image)
	url := escapeURL(string(n.Destination))
	if strings.HasPrefix(url, ImagesPath) {
		url = r.imagesURL + url[len(ImagesPath):]
	}
	_, _ = w.WriteString(`<img src="` + escapeAttribute(sanitizeURL(url)) + `" alt="` + escapeAttribute(altText(n, source)) + `"`)
	if title := linkTitle(n.Title); title != "" {
		_, _ = w.WriteString(` title="` + escapeAttribute(title) + `"`)
	}
	_, _ = w.WriteString(" />")
	return ast.WalkSkipChildren, nil
}

// renderRawHTML writes inline HTML as text.
func (r *nodeRenderer) renderRawHTML(w util.BufWriter, source []byte, node ast.Node, entering bool) (ast.WalkStatus, error) {
	if !entering {
		return ast.WalkContinue, nil
	}
	n := node.(*ast.RawHTML)
	for i := 0; i < n.Segments.Len(); i++ {
		segment := n.Segments.At(i)
		writeEscaped(w, segment.Value(source))
	}
	return ast.WalkSkipChildren, nil
}

// renderHTMLBlock writes an HTML block as a paragraph of text, like mistune's
// block_html with escape=True.
func (r *nodeRenderer) renderHTMLBlock(w util.BufWriter, source []byte, node ast.Node, entering bool) (ast.WalkStatus, error) {
	if !entering {
		return ast.WalkContinue, nil
	}
	n := node.(*ast.HTMLBlock)
	var raw bytes.Buffer
	for i := 0; i < n.Lines().Len(); i++ {
		line := n.Lines().At(i)
		raw.Write(line.Value(source))
	}
	if n.HasClosure() {
		raw.Write(n.ClosureLine.Value(source))
	}
	_, _ = w.WriteString("<p>")
	writeEscaped(w, []byte(pyval.Strip(raw.String())))
	_, _ = w.WriteString("</p>\n")
	return ast.WalkSkipChildren, nil
}

// altText is the plain text of an image description: the text of its
// children, with raw HTML kept as text (mistune strips the tags it rendered).
func altText(n ast.Node, source []byte) string {
	var b strings.Builder
	var walk func(ast.Node)
	walk = func(node ast.Node) {
		for c := node.FirstChild(); c != nil; c = c.NextSibling() {
			switch child := c.(type) {
			case *ast.Text:
				value := child.Segment.Value(source)
				if child.IsRaw() {
					b.Write(value)
				} else {
					b.Write(unescapePunctuation(value))
				}
				if child.SoftLineBreak() || child.HardLineBreak() {
					b.WriteByte('\n')
				}
			case *ast.String:
				b.Write(child.Value)
			case *ast.RawHTML:
				for i := 0; i < child.Segments.Len(); i++ {
					segment := child.Segments.At(i)
					b.Write(segment.Value(source))
				}
			case *ast.CodeSpan:
				for t := child.FirstChild(); t != nil; t = t.NextSibling() {
					if textNode, ok := t.(*ast.Text); ok {
						b.Write(textNode.Segment.Value(source))
					}
				}
			case *ast.AutoLink:
				b.Write(child.Label(source))
			default:
				walk(c)
			}
		}
	}
	walk(n)
	return b.String()
}

// linkTitle reads a link title like mistune: backslash escapes are removed.
func linkTitle(title []byte) string {
	return string(unescapePunctuation(title))
}

// unescapePunctuation removes the backslash before ASCII punctuation.
func unescapePunctuation(value []byte) []byte {
	return util.UnescapePunctuations(value)
}

// escapeURL is mistune's escape_url: backslash escapes and entities are
// resolved, then everything but ASCII letters, digits, "_.-~" and
// ":/?#@!$&()*+,;=%" is percent-encoded.
func escapeURL(url string) string {
	unescaped := unescapeEntities(string(unescapePunctuation([]byte(url))))
	var b strings.Builder
	for i := 0; i < len(unescaped); i++ {
		c := unescaped[i]
		if isURLSafe(c) {
			b.WriteByte(c)
		} else {
			b.WriteString("%")
			b.WriteString(strings.ToUpper(hexByte(c)))
		}
	}
	return b.String()
}

func isURLSafe(c byte) bool {
	switch {
	case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9':
		return true
	}
	return strings.IndexByte("_.-~:/?#@!$&()*+,;=%", c) >= 0
}

func hexByte(c byte) string {
	const digits = "0123456789abcdef"
	return string([]byte{digits[c>>4], digits[c&15]})
}

// sanitizeURL is mistune's sanitize_url: a URL that is not allowed becomes
// "#harmful-link".
func sanitizeURL(url string) string {
	normalized := strings.ToLower(strings.TrimLeftFunc(unquoteURL(url), pyval.IsSpace))
	if isSafeURL(normalized) {
		return url
	}
	return harmfulLink
}

func isSafeURL(url string) bool {
	for _, prefix := range safeProtocols {
		if strings.HasPrefix(url, prefix) {
			return true
		}
	}
	for _, prefix := range goodDataProtocols {
		if strings.HasPrefix(url, prefix) {
			return true
		}
	}
	if strings.HasPrefix(url, "/") || strings.HasPrefix(url, "#") || strings.HasPrefix(url, "?") {
		return true
	}
	firstPart, _, _ := strings.Cut(url, "/")
	return !strings.Contains(firstPart, ":")
}

// unquoteURL decodes percent escapes up to three times, like mistune's
// _unquote_url, so "%6Aavascript:" is seen as "javascript:".
func unquoteURL(url string) string {
	for range 3 {
		decoded := percentDecode(url)
		if decoded == url {
			break
		}
		url = decoded
	}
	return url
}

// percentDecode is Python's urllib.parse.unquote: "%XX" becomes a byte, other
// "%" signs stay.
func percentDecode(s string) string {
	if !strings.Contains(s, "%") {
		return s
	}
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] == '%' && i+2 < len(s) && isHex(s[i+1]) && isHex(s[i+2]) {
			b.WriteByte(unhex(s[i+1])<<4 | unhex(s[i+2]))
			i += 2
			continue
		}
		b.WriteByte(s[i])
	}
	return b.String()
}

func isHex(c byte) bool {
	return (c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')
}

func unhex(c byte) byte {
	switch {
	case c >= '0' && c <= '9':
		return c - '0'
	case c >= 'a' && c <= 'f':
		return c - 'a' + 10
	}
	return c - 'A' + 10
}

// entityPattern is mistune's _charref_re: entities need the final ";".
var entityPattern = regexp.MustCompile(`&(#[0-9]{1,7};|#[xX][0-9a-fA-F]+;|[^\t\n\f <&#;]{1,32};)`)

// unescapeEntities is mistune's unescape.
func unescapeEntities(s string) string {
	if !strings.Contains(s, "&") {
		return s
	}
	return entityPattern.ReplaceAllStringFunc(s, html.UnescapeString)
}

// escapeText is mistune's escape: &, <, > and " become entities.
func escapeText(s string) string {
	return textEscaper.Replace(s)
}

var textEscaper = strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", `"`, "&quot;")

// escapeAttribute is mistune's safe_entity, used for attribute values:
// entities are resolved, then the text is escaped.
func escapeAttribute(s string) string {
	return escapeText(unescapeEntities(s))
}

// writeEscaped writes text with &, <, > and " escaped and NUL replaced.
func writeEscaped(w util.BufWriter, value []byte) {
	start := 0
	for i, c := range value {
		var replacement string
		switch c {
		case '&':
			replacement = "&amp;"
		case '<':
			replacement = "&lt;"
		case '>':
			replacement = "&gt;"
		case '"':
			replacement = "&quot;"
		case 0:
			replacement = "\xef\xbf\xbd" // U+FFFD
		default:
			continue
		}
		_, _ = w.Write(value[start:i])
		_, _ = w.WriteString(replacement)
		start = i + 1
	}
	_, _ = w.Write(value[start:])
}

// textWriter writes Markdown text like mistune with escape=True: backslash
// escapes are removed, but entities like "&copy;" stay as written (goldmark's
// default writer would turn them into characters).
type textWriter struct{}

func (textWriter) Write(w util.BufWriter, source []byte) {
	writeEscaped(w, unescapePunctuation(source))
}

func (textWriter) RawWrite(w util.BufWriter, source []byte) {
	writeEscaped(w, source)
}

func (textWriter) SecureWrite(w util.BufWriter, source []byte) {
	_, _ = w.Write(bytes.ReplaceAll(source, []byte{0}, []byte("\xef\xbf\xbd")))
}

// doubleTildeStrikethrough is goldmark's strikethrough with mistune's rule:
// only "~~text~~" strikes through, a single "~" stays text.
type doubleTildeStrikethrough struct{}

func (doubleTildeStrikethrough) Extend(m goldmark.Markdown) {
	m.Parser().AddOptions(parser.WithInlineParsers(util.Prioritized(doubleTildeParser{}, 500)))
	m.Renderer().AddOptions(renderer.WithNodeRenderers(util.Prioritized(extension.NewStrikethroughHTMLRenderer(), 500)))
}

type doubleTildeParser struct{}

func (doubleTildeParser) Trigger() []byte { return []byte{'~'} }

func (doubleTildeParser) Parse(parent ast.Node, block text.Reader, pc parser.Context) ast.Node {
	before := block.PrecendingCharacter()
	line, segment := block.PeekLine()
	node := parser.ScanDelimiter(line, before, 1, tildeDelimiters{})
	if node == nil || node.OriginalLength != 2 || before == '~' {
		return nil
	}
	node.Segment = segment.WithStop(segment.Start + node.OriginalLength)
	block.Advance(node.OriginalLength)
	pc.PushDelimiter(node)
	return node
}

func (doubleTildeParser) CloseBlock(parent ast.Node, pc parser.Context) {}

type tildeDelimiters struct{}

func (tildeDelimiters) IsDelimiter(b byte) bool { return b == '~' }

func (tildeDelimiters) CanOpenCloser(opener, closer *parser.Delimiter) bool {
	return opener.Char == closer.Char
}

func (tildeDelimiters) OnMatch(consumes int) ast.Node { return extast.NewStrikethrough() }
