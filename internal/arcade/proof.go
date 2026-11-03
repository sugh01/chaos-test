package arcade

import (
	"fmt"

	"github.com/bsv-blockchain/go-sdk/transaction"
)

// ProofRoot computes the merkle root that a merklePath (BRC-74 BUMP hex, as arcade returns it
// in GET /tx/{txid}) proves for txid, and the block height the path claims. It does not say
// whether that root is on any chain; callers compare it with a node's header.
func ProofRoot(merklePath, txid string) (string, uint32, error) {
	mp, err := transaction.NewMerklePathFromHex(merklePath)
	if err != nil {
		return "", 0, fmt.Errorf("decode merklePath: %w", err)
	}
	root, err := mp.ComputeRootHex(&txid)
	if err != nil {
		return "", 0, fmt.Errorf("compute root for %s: %w", txid, err)
	}
	return root, mp.BlockHeight, nil
}
