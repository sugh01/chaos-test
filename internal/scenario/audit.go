package scenario

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/bsv-blockchain/chaos-test/internal/audit"
	"github.com/bsv-blockchain/chaos-test/internal/walletsvc"
)

// auditNodes resolves `nodes` (roles or names; default the whole inventory, SV nodes
// included) to RPC clients for the audit package.
func (x *executor) auditNodes(w map[string]any) []audit.Node {
	f := x.e.d.Fleet
	var out []audit.Node
	for _, name := range nodes(x, w, "nodes") {
		if sv := f.SV(name); sv != nil {
			out = append(out, audit.Node{Name: name, RPC: sv.RPCClient})
		} else if rpc := f.RPC(name); rpc != nil {
			out = append(out, audit.Node{Name: name, RPC: rpc})
		}
	}
	return out
}

// auditTxs collects the transactions proof_audit checks: `txids`, or with `source: wallet`
// the wallet tracker's rows sent through the broadcaster and created at or after `since`.
func (x *executor) auditTxs(w map[string]any) ([]audit.Tx, error) {
	var txs []audit.Tx
	if ids, ok := w["txids"].([]any); ok {
		for _, id := range ids {
			txs = append(txs, audit.Tx{ID: fmt.Sprint(id)})
		}
	}
	if str(w, "source") != "wallet" {
		return txs, nil
	}
	var since time.Time
	if s := str(w, "since"); s != "" {
		t, err := time.Parse(time.RFC3339, s)
		if err != nil {
			return nil, fmt.Errorf("bad since: %w", err)
		}
		since = t
	}
	rows, err := x.e.d.API.WalletRows()
	if err != nil {
		return nil, err
	}
	for _, r := range rows {
		// An all-zero txid is a send that produced no transaction, not one to audit.
		if strings.Trim(r.TxID, "0") == "" || (r.Target != "" && r.Target != walletsvc.TargetArcade) {
			continue
		}
		created, err := time.Parse(time.RFC3339, r.CreatedAt)
		if err == nil && created.Before(since) {
			continue
		}
		txs = append(txs, audit.Tx{ID: r.TxID, Submitted: created})
	}
	return txs, nil
}

// report turns an audit report into a step output map, so its fields can be asserted on
// (${audit.divergences}) and land in the run record.
func report(r any, summary string) (map[string]any, error) {
	b, err := json.Marshal(r)
	if err != nil {
		return nil, err
	}
	out := map[string]any{}
	if err := json.Unmarshal(b, &out); err != nil {
		return nil, err
	}
	out["summary"] = summary
	return out, nil
}
