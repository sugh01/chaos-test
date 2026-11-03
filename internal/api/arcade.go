package api

import (
	"context"
	"errors"
	"fmt"

	"github.com/bsv-blockchain/chaos-test/internal/arcade"
)

// ArcadeBlockStatus reads arcade's block_processing row for a block.
//
// This is arcade's durable projection of the reorg stream — the thing consumers point at — and
// is deliberately read separately from what arcade's chaintracks believes, because the two
// disagreeing is a real and reportable defect rather than an implementation detail.
func (s *Server) ArcadeBlockStatus(ctx context.Context, hash string) (*arcade.BlockStatus, error) {
	if s.d.ArcadeClient == nil {
		return nil, errors.New("arcade is not configured in the inventory")
	}
	if hash == "" {
		return nil, errors.New("a block hash is required")
	}
	return s.d.ArcadeClient.BlockStatus(ctx, hash)
}

// ArcadeTx reads arcade's record of a transaction: its status and, once mined, the block and
// merkle path. Returns arcade.ErrTxNotFound when arcade has never seen it.
func (s *Server) ArcadeTx(ctx context.Context, txid string) (*arcade.TxRecord, error) {
	if s.d.ArcadeClient == nil {
		return nil, errors.New("arcade is not configured in the inventory")
	}
	if txid == "" {
		return nil, errors.New("a txid is required")
	}
	return s.d.ArcadeClient.GetTx(ctx, txid)
}

// ArcadeCanonicalHashAt returns the block arcade's own chaintracks considers canonical at a
// height.
func (s *Server) ArcadeCanonicalHashAt(ctx context.Context, height uint32) (string, error) {
	if s.d.ArcadeClient == nil {
		return "", errors.New("arcade is not configured in the inventory")
	}
	h, err := s.d.ArcadeClient.HeaderAtHeight(ctx, height)
	if err != nil {
		return "", err
	}
	if h.Hash == "" {
		return "", fmt.Errorf("arcade returned no header at height %d", height)
	}
	return h.Hash, nil
}
