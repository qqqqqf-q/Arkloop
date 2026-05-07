//go:build desktop && !windows

package localshell

type processOutputDecoder struct{}

func newProcessOutputDecoder() *processOutputDecoder {
	return &processOutputDecoder{}
}

func (d *processOutputDecoder) Decode(chunk []byte) string {
	return string(chunk)
}

func (d *processOutputDecoder) Flush() string {
	return ""
}
