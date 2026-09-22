package labels

import "testing"

func TestProtectiveFailsSafe(t *testing.T) {
	tests := map[string]bool{
		"true": true, "1": true, "True": true,
		"yes": true, "": true, "on": true, // not booleans: still protect
		"false": false, "0": false, "FALSE": false,
	}
	for value, want := range tests {
		if got := Protective(map[string]string{Protected: value}, Protected); got != want {
			t.Errorf("Protective(%q) = %t, want %t", value, got, want)
		}
	}
	if Protective(map[string]string{}, Protected) {
		t.Error("absent label must not protect")
	}
}

func TestEnabledIsStrict(t *testing.T) {
	tests := map[string]bool{
		"true": true, "1": true, "TRUE": true,
		"yes": false, "": false, "false": false,
	}
	for value, want := range tests {
		if got := Enabled(map[string]string{Temporary: value}, Temporary); got != want {
			t.Errorf("Enabled(%q) = %t, want %t", value, got, want)
		}
	}
	if Enabled(nil, Temporary) {
		t.Error("absent label must not be enabled")
	}
}
