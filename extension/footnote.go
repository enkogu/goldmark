package extension

import (
	"bytes"
	"github.com/yuin/goldmark"
	gast "github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/extension/ast"
	"github.com/yuin/goldmark/parser"
	"github.com/yuin/goldmark/renderer"
	"github.com/yuin/goldmark/renderer/html"
	"github.com/yuin/goldmark/text"
	"github.com/yuin/goldmark/util"
	"strconv"
)

var footnoteListKey = parser.NewContextKey()

type footnoteBlockParser struct {
}

var defaultFootnoteBlockParser = &footnoteBlockParser{}

// NewFootnoteBlockParser returns a new parser.BlockParser that can parse
// footnotes of the Markdown(PHP Markdown Extra) text.
func NewFootnoteBlockParser() parser.BlockParser {
	return defaultFootnoteBlockParser
}

func (b *footnoteBlockParser) Open(parent gast.Node, reader text.Reader, pc parser.Context) (gast.Node, parser.State) {
	line, segment := reader.PeekLine()
	pos := pc.BlockOffset()
	if line[pos] != '[' {
		return nil, parser.NoChildren
	}
	pos++
	if pos > len(line)-1 || line[pos] != '^' {
		return nil, parser.NoChildren
	}
	open := pos + 1
	closes := -1
	closure := util.FindClosure(line[pos+1:], '[', ']', false, false)
	if closure > -1 {
		closes = pos + 1 + closure
		next := closes + 1
		if next >= len(line) || line[next] != ':' {
			return nil, parser.NoChildren
		}
	} else {
		return nil, parser.NoChildren
	}
	label := reader.Value(text.NewSegment(segment.Start+open, segment.Start+closes))
	if util.IsBlank(label) {
		return nil, parser.NoChildren
	}
	pos = pos + 2 + closes - open + 2
	childpos, padding := util.IndentPosition(line[pos:], pos, 1)
	reader.AdvanceAndSetPadding(pos+childpos, padding)
	item := ast.NewFootnote(label)
	return item, parser.HasChildren
}

func (b *footnoteBlockParser) Continue(node gast.Node, reader text.Reader, pc parser.Context) parser.State {
	line, _ := reader.PeekLine()
	if util.IsBlank(line) {
		return parser.Continue | parser.HasChildren
	}
	childpos, padding := util.IndentPosition(line, reader.LineOffset(), 4)
	if childpos < 0 {
		return parser.Close
	}
	reader.AdvanceAndSetPadding(childpos, padding)
	return parser.Continue | parser.HasChildren
}

func (b *footnoteBlockParser) Close(node gast.Node, reader text.Reader, pc parser.Context) {
	var list *ast.FootnoteList
	if tlist := pc.Get(footnoteListKey); tlist != nil {
		list = tlist.(*ast.FootnoteList)
	} else {
		list = ast.NewFootnoteList()
		pc.Set(footnoteListKey, list)
		var root gast.Node
		for n := node; n != nil; n = n.Parent() {
			root = n
		}
		root.AppendChild(root, list)
	}
	node.Parent().RemoveChild(node.Parent(), node)
	n := node.(*ast.Footnote)
	index := list.ChildCount() + 1
	n.Index = index
	list.AppendChild(list, node)
}

func (b *footnoteBlockParser) CanInterruptParagraph() bool {
	return true
}

func (b *footnoteBlockParser) CanAcceptIndentedLine() bool {
	return false
}

type footnoteParser struct {
}

var defaultFootnoteParser = &footnoteParser{}

// NewFootnoteParser returns a new parser.InlineParser that can parse
// footnote links of the Markdown(PHP Markdown Extra) text.
func NewFootnoteParser() parser.InlineParser {
	return defaultFootnoteParser
}

func (s *footnoteParser) Trigger() []byte {
	return []byte{'['}
}

func (s *footnoteParser) Parse(parent gast.Node, block text.Reader, pc parser.Context) gast.Node {
	line, segment := block.PeekLine()
	pos := 1
	if pos >= len(line) || line[pos] != '^' {
		return nil
	}
	pos++
	if pos >= len(line) {
		return nil
	}
	open := pos
	closure := util.FindClosure(line[pos:], '[', ']', false, false)
	if closure < 0 {
		return nil
	}
	closes := pos + closure
	value := block.Value(text.NewSegment(segment.Start+open, segment.Start+closes))
	block.Advance(closes + 1)

	var list *ast.FootnoteList
	if tlist := pc.Get(footnoteListKey); tlist != nil {
		list = tlist.(*ast.FootnoteList)
	}
	if list == nil {
		return nil
	}
	index := 0
	for def := list.FirstChild(); def != nil; def = def.NextSibling() {
		d := def.(*ast.Footnote)
		if bytes.Equal(d.Ref, value) {
			index = d.Index
			break
		}
	}
	if index == 0 {
		return nil
	}

	return ast.NewFootnoteLink(index)
}

type footnoteASTTransformer struct {
}

var defaultFootnoteASTTransformer = &footnoteASTTransformer{}

// NewFootnoteASTTransformer returns a new parser.ASTTransformer that
// insert a footnote list to the last of the document.
func NewFootnoteASTTransformer() parser.ASTTransformer {
	return defaultFootnoteASTTransformer
}

func (a *footnoteASTTransformer) Transform(node *gast.Document, reader text.Reader, pc parser.Context) {
	var list *ast.FootnoteList
	if tlist := pc.Get(footnoteListKey); tlist != nil {
		list = tlist.(*ast.FootnoteList)
		list.Parent().RemoveChild(list.Parent(), list)
	} else {
		return
	}
	pc.Set(footnoteListKey, nil)
	node.AppendChild(node, list)
}

// FootnoteHTMLRenderer is a renderer.NodeRenderer implementation that
// renders FootnoteLink nodes.
type FootnoteHTMLRenderer struct {
	html.Config
}

// NewFootnoteHTMLRenderer returns a new FootnoteHTMLRenderer.
func NewFootnoteHTMLRenderer(opts ...html.Option) renderer.NodeRenderer {
	r := &FootnoteHTMLRenderer{
		Config: html.NewConfig(),
	}
	for _, opt := range opts {
		opt.SetHTMLOption(&r.Config)
	}
	return r
}

// RegisterFuncs implements renderer.NodeRenderer.RegisterFuncs.
func (r *FootnoteHTMLRenderer) RegisterFuncs(reg renderer.NodeRendererFuncRegisterer) {
	reg.Register(ast.KindFootnoteLink, r.renderFootnoteLink)
	reg.Register(ast.KindFootnote, r.renderFootnote)
	reg.Register(ast.KindFootnoteList, r.renderFootnoteList)
}

func (r *FootnoteHTMLRenderer) renderFootnoteLink(w util.BufWriter, source []byte, node gast.Node, entering bool) (gast.WalkStatus, error) {
	if entering {
		n := node.(*ast.FootnoteLink)
		is := strconv.Itoa(n.Index)
		w.WriteString(`<sup id="fnref:`)
		w.WriteString(is)
		w.WriteString(`"><a href="#fn:`)
		w.WriteString(is)
		w.WriteString(`" class="footnote-ref" role="doc-noteref">`)
		w.WriteString(is)
		w.WriteString(`</a></sup>`)
	}
	return gast.WalkContinue, nil
}

func (r *FootnoteHTMLRenderer) renderFootnote(w util.BufWriter, source []byte, node gast.Node, entering bool) (gast.WalkStatus, error) {
	n := node.(*ast.Footnote)
	is := strconv.Itoa(n.Index)
	if entering {
		w.WriteString(`<li id="fn:`)
		w.WriteString(is)
		w.WriteString(`" role="doc-endnote">`)
		w.WriteString("\n")
	} else {
		w.WriteString("</li>\n")
	}
	return gast.WalkContinue, nil
}

func (r *FootnoteHTMLRenderer) renderFootnoteList(w util.BufWriter, source []byte, node gast.Node, entering bool) (gast.WalkStatus, error) {
	tag := "section"
	if r.Config.XHTML {
		tag = "div"
	}
	if entering {
		w.WriteString("<")
		w.WriteString(tag)
		w.WriteString(` class="footnotes" role="doc-endnotes">`)
		if r.Config.XHTML {
			w.WriteString("\n<hr />\n")
		} else {
			w.WriteString("\n<hr>\n")
		}
		w.WriteString("<ol>\n")
	} else {
		w.WriteString("</ol>\n")
		w.WriteString("<")
		w.WriteString(tag)
		w.WriteString(">\n")
	}
	return gast.WalkContinue, nil
}

type footnote struct {
}

// Footnote is an extension that allow you to use PHP Markdown Extra Footnotes.
var Footnote = &footnote{}

func (e *footnote) Extend(m goldmark.Markdown) {
	m.Parser().AddOption(
		parser.WithBlockParsers(
			util.Prioritized(NewFootnoteBlockParser(), 999),
		),
	)
	m.Parser().AddOption(
		parser.WithInlineParsers(
			util.Prioritized(NewFootnoteParser(), 101),
		),
	)
	m.Parser().AddOption(
		parser.WithASTTransformers(
			util.Prioritized(NewFootnoteASTTransformer(), 999),
		),
	)
	m.Renderer().AddOption(renderer.WithNodeRenderers(
		util.Prioritized(NewFootnoteHTMLRenderer(), 500),
	))
}