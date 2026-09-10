package sshkey

import (
	"bytes"
	"encoding/base64"
	"encoding/binary"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestGenerateProducesMatchingOpenSSHPair(t *testing.T) {
	pair, err := Generate("coolship:test")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(pair.Private, "-----BEGIN OPENSSH PRIVATE KEY-----\n") || !strings.HasSuffix(pair.Private, "\n-----END OPENSSH PRIVATE KEY-----\n") {
		t.Fatalf("private key armor: %q", pair.Private[:40])
	}
	fields := strings.Fields(pair.Public)
	if len(fields) != 3 || fields[0] != "ssh-ed25519" || fields[2] != "coolship:test" || strings.ContainsAny(pair.Public, "\r\n") {
		t.Fatalf("public key line: %q", pair.Public)
	}
	if _, err := Generate("two words"); err == nil {
		t.Fatal("comment with whitespace accepted")
	}
	// The container is what OpenSSH documents: magic, cipher none, one key
	// whose public blob is the same one the public line carries.
	armored := strings.TrimSuffix(strings.TrimPrefix(pair.Private, "-----BEGIN OPENSSH PRIVATE KEY-----\n"), "-----END OPENSSH PRIVATE KEY-----\n")
	body, err := base64.StdEncoding.DecodeString(strings.ReplaceAll(armored, "\n", ""))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.HasPrefix(body, []byte("openssh-key-v1\x00")) {
		t.Fatalf("magic: %q", body[:16])
	}
	rest := body[len("openssh-key-v1\x00"):]
	cipher, rest := readString(t, rest)
	kdf, rest := readString(t, rest)
	_, rest = readString(t, rest)
	count := binary.BigEndian.Uint32(rest)
	publicBlob, rest := readString(t, rest[4:])
	section, rest := readString(t, rest)
	if string(cipher) != "none" || string(kdf) != "none" || count != 1 || len(rest) != 0 || len(section)%8 != 0 {
		t.Fatalf("container cipher=%q kdf=%q keys=%d trailing=%d section=%d", cipher, kdf, count, len(rest), len(section))
	}
	if want, _ := base64.StdEncoding.DecodeString(fields[1]); !bytes.Equal(publicBlob, want) {
		t.Fatal("public blob in the private file differs from the public line")
	}
	if !bytes.Equal(section[:4], section[4:8]) {
		t.Fatal("check integers differ")
	}
	// Two calls never share a key.
	again, err := Generate("coolship:test")
	if err != nil || again.Public == pair.Public {
		t.Fatalf("second key: %v", err)
	}
}

func TestSSHKeygenReadsThePrivateKey(t *testing.T) {
	if _, err := exec.LookPath("ssh-keygen"); err != nil {
		t.Skip("ssh-keygen is not installed")
	}
	pair, err := Generate("coolship:roundtrip")
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "id_ed25519")
	if err := os.WriteFile(path, []byte(pair.Private), 0o600); err != nil { // a test directory; the product never writes the key
		t.Fatal(err)
	}
	out, err := exec.Command("ssh-keygen", "-y", "-f", path).CombinedOutput()
	if err != nil {
		t.Fatalf("ssh-keygen -y: %v\n%s", err, out)
	}
	if derived := strings.Fields(string(out)); len(derived) < 2 || derived[0] != "ssh-ed25519" || derived[1] != strings.Fields(pair.Public)[1] {
		t.Fatalf("ssh-keygen derived %q from a key whose public line is %q", out, pair.Public)
	}
}

func readString(t *testing.T, data []byte) ([]byte, []byte) {
	t.Helper()
	if len(data) < 4 {
		t.Fatal("truncated container")
	}
	length := binary.BigEndian.Uint32(data)
	if uint32(len(data)-4) < length {
		t.Fatal("truncated string")
	}
	return data[4 : 4+length], data[4+length:]
}
