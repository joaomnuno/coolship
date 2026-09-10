// Package sshkey generates the key pair a deploy key needs: an Ed25519
// private key in OpenSSH's own file format, which is what Coolify's helper
// container hands to ssh -i, and the one-line public key a Git host takes
// verbatim. It uses the standard library only and never touches the disk.
package sshkey

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"strings"
	"unicode"
)

// Pair is a freshly generated key pair. Private is secret: it is sent to
// Coolify once and must never be printed or written by a caller. Public is
// the line to register on the repository.
type Pair struct {
	Private string
	Public  string
}

const keyType = "ssh-ed25519"

// Generate creates an Ed25519 pair. The comment ends the public key line, so
// the key can be recognized on the Git host; it must be a single word.
func Generate(comment string) (Pair, error) {
	if strings.IndexFunc(comment, func(r rune) bool { return unicode.IsSpace(r) || unicode.IsControl(r) }) >= 0 {
		return Pair{}, errors.New("key comment must not contain whitespace")
	}
	public, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return Pair{}, err
	}
	var check [4]byte
	if _, err := rand.Read(check[:]); err != nil {
		return Pair{}, err
	}
	return Pair{Private: encodePrivate(public, private, check, comment), Public: encodePublic(public, comment)}, nil
}

// encodePublic renders the authorized_keys form: type, base64 blob, comment.
func encodePublic(public ed25519.PublicKey, comment string) string {
	blob := appendString(nil, []byte(keyType))
	blob = appendString(blob, public)
	line := keyType + " " + base64.StdEncoding.EncodeToString(blob)
	if comment != "" {
		line += " " + comment
	}
	return line
}

// encodePrivate renders the unencrypted "openssh-key-v1" container as
// ssh-keygen would write it, wrapped in its PEM-like armor.
func encodePrivate(public ed25519.PublicKey, private ed25519.PrivateKey, check [4]byte, comment string) string {
	publicBlob := appendString(nil, []byte(keyType))
	publicBlob = appendString(publicBlob, public)

	var section []byte
	section = append(section, check[:]...)
	section = append(section, check[:]...)
	section = appendString(section, []byte(keyType))
	section = appendString(section, public)
	section = appendString(section, private) // seed followed by the public half, as OpenSSH stores it
	section = appendString(section, []byte(comment))
	for pad := byte(1); len(section)%8 != 0; pad++ {
		section = append(section, pad)
	}

	var body []byte
	body = append(body, "openssh-key-v1\x00"...)
	body = appendString(body, []byte("none"))
	body = appendString(body, []byte("none"))
	body = appendString(body, nil)
	body = binary.BigEndian.AppendUint32(body, 1)
	body = appendString(body, publicBlob)
	body = appendString(body, section)

	encoded := base64.StdEncoding.EncodeToString(body)
	var out strings.Builder
	out.WriteString("-----BEGIN OPENSSH PRIVATE KEY-----\n")
	for len(encoded) > 70 {
		out.WriteString(encoded[:70])
		out.WriteByte('\n')
		encoded = encoded[70:]
	}
	out.WriteString(encoded)
	out.WriteString("\n-----END OPENSSH PRIVATE KEY-----\n")
	return out.String()
}

// appendString appends an SSH wire "string": a big-endian length and the bytes.
func appendString(buffer, value []byte) []byte {
	buffer = binary.BigEndian.AppendUint32(buffer, uint32(len(value)))
	return append(buffer, value...)
}
