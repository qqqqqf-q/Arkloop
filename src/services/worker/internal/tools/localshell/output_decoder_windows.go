//go:build desktop && windows

package localshell

import (
	"bytes"

	"golang.org/x/sys/windows"
	"golang.org/x/text/encoding"
	"golang.org/x/text/encoding/charmap"
	"golang.org/x/text/encoding/japanese"
	"golang.org/x/text/encoding/korean"
	"golang.org/x/text/encoding/simplifiedchinese"
	"golang.org/x/text/encoding/traditionalchinese"
	"golang.org/x/text/transform"
)

type processOutputDecoder struct {
	enc         encoding.Encoding
	transformer transform.Transformer
	pending     []byte
}

func newProcessOutputDecoder() *processOutputDecoder {
	return &processOutputDecoder{}
}

func (d *processOutputDecoder) Decode(chunk []byte) string {
	return d.decode(chunk, false)
}

func (d *processOutputDecoder) Flush() string {
	if len(d.pending) == 0 {
		return ""
	}
	pending := append([]byte(nil), d.pending...)
	d.pending = nil
	return d.decode(pending, true)
}

func (d *processOutputDecoder) decode(chunk []byte, atEOF bool) string {
	enc := windowsProcessOutputEncoding()
	if enc == nil {
		d.enc = nil
		d.transformer = nil
		d.pending = nil
		return string(chunk)
	}
	if d.enc != enc || d.transformer == nil {
		d.enc = enc
		d.transformer = enc.NewDecoder()
		d.pending = nil
	}

	src := append(append([]byte(nil), d.pending...), chunk...)
	d.pending = nil
	var out bytes.Buffer
	dst := make([]byte, max(len(src)*4, 4096))
	for len(src) > 0 {
		nDst, nSrc, err := d.transformer.Transform(dst, src, atEOF)
		if nDst > 0 {
			out.Write(dst[:nDst])
		}
		src = src[nSrc:]
		if err == nil {
			break
		}
		if err == transform.ErrShortDst {
			continue
		}
		if err == transform.ErrShortSrc && !atEOF {
			d.pending = append(d.pending[:0], src...)
			break
		}
		out.Write(src)
		break
	}
	return out.String()
}

func windowsProcessOutputEncoding() encoding.Encoding {
	if consoleCodePage, err := windows.GetConsoleOutputCP(); err == nil {
		if consoleCodePage == 65001 {
			// Console output is already UTF-8. Do not fall back to ACP decoders.
			return nil
		}
		if enc := processOutputEncodingForCodePage(consoleCodePage); enc != nil {
			return enc
		}
	}
	if enc := processOutputEncodingForCodePage(windows.GetACP()); enc != nil {
		return enc
	}
	return nil
}

func processOutputEncodingForCodePage(codePage uint32) encoding.Encoding {
	switch codePage {
	case 0, 65001:
		return nil
	case 437:
		return charmap.CodePage437
	case 850:
		return charmap.CodePage850
	case 852:
		return charmap.CodePage852
	case 866:
		return charmap.CodePage866
	case 874:
		return charmap.Windows874
	case 932:
		return japanese.ShiftJIS
	case 936:
		return simplifiedchinese.GB18030
	case 949:
		return korean.EUCKR
	case 950:
		return traditionalchinese.Big5
	case 1250:
		return charmap.Windows1250
	case 1251:
		return charmap.Windows1251
	case 1252:
		return charmap.Windows1252
	case 1253:
		return charmap.Windows1253
	case 1254:
		return charmap.Windows1254
	case 1255:
		return charmap.Windows1255
	case 1256:
		return charmap.Windows1256
	case 1257:
		return charmap.Windows1257
	case 1258:
		return charmap.Windows1258
	default:
		return nil
	}
}
