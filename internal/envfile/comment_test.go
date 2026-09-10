package envfile

import "testing"

func TestCommentIsWrittenOnceAndLeavesOtherCommentsAlone(t *testing.T) {
	f, err := Parse([]byte("# my own note about SECRET\nA=1\n"))
	if err != nil {
		t.Fatal(err)
	}
	note := "SECRET is withheld by Coolify (shown once); set it here yourself"
	if !f.Comment(note) {
		t.Fatal("first comment was not added")
	}
	if f.Comment(note) || f.Comment(" "+note+"\n") {
		t.Fatal("the same comment was appended again")
	}
	want := "# my own note about SECRET\nA=1\n# " + note + "\n"
	if got := string(f.Bytes()); got != want {
		t.Fatalf("got:\n%s\nwant:\n%s", got, want)
	}
	// A different comment about the same key is a different line.
	if !f.Comment("SECRET is set in CI") {
		t.Fatal("a different comment was refused")
	}
	if got := string(f.Bytes()); got != want+"# SECRET is set in CI\n" {
		t.Fatalf("got:\n%s", got)
	}
}
