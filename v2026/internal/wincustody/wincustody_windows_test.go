//go:build windows

package wincustody

import (
	"testing"

	"golang.org/x/sys/windows"
)

func TestValidName(t *testing.T) {
	for name, valid := range map[string]bool{
		"jwt":                       true,
		".provider.key":             true,
		".registration-0123abcd":    true,
		"jwt.registration.lock":     true,
		"jwt.rejected-1700000000-1": true,
		"":                          false,
		".":                         false,
		"..":                        false,
		"jwt:stream":                false,
		"jwt.":                      false,
		"jwt ":                      false,
		`a\b`:                       false,
		"a/b":                       false,
		"NUL":                       false,
		"nul.txt":                   false,
		"COM1":                      false,
		"lpt9.log":                  false,
		"COM¹":                      false,
		"CONOUT$":                   false,
		"console":                   true,
		"COM":                       true,
		"jwt\x00":                   false,
		"a*":                        false,
	} {
		if got := ValidName(name); got != valid {
			t.Errorf("ValidName(%q) = %v, want %v", name, got, valid)
		}
	}
}

// The descriptor decision itself, on descriptors Windows would hold. It is
// the part of custody an emulator cannot exercise through real files.
// directory here is the readable (mode&022) rule.
func TestDescriptorPrivate(t *testing.T) {
	user, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		t.Fatal(err)
	}
	me := user.User.Sid.String()
	profile := "D:(A;OICI;FA;;;SY)(A;OICI;FA;;;BA)(A;OICI;FA;;;" + me + ")"
	for _, test := range []struct {
		name      string
		sddl      string
		directory bool
		private   bool
	}{
		{"profile directory", "O:" + me + profile, true, true},
		{"profile file", "O:" + me + profile, false, true},
		{"store created", "O:" + me + "D:P(A;OICI;FA;;;" + me + ")(A;OICI;FA;;;SY)(A;OICI;FA;;;BA)", true, true},
		{"elevated owner", "O:BA" + profile, false, true},
		{"system owner", "O:SY" + profile, true, true},
		{"foreign owner", "O:S-1-5-21-1-2-3-1001" + profile, true, false},
		{"foreign owner file", "O:S-1-5-21-1-2-3-1001" + profile, false, false},
		{"empty dacl", "O:" + me + "D:P", false, true},
		{"users read directory", "O:" + me + profile + "(A;;0x1200a9;;;BU)", true, true},
		{"users read file", "O:" + me + profile + "(A;;FR;;;BU)", false, false},
		{"users synchronize file", "O:" + me + profile + "(A;;0x100000;;;BU)", false, false},
		{"everyone add file", "O:" + me + profile + "(A;;0x2;;;WD)", true, false},
		{"everyone delete child", "O:" + me + profile + "(A;;0x40;;;WD)", true, false},
		{"everyone write dac", "O:" + me + profile + "(A;;WD;;;WD)", true, false},
		{"everyone generic all", "O:" + me + profile + "(A;;GA;;;WD)", true, false},
		{"everyone generic write", "O:" + me + profile + "(A;;GW;;;WD)", true, false},
		{"authenticated users modify", "O:" + me + profile + "(A;OICI;0x1301bf;;;AU)", true, false},
		{"other user full", "O:" + me + profile + "(A;;FA;;;S-1-5-21-1-2-3-1001)", false, false},
		{"inherit-only for files", "O:" + me + profile + "(A;OICIIO;FA;;;WD)", true, false},
		{"users read inherited by files", "O:" + me + profile + "(A;OICI;0x1200a9;;;BU)", true, false},
		{"inherit-only for subdirectories", "O:" + me + profile + "(A;CIIO;FA;;;WD)", true, true},
		{"deny everyone", "O:" + me + profile + "(D;;FA;;;WD)", false, true},
		{"owner rights", "O:" + me + profile + "(A;;FA;;;OW)", false, true},
	} {
		sd, err := windows.SecurityDescriptorFromString(test.sddl)
		if err != nil {
			t.Fatalf("%s: %v", test.name, err)
		}
		err = DescriptorPrivate(sd, test.directory)
		if (err == nil) != test.private {
			t.Errorf("%s: private = %v (%v), want %v", test.name, err == nil, err, test.private)
		}
	}
}

// A present but null DACL grants everyone everything.
func TestDescriptorRefusesNullDacl(t *testing.T) {
	user, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		t.Fatal(err)
	}
	sd, err := windows.NewSecurityDescriptor()
	if err != nil {
		t.Fatal(err)
	}
	if err := sd.SetOwner(user.User.Sid, false); err != nil {
		t.Fatal(err)
	}
	if err := sd.SetDACL(nil, true, false); err != nil {
		t.Fatal(err)
	}
	for _, directory := range []bool{true, false} {
		if err := DescriptorPrivate(sd, directory); err == nil {
			t.Errorf("a null DACL was private (directory %v)", directory)
		}
	}
}
