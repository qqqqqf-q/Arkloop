package mcpoauth

import "strings"

func ParseWWWAuthenticateParams(header string) map[string]string {
	header = strings.TrimSpace(header)
	if header == "" {
		return map[string]string{}
	}
	scheme, rest, ok := strings.Cut(header, " ")
	if !ok || !strings.EqualFold(scheme, "Bearer") {
		return map[string]string{}
	}

	params := map[string]string{}
	for len(rest) > 0 {
		rest = strings.TrimLeft(rest, " \t,")
		if rest == "" {
			break
		}
		key, remainder, ok := strings.Cut(rest, "=")
		if !ok {
			break
		}
		key = strings.TrimSpace(key)
		remainder = strings.TrimLeft(remainder, " \t")
		if key == "" || remainder == "" {
			break
		}

		value, tail := readAuthParamValue(remainder)
		if key != "" {
			params[key] = value
		}
		rest = tail
	}
	return params
}

func readAuthParamValue(input string) (string, string) {
	if input[0] != '"' {
		if idx := strings.IndexByte(input, ','); idx >= 0 {
			return strings.TrimSpace(input[:idx]), input[idx+1:]
		}
		return strings.TrimSpace(input), ""
	}

	var builder strings.Builder
	escaped := false
	for i := 1; i < len(input); i++ {
		ch := input[i]
		if escaped {
			builder.WriteByte(ch)
			escaped = false
			continue
		}
		switch ch {
		case '\\':
			escaped = true
		case '"':
			return builder.String(), input[i+1:]
		default:
			builder.WriteByte(ch)
		}
	}
	return builder.String(), ""
}
