package coolify

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"strings"
)

// redacted replaces a secret value in a traced body.
const redacted = "[redacted]"

// withheldBody replaces a body that looks like JSON but cannot be parsed, such
// as a refusal cut short, since its secret fields cannot be found.
const withheldBody = "[body withheld: not valid JSON, so secrets could not be redacted; set COOLSHIP_DEBUG_UNREDACTED=1 to show it]"

// secretKey reports whether a JSON field carries a secret: an environment
// variable's value, a private key, a password, a token, or a secret. Fields
// that only refer to such a thing by identity (private_key_uuid) are kept.
func secretKey(key string) bool {
	key = strings.ToLower(key)
	if strings.HasSuffix(key, "_uuid") || strings.HasSuffix(key, "_id") {
		return false
	}
	if key == "value" || key == "real_value" {
		return true
	}
	for _, part := range []string{"secret", "password", "private_key", "token", "api_key"} {
		if strings.Contains(key, part) {
			return true
		}
	}
	return false
}

// redactBody returns a traced body with the values of secret fields replaced.
// A JSON body is re-encoded compactly in its original field order. A body
// that is not JSON is returned as it is, unless it starts like JSON, in which
// case it is withheld.
func redactBody(body []byte) []byte {
	trimmed := bytes.TrimSpace(body)
	if len(trimmed) == 0 {
		return body
	}
	if trimmed[0] != '{' && trimmed[0] != '[' {
		return body
	}
	out, err := redactJSON(trimmed)
	if err != nil {
		return []byte(withheldBody)
	}
	return out
}

type redactFrame struct {
	object    bool
	expectKey bool
	first     bool
	secret    bool // every value inside is a secret
	key       string
}

func redactJSON(data []byte) ([]byte, error) {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	var out bytes.Buffer
	var stack []*redactFrame
	for {
		token, err := decoder.Token()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, err
		}
		if delim, ok := token.(json.Delim); ok && (delim == '}' || delim == ']') {
			out.WriteByte(byte(delim))
			stack = stack[:len(stack)-1]
			if len(stack) > 0 && stack[len(stack)-1].object {
				stack[len(stack)-1].expectKey = true
			}
			continue
		}
		secret := false
		if len(stack) > 0 {
			parent := stack[len(stack)-1]
			switch {
			case parent.object && parent.expectKey:
				if !parent.first {
					out.WriteByte(',')
				}
				parent.first = false
				parent.expectKey = false
				parent.key, _ = token.(string)
				writeJSON(&out, parent.key)
				out.WriteByte(':')
				continue
			case parent.object:
				secret = parent.secret || secretKey(parent.key)
			default:
				if !parent.first {
					out.WriteByte(',')
				}
				parent.first = false
				secret = parent.secret
			}
		}
		if delim, ok := token.(json.Delim); ok {
			out.WriteByte(byte(delim))
			stack = append(stack, &redactFrame{object: delim == '{', expectKey: delim == '{', first: true, secret: secret})
			continue
		}
		if secret && token != nil && token != "" {
			token = redacted
		}
		writeJSON(&out, token)
		if len(stack) > 0 && stack[len(stack)-1].object {
			stack[len(stack)-1].expectKey = true
		}
	}
	if len(stack) != 0 {
		return nil, io.ErrUnexpectedEOF
	}
	return out.Bytes(), nil
}

func writeJSON(out *bytes.Buffer, value any) {
	var encoded bytes.Buffer
	encoder := json.NewEncoder(&encoded)
	encoder.SetEscapeHTML(false)
	// A decoded token always encodes.
	_ = encoder.Encode(value)
	out.Write(bytes.TrimSuffix(encoded.Bytes(), []byte("\n")))
}
