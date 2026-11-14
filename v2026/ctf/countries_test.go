package ctf

import (
	"crypto/sha256"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strconv"
	"testing"
)

// §4.1: L is the 249 ISO 3166-1 codes, lowercase, ascending, with the seven
// territories without a permanent population and without Kosovo.
func TestCountryList(t *testing.T) {
	all := Countries()
	if len(all) != NumCountries || NumCountries != 249 || NumSlots != 2490 || NumPots != 498 {
		t.Fatalf("|L| %d, S %d, pots %d", len(all), NumSlots, NumPots)
	}
	for i, c := range all {
		if c.Index() != i || CountryAt(i) != c || !c.InL() || c.Name() == "" {
			t.Fatalf("country %d %v: index %d name %q", i, c, c.Index(), c.Name())
		}
		if i > 0 && all[i-1].String() >= c.String() {
			t.Fatalf("%v follows %v", c, all[i-1])
		}
	}
	for _, code := range []string{"aq", "bv", "gs", "hm", "io", "tf", "um", "kp", "tm", "de", "is", "us", "ad", "zw"} {
		if _, ok := ParseCountry(code); !ok {
			t.Fatalf("%s is not in L", code)
		}
	}
	for _, code := range []string{"xk", "zz", "uk", "", "d", "deu", "d1", "é"} {
		if c, ok := ParseCountry(code); ok {
			t.Fatalf("%q parsed as %v", code, c)
		}
	}
	if CountryUnknown.InL() || CountryUnknown.Name() != "" || (Country{'X', 'K'}).InL() || (Country{0, 0}).Index() != -1 {
		t.Fatal("a code outside L is in L")
	}
	if c, ok := ParseCountry("DE"); !ok || c != cc("de") || c.String() != "de" || c.Name() != "Germany" {
		t.Fatalf("DE parsed as %v %v", c, ok)
	}
	if cc("ad").Index() != 0 || cc("zw").Index() != NumCountries-1 || cc("ba").Index() != 16 || cc("bb").Index() != 17 {
		t.Fatal("positions in L")
	}
}

// §5: the countries object is the 249 codes in order, which every header
// names; its reference is pinned.
func TestCountriesObject(t *testing.T) {
	object := CountriesObject()
	if len(object) != 2*NumCountries || string(object[:4]) != "adae" || string(object[len(object)-2:]) != "zw" {
		t.Fatalf("countries object %q", object)
	}
	if CountriesRef != sha256.Sum256(object) {
		t.Fatal("CountriesRef is not the object's sha256")
	}
	object[0] = 'x'
	if CountriesObject()[0] != 'a' {
		t.Fatal("CountriesObject returned the package's own bytes")
	}
	const want = "sha256:1dc01d610882808f4f3c56e8025d6fdac9d2b1cb8ca12c0458f4a367d2a17986"
	if CountriesRef.String() != want {
		t.Fatalf("CountriesRef %v, pinned %s", CountriesRef, want)
	}
}

// §4.1: slots are ordered by country, then provider before extender, then
// rank; the §6 example's remainder of 172 rao goes to AD … BA and BB's
// provider ranks 1 and 2.
func TestSlotOrder(t *testing.T) {
	s := 0
	for c := range NumCountries {
		for _, role := range []Role{RoleProvider, RoleExtender} {
			j := PotIndex(c, role)
			if pc, pr := PotAt(j); pc != c || pr != role {
				t.Fatalf("pot %d is %d %v", j, pc, pr)
			}
			for rank := 1; rank <= Ranks; rank++ {
				if SlotIndex(c, role, rank) != s {
					t.Fatalf("slot (%v, %v, %d) is %d, not %d", CountryAt(c), role, rank, SlotIndex(c, role, rank), s)
				}
				s++
			}
		}
	}
	if s != NumSlots {
		t.Fatalf("%d slots", s)
	}
	bb := cc("bb").Index()
	if SlotIndex(cc("ba").Index(), RoleExtender, 5) != 169 || SlotIndex(bb, RoleProvider, 2) != 171 || SlotIndex(bb, RoleProvider, 3) != 172 {
		t.Fatal("the §6 remainder boundary")
	}
}

// L is the server's table of ISO 3166-1 names, code for code and name for
// name (server/model/iso_country_names.go), when the sibling checkout is
// present.
func TestCountriesMatchServer(t *testing.T) {
	path := filepath.Join("..", "..", "server", "model", "iso_country_names.go")
	if _, err := os.Stat(path); err != nil {
		t.Skipf("no server checkout: %v", err)
	}
	file, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	server := map[string]string{}
	ast.Inspect(file, func(n ast.Node) bool {
		spec, ok := n.(*ast.ValueSpec)
		if !ok || len(spec.Names) != 1 || spec.Names[0].Name != "isoCountryNames" {
			return true
		}
		for _, elt := range spec.Values[0].(*ast.CompositeLit).Elts {
			kv := elt.(*ast.KeyValueExpr)
			code, err1 := strconv.Unquote(kv.Key.(*ast.BasicLit).Value)
			name, err2 := strconv.Unquote(kv.Value.(*ast.BasicLit).Value)
			if err1 != nil || err2 != nil {
				t.Fatalf("entry %v", kv)
			}
			server[code] = name
		}
		return false
	})
	if len(server) != NumCountries {
		t.Fatalf("the server's table has %d codes", len(server))
	}
	for _, c := range Countries() {
		if name, ok := server[c.String()]; !ok || name != c.Name() {
			t.Fatalf("%v: server %q, ctf %q", c, name, c.Name())
		}
	}
}
