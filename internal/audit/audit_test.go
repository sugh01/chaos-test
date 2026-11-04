package audit

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/bsv-blockchain/chaos-test/internal/arcade"
)

// fakeNode serves getbestblockhash, getblockheader, getblockhash and getchaintips from an
// in-memory chain.
type fakeNode struct {
	byHash   map[string]header
	byHeight map[uint32]string
	tip      string
	tips     []chainTip
}

func newChain(n uint32, fork uint32, tag string) *fakeNode {
	f := &fakeNode{byHash: map[string]header{}, byHeight: map[uint32]string{}}
	prev := ""
	for h := uint32(0); h <= n; h++ {
		id := fmt.Sprintf("%064d", h)
		if fork > 0 && h >= fork {
			id = fmt.Sprintf("%s%060d", tag, h)
		}
		f.byHash[id] = header{Hash: id, Height: h, PrevHash: prev, MerkleRoot: "root-" + id}
		f.byHeight[h] = id
		prev, f.tip = id, id
	}
	f.tips = []chainTip{{Height: n, Status: "active"}}
	return f
}

func (f *fakeNode) Call(_ context.Context, method string, params []any, out any) error {
	var v any
	switch method {
	case "getbestblockhash":
		v = f.tip
	case "getblockheader":
		h, ok := f.byHash[params[0].(string)]
		if !ok {
			return fmt.Errorf("block not found")
		}
		v = h
	case "getblockhash":
		v = f.byHeight[params[0].(uint32)]
	case "getchaintips":
		v = f.tips
	default:
		return fmt.Errorf("unexpected %s", method)
	}
	b, _ := json.Marshal(v)
	return json.Unmarshal(b, out)
}

func TestChainAgrees(t *testing.T) {
	nodes := []Node{{"a", newChain(20, 0, "")}, {"b", newChain(20, 0, "")}, {"sv1", newChain(20, 0, "")}}
	r, err := Chain(context.Background(), nodes, 0, func(_ context.Context, h uint32) (string, error) {
		return fmt.Sprintf("%064d", h), nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if r.Divergences != 0 || r.Heights != 21 || r.To != 20 {
		t.Fatalf("want 21 agreeing heights, got %s", r.Summary())
	}
}

func TestChainDivergenceNamesHeightAndNode(t *testing.T) {
	nodes := []Node{{"a", newChain(20, 0, "")}, {"c", newChain(20, 15, "cccc")}}
	r, err := Chain(context.Background(), nodes, 0, nil)
	if err != nil {
		t.Fatal(err)
	}
	if r.Divergences != 6 {
		t.Fatalf("want 6 divergent heights (15-20), got %s", r.Summary())
	}
	if !strings.Contains(r.Listed[0], "height 15") || !strings.Contains(r.Listed[0], "c has") {
		t.Fatalf("first divergence should name height 15 and node c: %s", r.Listed[0])
	}
}

func TestChainLowestTipAndStaleForks(t *testing.T) {
	behind := newChain(18, 0, "")
	behind.tips = append(behind.tips, chainTip{Height: 12, BranchLen: 3, Status: "valid-fork"})
	r, err := Chain(context.Background(), []Node{{"a", newChain(20, 0, "")}, {"b", behind}}, 0, nil)
	if err != nil {
		t.Fatal(err)
	}
	if r.To != 20 || r.Divergences != 0 || r.StaleTips != 1 || r.DeepestFork != 3 {
		t.Fatalf("want audit to the highest tip with one stale fork of 3, got %s", r.Summary())
	}
}

// A lagging node must not hide a divergence between the others above its tip (seen on the
// multinode stack: B one block behind while A and C split at the next height).
func TestChainLaggingNodeDoesNotHideDivergence(t *testing.T) {
	nodes := []Node{{"a", newChain(5, 0, "")}, {"b", newChain(4, 0, "")}, {"c", newChain(5, 5, "cccc")}}
	r, err := Chain(context.Background(), nodes, 0, nil)
	if err != nil {
		t.Fatal(err)
	}
	if r.Divergences != 1 || !strings.Contains(r.Listed[0], "height 5") {
		t.Fatalf("want the split at height 5 found despite b at 4, got %s", r.Summary())
	}
}

func TestChainBroadcasterDisagrees(t *testing.T) {
	r, err := Chain(context.Background(), []Node{{"a", newChain(5, 0, "")}}, 0, func(_ context.Context, h uint32) (string, error) {
		if h == 3 {
			return "other", nil
		}
		return fmt.Sprintf("%064d", h), nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if r.Divergences != 1 || !strings.Contains(r.Listed[0], "broadcaster") {
		t.Fatalf("want one broadcaster divergence, got %s", r.Summary())
	}
}

// Captured on the multinode stack (2026-11-03): tx f9808a6e… mined in block 1102.
const (
	txID      = "f9808a6e504b8b73157e76dbcace710b31cef6e63c4c044af64bd4beda396cb0"
	bump      = "fd4e0401020102b06c39dabed44bf64a044c3ce6f6ce310b71cecadb767e15738b4b506e8a80f900007b5a0e56f6d10be812d909d9d2d3aef8678adffddc566a737745b0671eaf439c"
	root      = "5e56ad8da6ce15477a19c7ee2217f25a27328064fcfdb25e333af0b745656b2e"
	blockHash = "35e429e778b2d44fc4944c2e479daeb2c54303a676853bf46bb87ea4742467bd"
)

func nodeWithBlock(hash, merkleRoot string) *fakeNode {
	return &fakeNode{
		byHash:   map[string]header{hash: {Hash: hash, Height: 1102, MerkleRoot: merkleRoot}},
		byHeight: map[uint32]string{1102: hash},
	}
}

func getter(recs map[string]*arcade.TxRecord) GetTx {
	return func(_ context.Context, id string) (*arcade.TxRecord, error) {
		if r, ok := recs[id]; ok {
			return r, nil
		}
		return nil, arcade.ErrTxNotFound
	}
}

func TestProofs(t *testing.T) {
	submitted := time.Date(2026, 11, 3, 8, 3, 50, 0, time.UTC)
	recs := map[string]*arcade.TxRecord{
		txID:      {TxID: txID, Status: arcade.StatusMined, BlockHash: blockHash, MerklePath: bump, Timestamp: submitted.Add(10 * time.Second).Format(time.RFC3339Nano)},
		"pending": {TxID: "pending", Status: arcade.StatusSeenOnNetwork},
	}
	good := []Node{{"a", nodeWithBlock(blockHash, root)}, {"sv1", nodeWithBlock(blockHash, root)}}
	r, err := Proofs(context.Background(), []Tx{{ID: txID, Submitted: submitted}, {ID: "pending"}, {ID: "gone"}}, getter(recs), good)
	if err != nil {
		t.Fatal(err)
	}
	if r.Proven != 1 || r.Unproven != 2 || r.Mismatched != 0 || r.LatencyMax != "10s" {
		t.Fatalf("want 1 proven (10s), 2 unproven, got %s", r.Summary())
	}
}

func TestProofsMismatchOnOneNode(t *testing.T) {
	recs := map[string]*arcade.TxRecord{txID: {TxID: txID, Status: arcade.StatusMined, BlockHash: blockHash, MerklePath: bump}}
	cases := map[string]*fakeNode{
		"wrong root":          nodeWithBlock(blockHash, strings.Repeat("0", 64)),
		"other block at 1102": nodeWithBlock(strings.Repeat("e", 64), root),
	}
	for name, sv := range cases {
		nodes := []Node{{"a", nodeWithBlock(blockHash, root)}, {"sv1", sv}}
		r, err := Proofs(context.Background(), []Tx{{ID: txID}}, getter(recs), nodes)
		if err != nil {
			t.Fatal(err)
		}
		if r.Mismatched != 1 || !strings.Contains(r.Listed[0], "sv1") {
			t.Fatalf("%s: want a mismatch naming sv1, got %s", name, r.Summary())
		}
	}
}
