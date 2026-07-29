package protocol

import "testing"

func TestAICHHashFromStringHexAndBase32(t *testing.T) {
	t.Parallel()
	hexValue := "A9993E364706816ABA3E25717850C26C9CD0D89D"
	fromHex, err := AICHHashFromString(hexValue)
	if err != nil {
		t.Fatalf("hex parse: %v", err)
	}
	fromBase32, err := AICHHashFromString(fromHex.Base32())
	if err != nil {
		t.Fatalf("base32 parse: %v", err)
	}
	if !fromHex.Equal(fromBase32) {
		t.Fatalf("hex %s != base32 %s", fromHex.String(), fromBase32.String())
	}
}

func TestCombineAICHHashes(t *testing.T) {
	t.Parallel()
	left := AICHHashFromSHA1([]byte("left"))
	right := AICHHashFromSHA1([]byte("right"))
	combined := CombineAICHHashes(left, right)
	if combined.IsZero() {
		t.Fatal("expected non-zero combined hash")
	}
	if combined.Equal(left) || combined.Equal(right) {
		t.Fatal("combined hash should differ from leaves")
	}
}
