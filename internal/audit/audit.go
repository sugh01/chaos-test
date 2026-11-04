// Package audit checks chain integrity from outside the nodes, through their RPC: every node
// must hold the same chain, and every merkle proof the broadcaster hands out must hold on every
// node. It reads nothing from node stores, so teranodes and SV nodes are audited the same way.
package audit

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/bsv-blockchain/chaos-test/internal/arcade"
)

// maxListed caps the divergences and failures a report spells out; the counts stay exact.
const maxListed = 10

// Caller is a Bitcoin-style JSON-RPC client (teranode and bitcoin-sv speak the same wire format).
type Caller interface {
	Call(ctx context.Context, method string, params []any, out any) error
}

// Node is one chain participant to audit.
type Node struct {
	Name string
	RPC  Caller
}

type header struct {
	Hash       string `json:"hash"`
	Height     uint32 `json:"height"`
	PrevHash   string `json:"previousblockhash"`
	MerkleRoot string `json:"merkleroot"`
}

type chainTip struct {
	Height    uint32 `json:"height"`
	BranchLen uint32 `json:"branchlen"`
	Status    string `json:"status"`
}

// Canonical returns the block hash a third party (the broadcaster's chaintracks) holds at a
// height.
type Canonical func(ctx context.Context, height uint32) (string, error)

// ChainReport is the result of Chain.
type ChainReport struct {
	From        uint32            `json:"from"`
	To          uint32            `json:"to"`
	Heights     int               `json:"heights"`
	Nodes       []string          `json:"nodes"`
	Tips        map[string]uint32 `json:"tips"`
	Divergences int               `json:"divergences"`
	Listed      []string          `json:"listed,omitempty"`
	StaleTips   int               `json:"staleTips"`
	DeepestFork uint32            `json:"deepestFork"`
}

func (r *ChainReport) diverge(format string, a ...any) {
	r.Divergences++
	if len(r.Listed) < maxListed {
		r.Listed = append(r.Listed, fmt.Sprintf(format, a...))
	}
}

// Summary is a one-line account of the report.
func (r *ChainReport) Summary() string {
	s := fmt.Sprintf("%d heights (%d-%d) on %d nodes, %d divergence(s), %d stale tip(s), deepest fork %d",
		r.Heights, r.From, r.To, len(r.Nodes), r.Divergences, r.StaleTips, r.DeepestFork)
	if len(r.Listed) > 0 {
		s += ": " + strings.Join(r.Listed, "; ")
	}
	return s
}

// Chain walks each node's chain from its tip back to height from, following previousblockhash
// (which also proves each chain is linked), and requires every node, and canonical when
// given, to hold the same block hash and merkle root at every height up to the lowest tip.
// Stale tips from getchaintips are counted, not treated as divergence: a resolved fork is
// normal; disagreement about the active chain is not.
func Chain(ctx context.Context, nodes []Node, from uint32, canonical Canonical) (*ChainReport, error) {
	if len(nodes) == 0 {
		return nil, errors.New("no nodes to audit")
	}
	r := &ChainReport{From: from, Tips: map[string]uint32{}}
	chains := make([]map[uint32]header, len(nodes))
	to := ^uint32(0)
	for i, n := range nodes {
		r.Nodes = append(r.Nodes, n.Name)
		chain, tip, err := walk(ctx, n, from, r)
		if err != nil {
			return nil, err
		}
		chains[i], r.Tips[n.Name] = chain, tip
		to = min(to, tip)
		if err := forks(ctx, n, r); err != nil {
			return nil, err
		}
	}
	if to < from {
		return r, nil
	}
	r.To = to
	for h := from; h <= to; h++ {
		r.Heights++
		ref := chains[0][h]
		for i := 1; i < len(nodes); i++ {
			got := chains[i][h]
			if got.Hash != ref.Hash || got.MerkleRoot != ref.MerkleRoot {
				r.diverge("height %d: %s has %s (root %s), %s has %s (root %s)", h,
					nodes[0].Name, short(ref.Hash), short(ref.MerkleRoot), nodes[i].Name, short(got.Hash), short(got.MerkleRoot))
			}
		}
		if canonical != nil {
			c, err := canonical(ctx, h)
			if err != nil {
				return nil, fmt.Errorf("canonical hash at %d: %w", h, err)
			}
			if c != ref.Hash {
				r.diverge("height %d: broadcaster has %s, %s has %s", h, short(c), nodes[0].Name, short(ref.Hash))
			}
		}
	}
	return r, nil
}

func walk(ctx context.Context, n Node, from uint32, r *ChainReport) (map[uint32]header, uint32, error) {
	var hash string
	if err := n.RPC.Call(ctx, "getbestblockhash", nil, &hash); err != nil {
		return nil, 0, fmt.Errorf("%s: getbestblockhash: %w", n.Name, err)
	}
	chain := map[uint32]header{}
	var tip uint32
	for first := true; ; first = false {
		var h header
		if err := n.RPC.Call(ctx, "getblockheader", []any{hash, true}, &h); err != nil {
			return nil, 0, fmt.Errorf("%s: getblockheader %s: %w", n.Name, short(hash), err)
		}
		if first {
			tip = h.Height
		} else if want := tip - uint32(len(chain)); h.Height != want {
			r.diverge("%s: block %s at height %d is the parent of height %d", n.Name, short(h.Hash), h.Height, want+1)
			return chain, tip, nil
		}
		chain[h.Height] = h
		if h.Height <= from || h.PrevHash == "" {
			return chain, tip, nil
		}
		hash = h.PrevHash
	}
}

func forks(ctx context.Context, n Node, r *ChainReport) error {
	var tips []chainTip
	if err := n.RPC.Call(ctx, "getchaintips", nil, &tips); err != nil {
		return fmt.Errorf("%s: getchaintips: %w", n.Name, err)
	}
	for _, t := range tips {
		if t.Status == "active" {
			continue
		}
		r.StaleTips++
		r.DeepestFork = max(r.DeepestFork, t.BranchLen)
	}
	return nil
}

// Tx is a transaction to audit: its id and, when known, when it was submitted.
type Tx struct {
	ID        string
	Submitted time.Time
}

// GetTx reads the broadcaster's record of a transaction.
type GetTx func(ctx context.Context, txid string) (*arcade.TxRecord, error)

// ProofReport is the result of Proofs.
type ProofReport struct {
	Checked    int      `json:"checked"`
	Proven     int      `json:"proven"`
	Unproven   int      `json:"unproven"`
	Mismatched int      `json:"mismatched"`
	Listed     []string `json:"listed,omitempty"`
	LatencyP50 string   `json:"latencyP50,omitempty"`
	LatencyP95 string   `json:"latencyP95,omitempty"`
	LatencyMax string   `json:"latencyMax,omitempty"`
}

// ProvenRatio is the share of checked transactions with a proof that held everywhere.
func (r *ProofReport) ProvenRatio() float64 {
	if r.Checked == 0 {
		return 0
	}
	return float64(r.Proven) / float64(r.Checked)
}

// Summary is a one-line account of the report.
func (r *ProofReport) Summary() string {
	s := fmt.Sprintf("%d tx checked: %d proven, %d unproven, %d mismatched", r.Checked, r.Proven, r.Unproven, r.Mismatched)
	if r.LatencyP50 != "" {
		s += fmt.Sprintf("; submit to MINED p50 %s, p95 %s, max %s", r.LatencyP50, r.LatencyP95, r.LatencyMax)
	}
	if len(r.Listed) > 0 {
		s += ": " + strings.Join(r.Listed, "; ")
	}
	return s
}

func (r *ProofReport) fail(mismatch bool, format string, a ...any) {
	if mismatch {
		r.Mismatched++
	} else {
		r.Unproven++
	}
	if len(r.Listed) < maxListed {
		r.Listed = append(r.Listed, fmt.Sprintf(format, a...))
	}
}

// Proofs requires each transaction to be MINED (or IMMUTABLE) with a merkle path that proves
// it against the header merkle root, at the same height, of the block every node holds at that
// height. Unproven means the broadcaster has no proof yet; mismatched means the proof it hands
// out is wrong for at least one node, which is an integrity failure.
func Proofs(ctx context.Context, txs []Tx, get GetTx, nodes []Node) (*ProofReport, error) {
	r := &ProofReport{}
	type key struct {
		node   string
		height uint32
	}
	canon := map[key]header{}
	var lat []time.Duration
	for _, tx := range txs {
		r.Checked++
		rec, err := get(ctx, tx.ID)
		switch {
		case errors.Is(err, arcade.ErrTxNotFound):
			r.fail(false, "%s: broadcaster has no record", short(tx.ID))
			continue
		case err != nil:
			return nil, fmt.Errorf("get %s: %w", short(tx.ID), err)
		case !arcade.IsGood(rec.Status):
			r.fail(false, "%s: %s", short(tx.ID), rec.Status)
			continue
		case rec.MerklePath == "":
			r.fail(false, "%s: %s without a merklePath", short(tx.ID), rec.Status)
			continue
		}
		root, height, err := arcade.ProofRoot(rec.MerklePath, tx.ID)
		if err != nil {
			r.fail(true, "%s: %v", short(tx.ID), err)
			continue
		}
		bad := ""
		for _, n := range nodes {
			k := key{n.Name, height}
			h, ok := canon[k]
			if !ok {
				if h, err = canonicalHeader(ctx, n, height); err != nil {
					return nil, err
				}
				canon[k] = h
			}
			if h.Hash != rec.BlockHash || h.MerkleRoot != root {
				bad = fmt.Sprintf("%s: proof for block %s at %d proves root %s, %s has %s (root %s) there",
					short(tx.ID), short(rec.BlockHash), height, short(root), n.Name, short(h.Hash), short(h.MerkleRoot))
				break
			}
		}
		if bad != "" {
			r.fail(true, "%s", bad)
			continue
		}
		r.Proven++
		if mined, err := time.Parse(time.RFC3339Nano, rec.Timestamp); err == nil && !tx.Submitted.IsZero() && mined.After(tx.Submitted) {
			lat = append(lat, mined.Sub(tx.Submitted))
		}
	}
	if len(lat) > 0 {
		sort.Slice(lat, func(i, j int) bool { return lat[i] < lat[j] })
		pick := func(p float64) string { return lat[int(p*float64(len(lat)-1))].Round(time.Millisecond).String() }
		r.LatencyP50, r.LatencyP95, r.LatencyMax = pick(0.5), pick(0.95), slices.Max(lat).Round(time.Millisecond).String()
	}
	return r, nil
}

func canonicalHeader(ctx context.Context, n Node, height uint32) (header, error) {
	var hash string
	if err := n.RPC.Call(ctx, "getblockhash", []any{height}, &hash); err != nil {
		return header{}, fmt.Errorf("%s: getblockhash %d: %w", n.Name, height, err)
	}
	var h header
	if err := n.RPC.Call(ctx, "getblockheader", []any{hash, true}, &h); err != nil {
		return header{}, fmt.Errorf("%s: getblockheader %s: %w", n.Name, short(hash), err)
	}
	return h, nil
}

func short(h string) string {
	if len(h) > 12 {
		return h[:12] + "…"
	}
	return h
}
