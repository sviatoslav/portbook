package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"portbook/internal/booking"
	"reflect"
	"strconv"
	"strings"
	"testing"
)

func TestListFormatsHelpAndDiagnostics(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("PORTBOOK_CONFIG_DIR", "relative invalid config ignored")
	for _, terminal := range []bool{false, true} {
		for _, args := range [][]string{{"list"}, {"list", "--all"}, {"list", "--"}} {
			var out, diag bytes.Buffer
			code := Run(context.Background(), args, &out, &diag, terminal)
			want := "{\"command\":\"list\",\"bookings\":[]}\n"
			if terminal {
				want = "No bookings found.\n"
			}
			if code != 0 || out.String() != want || diag.Len() != 0 {
				t.Fatal(args, code, out.String(), diag.String())
			}
		}
		cases := []struct {
			args []string
			json bool
		}{
			{[]string{"list", "--all", "--directory", t.TempDir()}, !terminal},
			{[]string{"list", "--all=false", "--directory", t.TempDir(), "--help"}, !terminal},
			{[]string{"list", "--all", "--all"}, !terminal},
			{[]string{"list", "--directory", t.TempDir(), "--directory", t.TempDir()}, !terminal},
			{[]string{"list", "--directory=relative", "--help", "--output=json"}, true},
			{[]string{"list", "--directory=", "--output=table"}, false},
			{[]string{"list", "--directory", "--output=json"}, !terminal},
			{[]string{"list", "--bogus", "--output=table"}, false},
			{[]string{"list", "--", "--help", "--output=table"}, !terminal},
			{[]string{"list", "--all", "operand", "--help"}, !terminal},
			{[]string{"list", "--output=json", "--output=table"}, true},
		}
		for _, tc := range cases {
			var out, diag bytes.Buffer
			code := Run(context.Background(), tc.args, &out, &diag, terminal)
			if code != 2 || out.Len() != 0 || strings.HasPrefix(diag.String(), "{") != tc.json {
				t.Fatal(tc.args, code, out.String(), diag.String())
			}
		}
	}
	for _, args := range [][]string{{"list", "--help"}, {"list", "--all", "--help"}} {
		var out, diag bytes.Buffer
		if code := Run(context.Background(), args, &out, &diag, false); code != 0 || !strings.Contains(out.String(), "--directory") || !strings.Contains(out.String(), "--all") {
			t.Fatal(code, out.String(), diag.String())
		}
	}
	var diag bytes.Buffer
	if code := Run(context.Background(), []string{"list"}, failedWriter{}, &diag, false); code != 1 || !strings.Contains(diag.String(), "runtime_error") {
		t.Fatal(code, diag.String())
	}
	// Explicit malformed global path flags remain usage errors even for list.
	var out bytes.Buffer
	diag.Reset()
	if code := Run(context.Background(), []string{"list", "--config-dir=relative"}, &out, &diag, false); code != 2 || out.Len() != 0 {
		t.Fatal(code, out.String(), diag.String())
	}
}

func TestListTableControlIdentities(t *testing.T) {
	entries := []booking.Entry{{Directory: "/owner\nwith\ttabs", Service: "server\n\t\x1b", Port: 123}}
	for _, all := range []bool{false, true} {
		var out bytes.Buffer
		if err := renderList(&out, "table", entries, all); err != nil {
			t.Fatal(err)
		}
		if strings.Count(out.String(), "\n") != 2 || !strings.Contains(out.String(), `"server\n\t\x1b"`) {
			t.Fatal(out.String())
		}
		if all && !strings.Contains(out.String(), `"/owner\nwith\ttabs"`) {
			t.Fatal(out.String())
		}
	}
	var out bytes.Buffer
	if err := renderList(&out, "json", entries, true); err != nil {
		t.Fatal(err)
	}
	var result struct{ Bookings []booking.Entry }
	if err := json.Unmarshal(out.Bytes(), &result); err != nil || !reflect.DeepEqual(entries, result.Bookings) {
		t.Fatal(out.String(), err)
	}
}

func TestListTableIdentitiesRemainDistinct(t *testing.T) {
	names := []string{"server\nname", `"server\nname"`, "line\u2028separator", "right\u202eto-left", "plain", "雪"}
	seen := map[string]string{}
	for _, name := range names {
		cell := listCell(name)
		if old, ok := seen[cell]; ok {
			t.Fatalf("distinct identities %q and %q both display as %q", old, name, cell)
		}
		seen[cell] = name
		if strings.ContainsAny(cell, "\n\r\t\x1b\u2028\u202e") {
			t.Fatalf("unescaped terminal content %q", cell)
		}
		if strings.HasPrefix(cell, `"`) {
			decoded, err := strconv.Unquote(cell)
			if err != nil || decoded != name {
				t.Fatal(cell, decoded, err)
			}
		} else if cell != name {
			t.Fatal(cell, name)
		}
	}
}

func TestListTablePreservesBoundarySpaces(t *testing.T) {
	var plain, spaced bytes.Buffer
	if err := renderList(&plain, "table", []booking.Entry{{Service: "server", Port: 123}}, false); err != nil {
		t.Fatal(err)
	}
	if err := renderList(&spaced, "table", []booking.Entry{{Service: "server ", Port: 123}}, false); err != nil {
		t.Fatal(err)
	}
	if plain.String() == spaced.String() {
		t.Fatalf("table hides trailing space in identity: %q", plain.String())
	}
	for _, name := range []string{" server", "server ", " "} {
		cell := listCell(name)
		decoded, err := strconv.Unquote(cell)
		if err != nil || decoded != name {
			t.Fatalf("boundary spaces not preserved: %q => %q (%v)", name, cell, err)
		}
	}
}
