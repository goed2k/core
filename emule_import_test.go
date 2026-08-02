package goed2k

import (
	"testing"
)

func TestParseEmulePreferencesINI(t *testing.T) {
	text := `
Nick=alice
Port=4711
UDPPort=4712
MaxUpload=100
EnableDHT=1
TempDir=/tmp/emule
ServerIP=10.0.0.1
ServerPort=4661
`
	prefs, err := ParseEmulePreferencesINI(text)
	if err != nil {
		t.Fatal(err)
	}
	if prefs.NickName != "alice" || prefs.ListenPort != 4711 || prefs.UDPPort != 4712 {
		t.Fatalf("unexpected prefs: %+v", prefs)
	}
	if !prefs.EnableDHT || prefs.TempDir != "/tmp/emule" {
		t.Fatalf("dht/temp: %+v", prefs)
	}
}

func TestApplyEmulePreferencesSetsTempLayout(t *testing.T) {
	st := NewSettings()
	ApplyEmulePreferences(&st, EmulePreferences{TempDir: "/var/temp"})
	if !st.UseEmuleTempLayout {
		t.Fatal("expected UseEmuleTempLayout when TempDir set")
	}
}
