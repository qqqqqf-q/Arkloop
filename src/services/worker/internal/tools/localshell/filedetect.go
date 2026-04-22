//go:build desktop

package localshell

import (
	"path/filepath"
	"regexp"
	"strings"
)

// DetectModifiedFiles extracts file paths that a shell command may have modified.
// Best-effort parsing; missed paths fall back to staleness checks.
func DetectModifiedFiles(command string, cwd string) []string {
	var paths []string
	paths = append(paths, detectRedirectTargets(command)...)
	paths = append(paths, detectSedInPlace(command)...)
	paths = append(paths, detectTeeTargets(command)...)
	paths = append(paths, detectCpMvTargets(command)...)

	seen := make(map[string]struct{}, len(paths))
	resolved := make([]string, 0, len(paths))
	for _, p := range paths {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		if !filepath.IsAbs(p) && cwd != "" {
			p = filepath.Join(cwd, p)
		}
		p = filepath.Clean(p)
		if _, ok := seen[p]; ok {
			continue
		}
		seen[p] = struct{}{}
		resolved = append(resolved, p)
	}
	return resolved
}

// > file, >> file
var redirectRe = regexp.MustCompile(`>>?\s*([^\s;|&]+)`)

func detectRedirectTargets(command string) []string {
	matches := redirectRe.FindAllStringSubmatch(command, -1)
	paths := make([]string, 0, len(matches))
	for _, m := range matches {
		paths = append(paths, unquote(m[1]))
	}
	return paths
}

// sed -i / sed --in-place
var sedInPlaceRe = regexp.MustCompile(`sed\s+(?:[^;|&]*?\s)?(?:-i|--in-place)(?:\s+\S+)?\s+(?:'[^']*'|"[^"]*"|\S+)\s+(.+?)(?:\s*[;|&]|$)`)

func detectSedInPlace(command string) []string {
	// simpler approach: split by common delimiters, find sed segments
	parts := splitPipeline(command)
	var paths []string
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if !strings.HasPrefix(part, "sed ") && !strings.HasPrefix(part, "sed\t") {
			continue
		}
		if !strings.Contains(part, "-i") && !strings.Contains(part, "--in-place") {
			continue
		}
		// last token(s) after the pattern are file args
		tokens := tokenize(part)
		// find the pattern arg (after -i and optional suffix), remaining are files
		inPlace := false
		skipNext := false
		for i, tok := range tokens {
			if skipNext {
				skipNext = false
				continue
			}
			if i == 0 {
				continue // "sed"
			}
			if tok == "-i" || tok == "--in-place" {
				inPlace = true
				continue
			}
			if strings.HasPrefix(tok, "-i") && len(tok) > 2 {
				// -iSUFFIX form
				inPlace = true
				continue
			}
			if strings.HasPrefix(tok, "-") && tok != "-" {
				if tok == "-e" || tok == "-f" {
					skipNext = true
				}
				continue
			}
			if inPlace {
				// first non-flag after seeing -i is the pattern, rest are files
				// actually: sed -i 's/a/b/' file1 file2
				// skip pattern, collect files
				for _, f := range tokens[i+1:] {
					if strings.HasPrefix(f, "-") {
						continue
					}
					paths = append(paths, unquote(f))
				}
				break
			}
		}
	}
	return paths
}

// tee / tee -a
func detectTeeTargets(command string) []string {
	parts := splitPipeline(command)
	var paths []string
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if !strings.HasPrefix(part, "tee ") && !strings.HasPrefix(part, "tee\t") {
			continue
		}
		tokens := tokenize(part)
		for i, tok := range tokens {
			if i == 0 {
				continue
			}
			if tok == "-a" || tok == "--append" {
				continue
			}
			if strings.HasPrefix(tok, "-") {
				continue
			}
			paths = append(paths, unquote(tok))
		}
	}
	return paths
}

// cp / mv -> last arg is target
func detectCpMvTargets(command string) []string {
	parts := splitPipeline(command)
	var paths []string
	for _, part := range parts {
		part = strings.TrimSpace(part)
		tokens := tokenize(part)
		if len(tokens) < 3 {
			continue
		}
		cmd := tokens[0]
		if cmd != "cp" && cmd != "mv" {
			continue
		}
		// last non-flag token is the destination
		last := tokens[len(tokens)-1]
		if !strings.HasPrefix(last, "-") {
			paths = append(paths, unquote(last))
		}
	}
	return paths
}

// splitPipeline splits on | ; && ||
func splitPipeline(command string) []string {
	// simple split; doesn't handle quotes perfectly
	var parts []string
	var current strings.Builder
	inSingle, inDouble := false, false
	runes := []rune(command)
	for i := 0; i < len(runes); i++ {
		ch := runes[i]
		if ch == '\'' && !inDouble {
			inSingle = !inSingle
			current.WriteRune(ch)
			continue
		}
		if ch == '"' && !inSingle {
			inDouble = !inDouble
			current.WriteRune(ch)
			continue
		}
		if inSingle || inDouble {
			current.WriteRune(ch)
			continue
		}
		if ch == '|' || ch == ';' {
			parts = append(parts, current.String())
			current.Reset()
			// skip && or ||
			if i+1 < len(runes) && (runes[i+1] == '|' || runes[i+1] == '&') {
				i++
			}
			continue
		}
		if ch == '&' {
			if i+1 < len(runes) && runes[i+1] == '&' {
				parts = append(parts, current.String())
				current.Reset()
				i++
				continue
			}
		}
		current.WriteRune(ch)
	}
	if current.Len() > 0 {
		parts = append(parts, current.String())
	}
	return parts
}

// tokenize splits a command fragment into shell-like tokens (basic quote handling)
func tokenize(s string) []string {
	var tokens []string
	var current strings.Builder
	inSingle, inDouble := false, false
	for _, ch := range s {
		if ch == '\'' && !inDouble {
			inSingle = !inSingle
			current.WriteRune(ch)
			continue
		}
		if ch == '"' && !inSingle {
			inDouble = !inDouble
			current.WriteRune(ch)
			continue
		}
		if (ch == ' ' || ch == '\t') && !inSingle && !inDouble {
			if current.Len() > 0 {
				tokens = append(tokens, current.String())
				current.Reset()
			}
			continue
		}
		current.WriteRune(ch)
	}
	if current.Len() > 0 {
		tokens = append(tokens, current.String())
	}
	return tokens
}

func unquote(s string) string {
	if len(s) >= 2 {
		if (s[0] == '\'' && s[len(s)-1] == '\'') || (s[0] == '"' && s[len(s)-1] == '"') {
			return s[1 : len(s)-1]
		}
	}
	return s
}
