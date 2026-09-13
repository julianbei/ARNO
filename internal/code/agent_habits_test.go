package code

import "testing"

func TestDeclarationName(t *testing.T) {
	cases := map[string]string{
		"func (c *Command) Name":         "Name",
		"func (c *Command) ParseFlags(":  "ParseFlags",
		"pub(crate) fn walk_dir":         "walk_dir",
		"def parse_header(value)":        "parse_header",
		"export async function getJSON":  "getJSON",
		"class HTTPAdapter(BaseAdapter)": "HTTPAdapter",
		"type Command struct":            "Command",
		"(":                              "",
	}
	for query, want := range cases {
		if got := declarationName(query); got != want {
			t.Errorf("%q: got %q, want %q", query, got, want)
		}
	}
}

func TestInsertDropsAnchorRepeatedAtTheSeam(t *testing.T) {
	source := "package a\n\nfunc B() {}\n"

	before, err := insertInto(source, "func B() {}", InsertBefore, "func A() {}\n\nfunc B() {}")
	if err != nil {
		t.Fatal(err)
	}
	if want := "package a\n\nfunc A() {}\n\nfunc B() {}\n"; before != want {
		t.Errorf("before:\ngot  %q\nwant %q", before, want)
	}

	after, err := insertInto(source, "func B() {}", InsertAfter, "func B() {}\n\nfunc C() {}")
	if err != nil {
		t.Fatal(err)
	}
	if want := "package a\n\nfunc B() {}\n\nfunc C() {}\n"; after != want {
		t.Errorf("after:\ngot  %q\nwant %q", after, want)
	}

	plain, err := insertInto(source, "func B() {}", InsertBefore, "func A() {}\n")
	if err != nil {
		t.Fatal(err)
	}
	if want := "package a\n\nfunc A() {}\nfunc B() {}\n"; plain != want {
		t.Errorf("plain:\ngot  %q\nwant %q", plain, want)
	}
}
