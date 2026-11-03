package arcade

import "testing"

// Captured from a live BSV Broadcaster on the teranode multinode stack (2026-11-03): tx
// f9808a6e… mined in block 1102, whose header merkle root was 5e56ad8d….
const (
	proofTxID = "f9808a6e504b8b73157e76dbcace710b31cef6e63c4c044af64bd4beda396cb0"
	proofBUMP = "fd4e0401020102b06c39dabed44bf64a044c3ce6f6ce310b71cecadb767e15738b4b506e8a80f900007b5a0e56f6d10be812d909d9d2d3aef8678adffddc566a737745b0671eaf439c"
	proofRoot = "5e56ad8da6ce15477a19c7ee2217f25a27328064fcfdb25e333af0b745656b2e"
)

func TestProofRoot(t *testing.T) {
	root, height, err := ProofRoot(proofBUMP, proofTxID)
	if err != nil {
		t.Fatal(err)
	}
	if root != proofRoot || height != 1102 {
		t.Fatalf("got root %s at height %d, want %s at 1102", root, height, proofRoot)
	}
}

func TestProofRootRejectsOtherTx(t *testing.T) {
	other := "cb5487fe3c2a60ad715a87bbe30ce29c495bcbcd2b6d3f02ce8e3f100ee8ea25"
	if root, _, err := ProofRoot(proofBUMP, other); err == nil && root == proofRoot {
		t.Fatalf("a path for %s proved %s", proofTxID, other)
	}
}

func TestProofRootRejectsGarbage(t *testing.T) {
	if _, _, err := ProofRoot("zz", proofTxID); err == nil {
		t.Fatal("decoded a non-hex merklePath")
	}
}
