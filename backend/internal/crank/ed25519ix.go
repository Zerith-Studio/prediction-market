package crank

import (
	"encoding/binary"

	"github.com/gagliardetto/solana-go"
)

// Ed25519ProgramID is the native Ed25519 signature-verification precompile.
var Ed25519ProgramID = solana.MustPublicKeyFromBase58("Ed25519SigVerify111111111111111111111111111")

// ed25519SelfIndex marks an offset as "within this same instruction's data"
// (mirrors ED25519_IX_SELF in programs/pitchmarket/src/sig_verify.rs).
const ed25519SelfIndex = uint16(0xFFFF)

// NewEd25519Instruction builds the self-contained Ed25519 precompile
// instruction byte-for-byte in the layout sig_verify.rs asserts:
//
//	[16-byte header][signature 64][pubkey 32][message N]
//
// with all three instruction indices set to 0xFFFF (self). The precompile
// itself does the cryptographic check when the tx executes; the program then
// introspects this instruction to bind (pubkey, message) to the order.
func NewEd25519Instruction(pubkey [32]byte, message []byte, sig [64]byte) solana.Instruction {
	const headerLen = 16
	sigOff := uint16(headerLen)
	pkOff := sigOff + 64
	msgOff := pkOff + 32

	data := make([]byte, 0, headerLen+64+32+len(message))
	data = append(data, 1, 0) // num_signatures = 1, padding
	data = binary.LittleEndian.AppendUint16(data, sigOff)
	data = binary.LittleEndian.AppendUint16(data, ed25519SelfIndex)
	data = binary.LittleEndian.AppendUint16(data, pkOff)
	data = binary.LittleEndian.AppendUint16(data, ed25519SelfIndex)
	data = binary.LittleEndian.AppendUint16(data, msgOff)
	data = binary.LittleEndian.AppendUint16(data, uint16(len(message)))
	data = binary.LittleEndian.AppendUint16(data, ed25519SelfIndex)
	data = append(data, sig[:]...)
	data = append(data, pubkey[:]...)
	data = append(data, message...)

	// The precompile takes no accounts — everything is in the data.
	return solana.NewInstruction(Ed25519ProgramID, solana.AccountMetaSlice{}, data)
}
