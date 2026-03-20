package telegrambot

import (
	"bytes"
	"fmt"
	"html"
	"strings"
	"unicode/utf8"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/extension"
	"github.com/yuin/goldmark/text"
	extast "github.com/yuin/goldmark/extension/ast"
)

var telegramGoldmark = goldmark.New(
	goldmark.WithExtensions(extension.GFM),
)

func formatAssistantMarkdownAsHTMLGoldmark(src string) string {
	if strings.TrimSpace(src) == "" {
		return src
	}
	doc := telegramGoldmark.Parser().Parse(text.NewReader([]byte(src)))
	var buf bytes.Buffer
	renderGoldmark(&buf, doc, []byte(src))
	return strings.TrimSpace(buf.String())
}

func blockRawFromLines(lines *text.Segments, src []byte) string {
	if lines == nil {
		return ""
	}
	var b strings.Builder
	for i := 0; i < lines.Len(); i++ {
		seg := lines.At(i)
		b.Write(seg.Value(src))
	}
	return b.String()
}

func renderGoldmark(w *bytes.Buffer, n ast.Node, src []byte) {
	switch n := n.(type) {
	case *ast.Document:
		for c := n.FirstChild(); c != nil; c = c.NextSibling() {
			renderGoldmark(w, c, src)
		}
	case *ast.Paragraph:
		for c := n.FirstChild(); c != nil; c = c.NextSibling() {
			renderGoldmark(w, c, src)
		}
		w.WriteByte('\n')
	case *ast.Heading:
		w.WriteString("<b>")
		for c := n.FirstChild(); c != nil; c = c.NextSibling() {
			renderGoldmark(w, c, src)
		}
		w.WriteString("</b>\n")
	case *ast.Blockquote:
		w.WriteString("<blockquote>")
		for c := n.FirstChild(); c != nil; c = c.NextSibling() {
			renderGoldmark(w, c, src)
		}
		w.WriteString("</blockquote>\n")
	case *ast.CodeBlock:
		w.WriteString("<pre>")
		w.WriteString(telegramEscapeHTML(blockRawFromLines(n.Lines(), src)))
		w.WriteString("</pre>\n")
	case *ast.FencedCodeBlock:
		body := strings.TrimRight(blockRawFromLines(n.Lines(), src), "\n")
		escaped := telegramEscapeHTML(body)
		lang := strings.TrimSpace(string(n.Language(src)))
		if lang != "" {
			fmt.Fprintf(w, "<pre><code class=\"language-%s\">%s</code></pre>\n", telegramEscapeHTML(lang), escaped)
		} else {
			w.WriteString("<pre>" + escaped + "</pre>\n")
		}
	case *ast.ThematicBreak:
		w.WriteByte('\n')
	case *ast.List:
		for c := n.FirstChild(); c != nil; c = c.NextSibling() {
			renderGoldmark(w, c, src)
		}
	case *ast.ListItem:
		w.WriteString("• ")
		for c := n.FirstChild(); c != nil; c = c.NextSibling() {
			switch ch := c.(type) {
			case *ast.Paragraph:
				for ic := ch.FirstChild(); ic != nil; ic = ic.NextSibling() {
					renderGoldmark(w, ic, src)
				}
			case *ast.List:
				w.WriteByte('\n')
				renderGoldmark(w, ch, src)
			default:
				renderGoldmark(w, c, src)
			}
		}
		w.WriteByte('\n')
	case *ast.Text:
		w.WriteString(telegramEscapeHTML(string(n.Value(src))))
		if n.HardLineBreak() {
			w.WriteByte('\n')
		} else if n.SoftLineBreak() {
			w.WriteByte(' ')
		}
	case *ast.CodeSpan:
		w.WriteString("<code>")
		w.WriteString(telegramEscapeHTML(codeSpanText(n, src)))
		w.WriteString("</code>")
	case *ast.Emphasis:
		open, close := "<i>", "</i>"
		if n.Level >= 2 {
			open, close = "<b>", "</b>"
		}
		w.WriteString(open)
		for c := n.FirstChild(); c != nil; c = c.NextSibling() {
			renderGoldmark(w, c, src)
		}
		w.WriteString(close)
	case *ast.Link:
		dest := strings.TrimSpace(string(n.Destination))
		if linkDestAllowed(dest) {
			fmt.Fprintf(w, `<a href="%s">`, html.EscapeString(dest))
			for c := n.FirstChild(); c != nil; c = c.NextSibling() {
				renderGoldmark(w, c, src)
			}
			w.WriteString("</a>")
			return
		}
		for c := n.FirstChild(); c != nil; c = c.NextSibling() {
			renderGoldmark(w, c, src)
		}
	case *ast.Image:
		dest := strings.TrimSpace(string(n.Destination))
		var alt strings.Builder
		for c := n.FirstChild(); c != nil; c = c.NextSibling() {
			if t, ok := c.(*ast.Text); ok {
				alt.Write(t.Value(src))
			}
		}
		alts := alt.String()
		if linkDestAllowed(dest) {
			fmt.Fprintf(w, `<a href="%s">`, html.EscapeString(dest))
			w.WriteString(telegramEscapeHTML(alts))
			w.WriteString("</a>")
			return
		}
		w.WriteString(telegramEscapeHTML(alts))
	case *ast.AutoLink:
		url := string(n.URL(src))
		if n.AutoLinkType == ast.AutoLinkURL && linkDestAllowed(url) {
			fmt.Fprintf(w, `<a href="%s">`, html.EscapeString(strings.TrimSpace(url)))
			w.WriteString(telegramEscapeHTML(string(n.Label(src))))
			w.WriteString("</a>")
			return
		}
		w.WriteString(telegramEscapeHTML(string(n.Label(src))))
	case *extast.Strikethrough:
		w.WriteString("<s>")
		for c := n.FirstChild(); c != nil; c = c.NextSibling() {
			renderGoldmark(w, c, src)
		}
		w.WriteString("</s>")
	case *extast.TaskCheckBox:
		if n.IsChecked {
			w.WriteString("☑ ")
		} else {
			w.WriteString("☐ ")
		}
	case *extast.Table:
		w.WriteString("<pre>")
		w.WriteString(telegramEscapeHTML(renderTableASCII(n, src)))
		w.WriteString("</pre>\n")
	default:
		for c := n.FirstChild(); c != nil; c = c.NextSibling() {
			renderGoldmark(w, c, src)
		}
	}
}

func codeSpanText(n *ast.CodeSpan, src []byte) string {
	var b strings.Builder
	for c := n.FirstChild(); c != nil; c = c.NextSibling() {
		if t, ok := c.(*ast.Text); ok {
			b.Write(t.Value(src))
		}
	}
	return b.String()
}

func linkDestAllowed(dest string) bool {
	return strings.HasPrefix(dest, "http://") || strings.HasPrefix(dest, "https://")
}

func renderTableASCII(tab *extast.Table, src []byte) string {
	var rows [][]string
	for c := tab.FirstChild(); c != nil; c = c.NextSibling() {
		switch h := c.(type) {
		case *extast.TableHeader:
			for r := h.FirstChild(); r != nil; r = r.NextSibling() {
				if row, ok := r.(*extast.TableRow); ok {
					rows = append(rows, tableRowCells(row, src))
				}
			}
		case *extast.TableRow:
			rows = append(rows, tableRowCells(h, src))
		}
	}
	if len(rows) == 0 {
		return ""
	}
	maxCol := 0
	for _, row := range rows {
		if len(row) > maxCol {
			maxCol = len(row)
		}
	}
	widths := make([]int, maxCol)
	for _, row := range rows {
		for i, cell := range row {
			if i >= len(widths) {
				continue
			}
			w := utf8.RuneCountInString(cell)
			if w > widths[i] {
				widths[i] = w
			}
		}
	}
	var b strings.Builder
	for _, row := range rows {
		for i, cell := range row {
			if i > 0 {
				b.WriteString(" | ")
			}
			pad := 0
			if i < len(widths) {
				pad = widths[i] - utf8.RuneCountInString(cell)
				if pad < 0 {
					pad = 0
				}
			}
			b.WriteString(cell)
			if pad > 0 {
				b.WriteString(strings.Repeat(" ", pad))
			}
		}
		b.WriteByte('\n')
	}
	return strings.TrimRight(b.String(), "\n")
}

func tableRowCells(row *extast.TableRow, src []byte) []string {
	var cells []string
	for c := row.FirstChild(); c != nil; c = c.NextSibling() {
		cell, ok := c.(*extast.TableCell)
		if !ok {
			continue
		}
		cells = append(cells, strings.TrimSpace(cellPlain(cell, src)))
	}
	return cells
}

func cellPlain(cell *extast.TableCell, src []byte) string {
	var b strings.Builder
	var walk func(ast.Node)
	walk = func(n ast.Node) {
		if n == nil {
			return
		}
		switch x := n.(type) {
		case *ast.Text:
			b.Write(x.Value(src))
		default:
			for c := n.FirstChild(); c != nil; c = c.NextSibling() {
				walk(c)
			}
		}
	}
	for c := cell.FirstChild(); c != nil; c = c.NextSibling() {
		walk(c)
	}
	return b.String()
}
