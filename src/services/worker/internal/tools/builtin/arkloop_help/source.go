package arkloophelp

import (
	"embed"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"unicode"
)

//go:embed docs/*.md
var docsFS embed.FS

const docsDir = "docs"

type Chunk struct {
	Path  string `json:"path"`
	Title string `json:"title"`
	Text  string `json:"text"`
	order int    `json:"-"`
}

var (
	loadOnce sync.Once
	chunks   []Chunk
	loadErr  error
)

func chunksIndex() ([]Chunk, error) {
	loadOnce.Do(func() {
		chunks, loadErr = loadAllChunks()
	})
	return chunks, loadErr
}

func loadAllChunks() ([]Chunk, error) {
	entries, err := docsFS.ReadDir(docsDir)
	if err != nil {
		return nil, err
	}
	var out []Chunk
	order := 0
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(strings.ToLower(e.Name()), ".md") {
			continue
		}
		rel := path.Join(docsDir, e.Name())
		raw, err := docsFS.ReadFile(rel)
		if err != nil {
			return nil, err
		}
		parts := splitByH2(string(raw), e.Name())
		for _, p := range parts {
			out = append(out, Chunk{
				Path:  rel,
				Title: p.title,
				Text:  p.body,
				order: order,
			})
			order++
		}
	}
	return out, nil
}

type h2Part struct {
	title string
	body  string
}

func splitByH2(content, fileName string) []h2Part {
	lines := strings.Split(content, "\n")
	var parts []h2Part
	var buf strings.Builder
	var preamble strings.Builder
	beforeFirstH2 := true
	var curTitle string
	fallbackTitle := strings.TrimSuffix(fileName, filepath.Ext(fileName))

	flushSection := func() {
		body := strings.TrimSpace(buf.String())
		title := strings.TrimSpace(curTitle)
		if title == "" && body == "" {
			return
		}
		if title == "" {
			title = fallbackTitle
		}
		parts = append(parts, h2Part{title: title, body: body})
	}

	for _, line := range lines {
		if strings.HasPrefix(line, "## ") && !strings.HasPrefix(line, "###") {
			if beforeFirstH2 {
				pre := strings.TrimSpace(preamble.String())
				if pre != "" {
					t, b := splitPreamble(pre, fallbackTitle)
					parts = append(parts, h2Part{title: t, body: b})
				}
				beforeFirstH2 = false
				preamble.Reset()
			} else {
				flushSection()
			}
			buf.Reset()
			curTitle = strings.TrimSpace(strings.TrimPrefix(line, "## "))
			continue
		}
		if beforeFirstH2 {
			preamble.WriteString(line)
			preamble.WriteByte('\n')
		} else {
			buf.WriteString(line)
			buf.WriteByte('\n')
		}
	}
	if beforeFirstH2 {
		pre := strings.TrimSpace(preamble.String())
		if pre != "" {
			t, b := splitPreamble(pre, fallbackTitle)
			parts = append(parts, h2Part{title: t, body: b})
		}
	} else {
		flushSection()
	}
	return parts
}

func splitPreamble(pre, fallbackTitle string) (title, body string) {
	lines := strings.Split(pre, "\n")
	if len(lines) == 0 {
		return fallbackTitle, ""
	}
	first := strings.TrimSpace(lines[0])
	if strings.HasPrefix(first, "# ") && !strings.HasPrefix(first, "##") {
		title = strings.TrimSpace(strings.TrimPrefix(first, "# "))
		body = strings.TrimSpace(strings.Join(lines[1:], "\n"))
		return title, body
	}
	return fallbackTitle, pre
}

func scoreChunk(text, query string) int {
	tl := strings.ToLower(text)
	ql := strings.TrimSpace(strings.ToLower(query))
	if ql == "" {
		return 0
	}
	score := 0
	if strings.Contains(tl, ql) {
		score += 10
	}
	for _, tok := range strings.Fields(ql) {
		if len([]rune(tok)) < 2 {
			continue
		}
		score += strings.Count(tl, tok)
	}
	for _, tok := range tokenizeCJK(ql) {
		if tok == "" {
			continue
		}
		if strings.Contains(tl, tok) {
			score += 3
		}
	}
	return score
}

func tokenizeCJK(s string) []string {
	runes := []rune(s)
	var out []string
	i := 0
	for i < len(runes) {
		if unicode.Is(unicode.Han, runes[i]) {
			j := i
			for j < len(runes) && (unicode.Is(unicode.Han, runes[j]) || runes[j] == '，' || runes[j] == '。') {
				j++
			}
			if j-i >= 2 {
				out = append(out, string(runes[i:j]))
			}
			i = j
			continue
		}
		i++
	}
	return out
}

// Search returns the top limit chunks by keyword score (stable tie-break: load order).
func Search(query string, limit int) ([]Chunk, error) {
	all, err := chunksIndex()
	if err != nil {
		return nil, err
	}
	if limit <= 0 {
		limit = 4
	}
	if limit > 12 {
		limit = 12
	}

	type scored struct {
		Chunk
		score int
	}
	var rows []scored
	q := strings.TrimSpace(query)
	for _, c := range all {
		hay := c.Title + "\n" + c.Text
		s := scoreChunk(hay, q)
		if s <= 0 {
			continue
		}
		rows = append(rows, scored{Chunk: c, score: s})
	}
	if len(rows) == 0 && q != "" {
		for _, c := range all {
			rows = append(rows, scored{Chunk: c, score: 1})
		}
	}

	sort.SliceStable(rows, func(i, j int) bool {
		if rows[i].score != rows[j].score {
			return rows[i].score > rows[j].score
		}
		return rows[i].order < rows[j].order
	})
	if len(rows) > limit {
		rows = rows[:limit]
	}
	out := make([]Chunk, 0, len(rows))
	for _, r := range rows {
		out = append(out, r.Chunk)
	}
	return out, nil
}
