package ports

import (
	"reflect"
	"testing"
)

func TestSetOperations(t *testing.T) {
	var p Pool
	steps := []struct {
		input          []Range
		add            bool
		changed, total int
		want           []Range
	}{
		{[]Range{{50000, 50099}}, true, 100, 100, []Range{{50000, 50099}}},
		{[]Range{{50050, 50149}}, true, 50, 150, []Range{{50000, 50149}}},
		{[]Range{{50040, 50059}}, false, 20, 130, []Range{{50000, 50039}, {50060, 50149}}},
		{[]Range{{50149, 50149}}, false, 1, 129, []Range{{50000, 50039}, {50060, 50148}}},
		{[]Range{{50000, 50000}}, true, 0, 129, []Range{{50000, 50039}, {50060, 50148}}},
		{[]Range{{50040, 50059}}, false, 0, 129, []Range{{50000, 50039}, {50060, 50148}}},
		{[]Range{{50020, 50080}}, false, 41, 88, []Range{{50000, 50019}, {50081, 50148}}},
		{[]Range{{1, 65535}}, false, 88, 0, []Range{}},
		{[]Range{{65535, 65535}, {1, 1}}, true, 2, 2, []Range{{1, 1}, {65535, 65535}}},
		{[]Range{{2, 65534}}, true, 65533, 65535, []Range{{1, 65535}}},
		{[]Range{{1, 65535}}, true, 0, 65535, []Range{{1, 65535}}},
		{[]Range{{1, 65535}}, false, 65535, 0, []Range{}},
	}
	for i, s := range steps {
		if n := p.Apply(s.input, s.add); n != s.changed {
			t.Fatalf("step %d changed %d, want %d", i, n, s.changed)
		}
		if n := p.Count(); n != s.total {
			t.Fatalf("step %d count %d, want %d", i, n, s.total)
		}
		if got := p.Ranges(); !reflect.DeepEqual(got, s.want) {
			t.Fatalf("step %d ranges %v, want %v", i, got, s.want)
		}
	}
}

func TestNormalization(t *testing.T) {
	p, err := FromRanges([]Range{{20, 30}, {5, 9}, {10, 15}, {8, 11}, {20, 30}, {16, 19}, {50, 50}})
	if err != nil {
		t.Fatal(err)
	}
	if got, want := p.Ranges(), ([]Range{{5, 30}, {50, 50}}); !reflect.DeepEqual(got, want) {
		t.Fatalf("%v != %v", got, want)
	}
	if p.Count() != 27 {
		t.Fatal(p.Count())
	}
	if _, err := FromRanges([]Range{{1, 5}, {0, 2}}); err == nil {
		t.Fatal("accepted invalid stored range")
	}
}

func TestParse(t *testing.T) {
	for _, s := range []string{"1", "65535", "1-65535", "51000", "00001-00002"} {
		if _, err := Parse(s); err != nil {
			t.Errorf("%q: %v", s, err)
		}
	}
	for _, s := range []string{"", "0", "65536", "2-1", "-1", "+1", "1-", "-2", "1-2-3", "1:2", "1,2", " 1", "1 ", "1 -2", "1.0", "١", "9999999999999999999999999999", "0-65535"} {
		if _, err := Parse(s); err == nil {
			t.Errorf("accepted %q", s)
		}
	}
}
