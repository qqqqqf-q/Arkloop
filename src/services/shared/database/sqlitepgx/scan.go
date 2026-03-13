//go:build desktop

package sqlitepgx

import (
	"encoding/json"
	"fmt"
	"reflect"
	"time"
)

// sqlite 常见时间格式
var timeFormats = []string{
	time.RFC3339Nano,
	time.RFC3339,
	"2006-01-02T15:04:05",
	"2006-01-02 15:04:05",
	"2006-01-02",
}

func parseTime(s string) (time.Time, error) {
	for _, f := range timeFormats {
		if t, err := time.Parse(f, s); err == nil {
			return t, nil
		}
	}
	return time.Time{}, fmt.Errorf("sqlitepgx: cannot parse %q as time", s)
}

// timeScanner 将 TEXT/string 自动转换为 time.Time。
type timeScanner struct {
	dest reflect.Value
	ptr  bool
}

func (ts *timeScanner) Scan(src any) error {
	if src == nil {
		if ts.ptr {
			ts.dest.Set(reflect.Zero(ts.dest.Type()))
		} else {
			ts.dest.Set(reflect.ValueOf(time.Time{}))
		}
		return nil
	}
	switch v := src.(type) {
	case time.Time:
		if ts.ptr {
			ts.dest.Set(reflect.ValueOf(&v))
		} else {
			ts.dest.Set(reflect.ValueOf(v))
		}
	case string:
		t, err := parseTime(v)
		if err != nil {
			return err
		}
		if ts.ptr {
			ts.dest.Set(reflect.ValueOf(&t))
		} else {
			ts.dest.Set(reflect.ValueOf(t))
		}
	default:
		return fmt.Errorf("sqlitepgx: cannot scan %T into time.Time", src)
	}
	return nil
}

// jsonSliceScanner 将 JSON 文本反序列化为 []string。
type jsonSliceScanner struct {
	dest reflect.Value
}

func (js *jsonSliceScanner) Scan(src any) error {
	if src == nil {
		js.dest.Set(reflect.Zero(js.dest.Type()))
		return nil
	}
	var raw []byte
	switch v := src.(type) {
	case string:
		raw = []byte(v)
	case []byte:
		raw = v
	default:
		return fmt.Errorf("sqlitepgx: cannot scan %T into []string", src)
	}
	var result []string
	if err := json.Unmarshal(raw, &result); err != nil {
		return fmt.Errorf("sqlitepgx: json decode []string: %w", err)
	}
	js.dest.Set(reflect.ValueOf(result))
	return nil
}

// jsonRawScanner 将 TEXT 原样转为 json.RawMessage ([]byte)。
type jsonRawScanner struct {
	dest reflect.Value
}

func (jr *jsonRawScanner) Scan(src any) error {
	if src == nil {
		jr.dest.Set(reflect.Zero(jr.dest.Type()))
		return nil
	}
	switch v := src.(type) {
	case string:
		jr.dest.Set(reflect.ValueOf(json.RawMessage(v)))
	case []byte:
		cp := make(json.RawMessage, len(v))
		copy(cp, v)
		jr.dest.Set(reflect.ValueOf(cp))
	default:
		return fmt.Errorf("sqlitepgx: cannot scan %T into json.RawMessage", src)
	}
	return nil
}

var (
	timeType       = reflect.TypeOf(time.Time{})
	timePtrType    = reflect.TypeOf((*time.Time)(nil))
	stringSliceType = reflect.TypeOf([]string(nil))
	rawMessageType  = reflect.TypeOf(json.RawMessage(nil))
)

// wrapScanTargets 遍历 Scan 目标参数，将需要特殊处理的类型替换为自定义 scanner。
func wrapScanTargets(dest []any) []any {
	out := make([]any, len(dest))
	for i, d := range dest {
		v := reflect.ValueOf(d)
		if v.Kind() == reflect.Ptr && !v.IsNil() {
			elem := v.Elem()
			switch elem.Type() {
			case timeType:
				out[i] = &timeScanner{dest: elem, ptr: false}
				continue
			case timePtrType:
				out[i] = &timeScanner{dest: elem, ptr: true}
				continue
			case stringSliceType:
				out[i] = &jsonSliceScanner{dest: elem}
				continue
			case rawMessageType:
				out[i] = &jsonRawScanner{dest: elem}
				continue
			}
		}
		out[i] = d
	}
	return out
}
