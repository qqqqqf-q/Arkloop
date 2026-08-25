package lsp

import (
	"bufio"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"unicode/utf8"
)

// PathToURI converts an absolute filesystem path to a file:// URI.
func PathToURI(absPath string) string {
	// On Windows, filepath uses backslashes.
	absPath = filepath.ToSlash(absPath)

	// url.URL expects the path to start with / on all platforms.
	// On Windows, paths like C:/foo need a leading /.
	if runtime.GOOS == "windows" && len(absPath) > 0 && absPath[0] != '/' {
		absPath = "/" + absPath
	}

	u := url.URL{
		Scheme: "file",
		Path:   absPath,
	}
	return u.String()
}

// URIToPath converts a file:// URI back to a filesystem path.
func URIToPath(uri string) (string, error) {
	u, err := url.Parse(uri)
	if err != nil {
		return "", fmt.Errorf("parse uri: %w", err)
	}
	if u.Scheme != "file" {
		return "", fmt.Errorf("unsupported scheme %q", u.Scheme)
	}

	p := u.Path
	// On Windows, /C:/foo -> C:/foo
	if runtime.GOOS == "windows" && len(p) >= 3 && p[0] == '/' && p[2] == ':' {
		p = p[1:]
	}

	return filepath.FromSlash(p), nil
}

// readLine reads the nth line (0-based) from a file.
func readLine(filePath string, lineNum uint32) (string, error) {
	f, err := os.Open(filePath)
	if err != nil {
		return "", err
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	var cur uint32
	for scanner.Scan() {
		if cur == lineNum {
			return scanner.Text(), nil
		}
		cur++
	}
	if err := scanner.Err(); err != nil {
		return "", err
	}
	return "", fmt.Errorf("line %d out of range (file has %d lines)", lineNum, cur)
}

// ExternalToLSPPosition converts 1-based line and column (UTF-8 byte offset)
// to a 0-based LSP Position with UTF-16 character offset.
func ExternalToLSPPosition(filePath string, line, col int) (Position, error) {
	if line < 1 || col < 1 {
		return Position{}, fmt.Errorf("line and col must be >= 1, got %d:%d", line, col)
	}

	lspLine := uint32(line - 1)
	lineText, err := readLine(filePath, lspLine)
	if err != nil {
		return Position{}, fmt.Errorf("read line: %w", err)
	}

	// col is 1-based UTF-8 byte offset; convert to 0-based byte index.
	byteIdx := col - 1
	if byteIdx > len(lineText) {
		byteIdx = len(lineText)
	}

	prefix := lineText[:byteIdx]
	utf16Col := UTF16Len(prefix)

	return Position{Line: lspLine, Character: utf16Col}, nil
}

// LSPToExternalPosition converts a 0-based LSP Position to 1-based line and
// column (UTF-8 byte offset).
func LSPToExternalPosition(filePath string, pos Position) (line, col int, err error) {
	lineText, err := readLine(filePath, pos.Line)
	if err != nil {
		return 0, 0, fmt.Errorf("read line: %w", err)
	}

	byteOff := UTF8OffsetFromUTF16(lineText, pos.Character)
	return int(pos.Line) + 1, byteOff + 1, nil
}

// UTF16Len returns the number of UTF-16 code units needed to encode s.
func UTF16Len(s string) uint32 {
	var n uint32
	for _, r := range s {
		if r >= 0x10000 {
			n += 2 // surrogate pair
		} else {
			n++
		}
	}
	return n
}

// UTF8OffsetFromUTF16 converts a UTF-16 offset within a line to a UTF-8 byte offset.
func UTF8OffsetFromUTF16(line string, utf16Offset uint32) int {
	var u16 uint32
	var byteOff int
	for _, r := range line {
		if u16 >= utf16Offset {
			break
		}
		byteOff += utf8.RuneLen(r)
		if r >= 0x10000 {
			u16 += 2
		} else {
			u16++
		}
	}
	return byteOff
}
