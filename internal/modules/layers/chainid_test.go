package layers

import (
	"crypto/sha256"
	"encoding/hex"
	"testing"

	"github.com/opencontainers/go-digest"
	"github.com/opencontainers/image-spec/identity"
)

func TestChainIDsMatchesOCIReference(t *testing.T) {
	diffIDs := []string{
		"sha256:08000c18d16dadf9553d747a58cf44023423a9ab010aab96cf263d2216b8b350",
		"sha256:d71eae0084c1a3e15f1ba4e2d8b1ce8f3ae4d4a2ac2e3a0e0b9b3b2f5e7fd2a1",
		"sha256:08000c18d16dadf9553d747a58cf44023423a9ab010aab96cf263d2216b8b350",
		"sha256:c56f134d3805d7e3a5e5d8e6a0d6b0e8c5a5b7c8d9e0f1a2b3c4d5e6f7a8b9c0",
	}

	got := ChainIDs(diffIDs)

	want := make([]digest.Digest, len(diffIDs))
	for i, d := range diffIDs {
		want[i] = digest.Digest(d)
	}
	identity.ChainIDs(want)

	if len(got) != len(want) {
		t.Fatalf("got %d chain IDs, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i].String() {
			t.Errorf("layer %d: got %s, want %s", i+1, got[i], want[i])
		}
	}
}

func TestChainIDsDefinition(t *testing.T) {
	d1 := "sha256:1111111111111111111111111111111111111111111111111111111111111111"
	d2 := "sha256:2222222222222222222222222222222222222222222222222222222222222222"

	got := ChainIDs([]string{d1, d2})

	sum := sha256.Sum256([]byte(d1 + " " + d2))
	want2 := "sha256:" + hex.EncodeToString(sum[:])
	if got[0] != d1 {
		t.Errorf("ChainID(L1) = %s, want DiffID(L1) %s", got[0], d1)
	}
	if got[1] != want2 {
		t.Errorf("ChainID(L2) = %s, want %s", got[1], want2)
	}
}

func TestChainIDsEmpty(t *testing.T) {
	if got := ChainIDs(nil); len(got) != 0 {
		t.Errorf("ChainIDs(nil) = %v, want empty", got)
	}
}
