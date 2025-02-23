package txlistdecoder

import (
	"context"
	"crypto/sha256"
	"fmt"
	"math/big"

	"github.com/ethereum-optimism/optimism/op-service/eth"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto/kzg4844"
	"github.com/ethereum/go-ethereum/log"

	"github.com/taikoxyz/taiko-mono/packages/taiko-client/bindings/metadata"
	"github.com/taikoxyz/taiko-mono/packages/taiko-client/pkg"
	"github.com/taikoxyz/taiko-mono/packages/taiko-client/pkg/rpc"
)

// BlobFetcher is responsible for fetching the txList blob from the L1 block sidecar.
type BlobFetcher struct {
	l1Beacon   *rpc.BeaconClient
	dataSource *rpc.BlobDataSource
}

// NewBlobTxListFetcher creates a new BlobFetcher instance based on the given rpc client.
func NewBlobTxListFetcher(l1Beacon *rpc.BeaconClient, ds *rpc.BlobDataSource) *BlobFetcher {
	return &BlobFetcher{l1Beacon, ds}
}

// Fetch implements the TxListFetcher interface.
func (d *BlobFetcher) Fetch(
	ctx context.Context,
	_ *types.Transaction,
	meta metadata.TaikoBlockMetaData,
) ([]byte, error) {
	if !meta.GetBlobUsed() {
		return nil, pkg.ErrBlobUsed
	}

	// Fetch the L1 block sidecars.
	sidecars, err := d.dataSource.GetBlobs(ctx, meta.GetProposedAt(), meta.GetBlobHash())
	if err != nil {
		return nil, err
	}

	log.Info("Fetch sidecars", "blockNumber", meta.GetRawBlockHeight(), "sidecars", len(sidecars))

	// Compare the blob hash with the sidecar's kzg commitment.
	for i, sidecar := range sidecars {
		log.Info(
			"Block sidecar",
			"index", i,
			"KzgCommitment", sidecar.KzgCommitment,
			"blobHash", meta.GetBlobHash(),
		)

		commitment := kzg4844.Commitment(common.FromHex(sidecar.KzgCommitment))
		if kzg4844.CalcBlobHashV1(sha256.New(), &commitment) == meta.GetBlobHash() {
			blob := eth.Blob(common.FromHex(sidecar.Blob))
			bytes, err := blob.ToData()
			if err != nil {
				return nil, err
			}

			if meta.GetBlobTxListLength() == 0 {
				return bytes, nil
			}

			b, err := sliceTxList(meta.GetBlockID(), bytes, meta.GetBlobTxListOffset(), meta.GetBlobTxListLength())
			if err != nil {
				log.Warn("Invalid txlist offset and size in metadata", "blockID", meta.GetBlockID(), "err", err)
				return []byte{}, nil
			}
			return b, nil
		}
	}

	return nil, pkg.ErrSidecarNotFound
}

// sliceTxList returns the sliced txList bytes from the given offset and length.
func sliceTxList(id *big.Int, b []byte, offset, length uint32) ([]byte, error) {
	if offset+length > uint32(len(b)) {
		return nil, fmt.Errorf(
			"invalid txlist offset and size in metadata (%d): offset=%d, size=%d, blobSize=%d", id, offset, length, len(b),
		)
	}
	return b[offset : offset+length], nil
}
