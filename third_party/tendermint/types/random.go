package types

import (
	ctmproto "github.com/cometbft/cometbft/proto/tendermint/types"
)

type EnclaveRandom struct {
	Random []byte `json:"random"`
	Proof  []byte `json:"proof"`
}

func (enclaveRandom EnclaveRandom) ValidateBasic() error {
	// todo tm-enclave: this
	//if len(enclaveRandom.Random) != 100 {
	//	return error
	//}
	return nil
}

func (enclaveRandom *EnclaveRandom) ToProto() *ctmproto.EncryptedRandom {

	if enclaveRandom == nil {
		return nil
	}

	return &ctmproto.EncryptedRandom{
		Random: enclaveRandom.Random,
		Proof:  enclaveRandom.Proof,
	}
}

// FromProto sets a protobuf BlockID to the given pointer.
// It returns an error if the block id is invalid.
func EnclaveRandomFromProto(encRand *ctmproto.EncryptedRandom) (*EnclaveRandom, error) {
	if encRand == nil {
		return nil, nil
	}

	encRandom := new(EnclaveRandom)

	encRandom.Proof = encRand.Proof
	encRandom.Random = encRand.Random

	return encRandom, encRandom.ValidateBasic()
}

//// BlockID
//type BlockID struct {
//	Hash          tmbytes.HexBytes `json:"hash"`
//	PartSetHeader PartSetHeader    `json:"parts"`
//}
//
//// Equals returns true if the BlockID matches the given BlockID
//func (blockID BlockID) Equals(other BlockID) bool {
//	return bytes.Equal(blockID.Hash, other.Hash) &&
//		blockID.PartSetHeader.Equals(other.PartSetHeader)
//}
//
//// Key returns a machine-readable string representation of the BlockID
//func (blockID BlockID) Key() string {
//	pbph := blockID.PartSetHeader.ToProto()
//	bz, err := pbph.Marshal()
//	if err != nil {
//		panic(err)
//	}
//
//	return fmt.Sprint(string(blockID.Hash), string(bz))
//}
//
//// ValidateBasic performs basic validation.
//func (blockID BlockID) ValidateBasic() error {
//	// Hash can be empty in case of POLBlockID in Proposal.
//	if err := ValidateHash(blockID.Hash); err != nil {
//		return fmt.Errorf("wrong Hash")
//	}
//	if err := blockID.PartSetHeader.ValidateBasic(); err != nil {
//		return fmt.Errorf("wrong PartSetHeader: %v", err)
//	}
//	return nil
//}
//
//// IsZero returns true if this is the BlockID of a nil block.
//func (blockID BlockID) IsZero() bool {
//	return len(blockID.Hash) == 0 &&
//		blockID.PartSetHeader.IsZero()
//}
//
//// IsComplete returns true if this is a valid BlockID of a non-nil block.
//func (blockID BlockID) IsComplete() bool {
//	return len(blockID.Hash) == tmhash.Size &&
//		blockID.PartSetHeader.Total > 0 &&
//		len(blockID.PartSetHeader.Hash) == tmhash.Size
//}
//
//// String returns a human readable string representation of the BlockID.
////
//// 1. hash
//// 2. part set header
////
//// See PartSetHeader#String
//func (blockID BlockID) String() string {
//	return fmt.Sprintf(`%v:%v`, blockID.Hash, blockID.PartSetHeader)
//}
//
//// ToProto converts BlockID to protobuf
//func (blockID *BlockID) ToProto() tmproto.BlockID {
//	if blockID == nil {
//		return tmproto.BlockID{}
//	}
//
//	return tmproto.BlockID{
//		Hash:          blockID.Hash,
//		PartSetHeader: blockID.PartSetHeader.ToProto(),
//	}
//}
