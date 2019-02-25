package gobjdump

import (
	"bytes"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"io"
	"strings"
)

type Z80AsmErrorType uint8

const (
	Z80AsmErrorIllegalInstruction Z80AsmErrorType = iota
	Z80AsmErrorUnimplementedInstruction
	Z80AsmErrorMalformedInstruction
	Z80AsmErrorUnknown
)

type Z80AsmError struct {
	errorType Z80AsmErrorType
}

func (e *Z80AsmError) Error() string {
	switch e.errorType {
	case Z80AsmErrorIllegalInstruction:
		return "Illegal Instruction"
	case Z80AsmErrorUnimplementedInstruction:
		return "Unimplemented Instruction"
	case Z80AsmErrorMalformedInstruction:
		return "Malformed Instruction"
	default:
		return "Unknown"
	}
}

type GBInstruction struct {
	Addr        uint32
	Instruction []uint8
	Mnemonic    []string
	Err         error
    Prev        *GBInstruction
    Next        *GBInstruction
}

var r8 = []string{
	"b",
	"c",
	"d",
	"e",
	"h",
	"l",
	"[hl]",
	"a",
}

var r16_sp = []string{
	"bc",
	"de",
	"hl",
	"sp",
}

var r16_af = []string{
	"bc",
	"de",
	"hl",
	"af",
}

var conditions = []string{
	"NZ",
	"Z",
	"NC",
	"C",
	"PO",
	"PE",
	"P",
	"M",
}

var rotateShift = []string{
	"rlc",
	"rrc",
	"rl",
	"rr",
	"sla",
	"sra",
	"swap",
	"srl",
}

var ALU = [][]string{
	[]string{"add", "a"},
	[]string{"adc", "a"},
	[]string{"sub"},
	[]string{"sbc", "a"},
	[]string{"and"},
	[]string{"xor"},
	[]string{"or"},
	[]string{"cp"},
}

var interruptModes = []string{
	"0",
	"0/1",
	"1",
	"2",
	"0",
	"0/1",
	"1",
	"2",
}

var blockInstructions = [4][4]string{
	[4]string{"ldi", "cpi", "ini", "outi"},
	[4]string{"ldd", "cpd", "ind", "outd"},
	[4]string{"ldir", "cpir", "inir", "otir"},
	[4]string{"lddr", "cpdr", "indr", "otdr"},
}

/* Consumes an immediate 8 bit value from the stream, updates the args buffer with it */
func imm8(r *bytes.Reader, instruction *[]uint8) (string, error) {
	nextByte, err := r.ReadByte()
	if err != nil {
		if err == io.EOF {
			return "", &Z80AsmError{errorType: Z80AsmErrorMalformedInstruction}
		} else {
			return "", &Z80AsmError{errorType: Z80AsmErrorUnknown}
		}
	}
	*instruction = append(*instruction, nextByte)
	return fmt.Sprintf("0x%02x", nextByte), nil
}

/* Consumes a signed immediate 8 bit value from the stream, updates the args buffer with it */
func imm8_s(r *bytes.Reader, instruction *[]uint8) (string, error) {
	nextByte, err := r.ReadByte()
	if err != nil {
		if err == io.EOF {
			return "", &Z80AsmError{errorType: Z80AsmErrorMalformedInstruction}
		} else {
			return "", &Z80AsmError{errorType: Z80AsmErrorUnknown}
		}
	}
	*instruction = append(*instruction, nextByte)
	return fmt.Sprintf("%d", int8(nextByte)), nil
}

func imm16(r *bytes.Reader, instruction *[]uint8) (string, error) {
	imm := make([]uint8, 2)
	_, err := io.ReadFull(r, imm)
	if err != nil {
		if err == io.EOF || err == io.ErrUnexpectedEOF {
			return "", &Z80AsmError{errorType: Z80AsmErrorMalformedInstruction}
		} else {
			return "", &Z80AsmError{errorType: Z80AsmErrorUnknown}
		}
	}
	*instruction = append(*instruction, imm[0], imm[1])
	return fmt.Sprintf("0x%02x%02x", imm[1], imm[0]), nil
}

func imm16_addr(r *bytes.Reader, instruction *[]uint8) (string, error) {
	imm := make([]uint8, 2)
	_, err := io.ReadFull(r, imm)
	if err != nil {
		if err == io.EOF || err == io.ErrUnexpectedEOF {
			return "", &Z80AsmError{errorType: Z80AsmErrorMalformedInstruction}
		} else {
			return "", &Z80AsmError{errorType: Z80AsmErrorUnknown}
		}
	}
	*instruction = append(*instruction, imm[0], imm[1])
	return fmt.Sprintf("[0x%02x%02x]", imm[1], imm[0]), nil
}

func r16_af_addr(r *bytes.Reader, instruction *[]uint8) string {
	reg_index := ((*instruction)[0] & 0x30) >> 4
	return fmt.Sprintf("[%s]", r16_af[reg_index])
}

func r16_sp_addr(r *bytes.Reader, instruction *[]uint8) string {
	reg_index := ((*instruction)[0] & 0x30) >> 4
	return fmt.Sprintf("[%s]", r16_sp[reg_index])
}

func decodeDJNZ(r *bytes.Reader, instruction *[]uint8, mnemonic *[]string) error {
	*mnemonic = append(*mnemonic, "djnz")
	/* Read operand (next byte) */
	operand, err := imm8_s(r, instruction)
	if err != nil {
		return err
	}
	*mnemonic = append(*mnemonic, operand)
	return nil
}

func decodeJR_E(r *bytes.Reader, instruction *[]uint8, mnemonic *[]string) error {
	*mnemonic = append(*mnemonic, "jr")
	/* Read operand (next byte) */
	operand, err := imm8_s(r, instruction)
	if err != nil {
		return err
	}
	*mnemonic = append(*mnemonic, operand)
	return nil
}

func decodeJR_cond_E(r *bytes.Reader, instruction *[]uint8, mnemonic *[]string) error {
	*mnemonic = append(*mnemonic, "jr")
	cond_index := ((*instruction)[0]&0x38)>>3 - 4
	*mnemonic = append(*mnemonic, conditions[cond_index])
	operand, err := imm8_s(r, instruction)
	if err != nil {
		return err
	}
	*mnemonic = append(*mnemonic, operand)
	return nil

}

func decodeLD_r16_nn(r *bytes.Reader, instruction *[]uint8, mnemonic *[]string) error {
	reg_index := ((*instruction)[0] & 0x30) >> 4
	*mnemonic = append(*mnemonic, "ld")
	*mnemonic = append(*mnemonic, r16_sp[reg_index])
	operand, err := imm16(r, instruction)
	if err != nil {
		return err
	}
	*mnemonic = append(*mnemonic, operand)
	return nil
}

func decodeADD_hl_r16(r *bytes.Reader, instruction *[]uint8, mnemonic *[]string) {
	reg_index := ((*instruction)[0] & 0x30) >> 4
	*mnemonic = append(*mnemonic, "add")
	*mnemonic = append(*mnemonic, "hl")
	*mnemonic = append(*mnemonic, (r16_sp[reg_index]))
}

func decodeLD_BC_A(r *bytes.Reader, instruction *[]uint8, mnemonic *[]string) {
	*mnemonic = append(*mnemonic, "ld")
	*mnemonic = append(*mnemonic, "[bc]")
	*mnemonic = append(*mnemonic, "a")
}

func decodeLD_DE_A(r *bytes.Reader, instruction *[]uint8, mnemonic *[]string) {
	*mnemonic = append(*mnemonic, "ld")
	*mnemonic = append(*mnemonic, "[de]")
	*mnemonic = append(*mnemonic, "a")
}

func decodeLDI_HL_A(r *bytes.Reader, instruction *[]uint8, mnemonic *[]string) {
	*mnemonic = append(*mnemonic, "ldi")
	*mnemonic = append(*mnemonic, "[hl]")
	*mnemonic = append(*mnemonic, "a")
}

func decodeLDD_HL_A(r *bytes.Reader, instruction *[]uint8, mnemonic *[]string) {
	*mnemonic = append(*mnemonic, "ldd")
	*mnemonic = append(*mnemonic, "[hl]")
	*mnemonic = append(*mnemonic, "a")
}

func decodeLD_nn_HL(r *bytes.Reader, instruction *[]uint8, mnemonic *[]string) error {
	*mnemonic = append(*mnemonic, "ld")
	operand, err := imm16_addr(r, instruction)
	if err != nil {
		return err
	}
	*mnemonic = append(*mnemonic, operand)
	*mnemonic = append(*mnemonic, "hl")
	return nil
}

func decodeLD_nn_A(r *bytes.Reader, instruction *[]uint8, mnemonic *[]string) error {
	*mnemonic = append(*mnemonic, "ld")
	operand, err := imm16_addr(r, instruction)
	if err != nil {
		return err
	}
	*mnemonic = append(*mnemonic, operand)
	*mnemonic = append(*mnemonic, "a")
	return nil
}

func decodeLD_n_A(r *bytes.Reader, instruction *[]uint8, mnemonic *[]string) error {
	*mnemonic = append(*mnemonic, "ld")
	operand, err := imm8(r, instruction)
	if err != nil {
		return err
	}
	*mnemonic = append(*mnemonic, fmt.Sprintf("[0xff00 + %s]", operand))
	*mnemonic = append(*mnemonic, "a")
	return nil
}

func decodeADD_SP_n(r *bytes.Reader, instruction *[]uint8, mnemonic *[]string) error {
	*mnemonic = append(*mnemonic, "add")
	*mnemonic = append(*mnemonic, "sp")
	operand, err := imm8_s(r, instruction)
	if err != nil {
		return err
	}
	*mnemonic = append(*mnemonic, operand)
	return nil
}

func decodeLD_A_n(r *bytes.Reader, instruction *[]uint8, mnemonic *[]string) error {
	*mnemonic = append(*mnemonic, "ld")
	*mnemonic = append(*mnemonic, "a")
	operand, err := imm8(r, instruction)
	if err != nil {
		return err
	}
	*mnemonic = append(*mnemonic, fmt.Sprintf("[0xff00 + %s]", operand))
	return nil
}

func decodeLD_HL_SP(r *bytes.Reader, instruction *[]uint8, mnemonic *[]string) error {
	*mnemonic = append(*mnemonic, "ldhl")
	operand, err := imm8_s(r, instruction)
	if err != nil {
		return err
	}
	*mnemonic = append(*mnemonic, "sp")
	*mnemonic = append(*mnemonic, operand)
	return nil
}

func decodeLD_A_BC(r *bytes.Reader, instruction *[]uint8, mnemonic *[]string) {
	*mnemonic = append(*mnemonic, "ld")
	*mnemonic = append(*mnemonic, "a")
	*mnemonic = append(*mnemonic, "[bc]")
}

func decodeLD_A_DE(r *bytes.Reader, instruction *[]uint8, mnemonic *[]string) {
	*mnemonic = append(*mnemonic, "ld")
	*mnemonic = append(*mnemonic, "a")
	*mnemonic = append(*mnemonic, "[de]")
}

func decodeLDI_A_HL(r *bytes.Reader, instruction *[]uint8, mnemonic *[]string) {
	*mnemonic = append(*mnemonic, "ldi")
	*mnemonic = append(*mnemonic, "a")
	*mnemonic = append(*mnemonic, "[hl]")
}

func decodeLDD_A_HL(r *bytes.Reader, instruction *[]uint8, mnemonic *[]string) {
	*mnemonic = append(*mnemonic, "ldd")
	*mnemonic = append(*mnemonic, "a")
	*mnemonic = append(*mnemonic, "[hl]")
}

func decodeLD_HL_nn(r *bytes.Reader, instruction *[]uint8, mnemonic *[]string) error {
	*mnemonic = append(*mnemonic, "ld")
	*mnemonic = append(*mnemonic, "hl")
	operand, err := imm16_addr(r, instruction)
	if err != nil {
		return err
	}
	*mnemonic = append(*mnemonic, operand)
	return nil
}

func decodeLD_A_nn(r *bytes.Reader, instruction *[]uint8, mnemonic *[]string) error {
	*mnemonic = append(*mnemonic, "ld")
	*mnemonic = append(*mnemonic, "a")
	operand, err := imm16_addr(r, instruction)
	if err != nil {
		return err
	}
	*mnemonic = append(*mnemonic, operand)
	return nil
}

func decodeINC_r16(r *bytes.Reader, instruction *[]uint8, mnemonic *[]string) {
	reg_index := ((*instruction)[0] & 0x30) >> 4
	*mnemonic = append(*mnemonic, "inc")
	*mnemonic = append(*mnemonic, r16_sp[reg_index])
}

func decodeDEC_r16(r *bytes.Reader, instruction *[]uint8, mnemonic *[]string) {
	reg_index := ((*instruction)[0] & 0x30) >> 4
	*mnemonic = append(*mnemonic, "dec")
	*mnemonic = append(*mnemonic, r16_sp[reg_index])
}

func decodeINC_r8(r *bytes.Reader, instruction *[]uint8, mnemonic *[]string) {
	reg_index := ((*instruction)[0] & 0x38) >> 3
	*mnemonic = append(*mnemonic, "inc")
	*mnemonic = append(*mnemonic, r8[reg_index])
}

func decodeDEC_r8(r *bytes.Reader, instruction *[]uint8, mnemonic *[]string) {
	reg_index := ((*instruction)[0] & 0x38) >> 3
	*mnemonic = append(*mnemonic, "dec")
	*mnemonic = append(*mnemonic, r8[reg_index])
}

func decodeLD_r8_n(r *bytes.Reader, instruction *[]uint8, mnemonic *[]string) error {
	reg_index := ((*instruction)[0] & 0x38) >> 3
	*mnemonic = append(*mnemonic, "ld")
	*mnemonic = append(*mnemonic, r8[reg_index])
	operand, err := imm8(r, instruction)
	if err != nil {
		return err
	}
	*mnemonic = append(*mnemonic, operand)
	return nil
}

func decodeLD_r8_r8(r *bytes.Reader, instruction *[]uint8, mnemonic *[]string) {
	reg_dst := ((*instruction)[0] & 0x38) >> 3
	reg_src := (*instruction)[0] & 0x7
	*mnemonic = append(*mnemonic, "ld")
	*mnemonic = append(*mnemonic, r8[reg_dst])
	*mnemonic = append(*mnemonic, r8[reg_src])
}

func decodeALU_r8(r *bytes.Reader, instruction *[]uint8, mnemonic *[]string) {
	alu_op := ((*instruction)[0] & 0x38) >> 3
	reg_index := (*instruction)[0] & 0x07
	*mnemonic = append(*mnemonic, ALU[alu_op]...)
	*mnemonic = append(*mnemonic, r8[reg_index])
}

func decodeRET_cc(r *bytes.Reader, instruction *[]uint8, mnemonic *[]string) {
	cc := ((*instruction)[0] & 0x38) >> 3
	*mnemonic = append(*mnemonic, "ret")
	*mnemonic = append(*mnemonic, conditions[cc])
}

func decodePOP_r16(r *bytes.Reader, instruction *[]uint8, mnemonic *[]string) {
	reg_index := ((*instruction)[0] & 0x30) >> 4
	*mnemonic = append(*mnemonic, "pop")
	*mnemonic = append(*mnemonic, r16_af[reg_index])
}

func decodeJP_HL(r *bytes.Reader, instruction *[]uint8, mnemonic *[]string) {
	*mnemonic = append(*mnemonic, "jp")
	*mnemonic = append(*mnemonic, "[hl]")
}

func decodeLD_SP_HL(r *bytes.Reader, instruction *[]uint8, mnemonic *[]string) {
	*mnemonic = append(*mnemonic, "ld")
	*mnemonic = append(*mnemonic, "sp")
	*mnemonic = append(*mnemonic, "hl")
}

func decodeLD_C_A(r *bytes.Reader, instruction *[]uint8, mnemonic *[]string) {
	*mnemonic = append(*mnemonic, "ld")
	*mnemonic = append(*mnemonic, "[0xff00 + C]")
	*mnemonic = append(*mnemonic, "a")
}

func decodeLD_A_C(r *bytes.Reader, instruction *[]uint8, mnemonic *[]string) {
	*mnemonic = append(*mnemonic, "ld")
	*mnemonic = append(*mnemonic, "a")
	*mnemonic = append(*mnemonic, "[0xff00 + C]")
}

func decodeJP_cc_nn(r *bytes.Reader, instruction *[]uint8, mnemonic *[]string) error {
	cc := ((*instruction)[0] & 0x38) >> 3
	*mnemonic = append(*mnemonic, "jp")
	*mnemonic = append(*mnemonic, conditions[cc])
	operand, err := imm16(r, instruction)
	if err != nil {
		return err
	}
	*mnemonic = append(*mnemonic, operand)
	return nil
}

func decodeJP_nn(r *bytes.Reader, instruction *[]uint8, mnemonic *[]string) error {
	*mnemonic = append(*mnemonic, "jp")
	operand, err := imm16(r, instruction)
	if err != nil {
		return err
	}
	*mnemonic = append(*mnemonic, operand)
	return nil
}

func decodeOUT_n_A(r *bytes.Reader, instruction *[]uint8, mnemonic *[]string) error {
	*mnemonic = append(*mnemonic, "out")
	operand, err := imm8(r, instruction)
	if err != nil {
		return err
	}
	*mnemonic = append(*mnemonic, fmt.Sprintf("[%s]", operand))
	*mnemonic = append(*mnemonic, "a")
	return nil
}

func decodeIN_a_n(r *bytes.Reader, instruction *[]uint8, mnemonic *[]string) error {
	*mnemonic = append(*mnemonic, "in")
	*mnemonic = append(*mnemonic, "a")
	operand, err := imm8(r, instruction)
	if err != nil {
		return err
	}
	*mnemonic = append(*mnemonic, fmt.Sprintf("[%s]", operand))
	return nil
}

func decodeEX_SP_HL(r *bytes.Reader, instruction *[]uint8, mnemonic *[]string) {
	*mnemonic = append(*mnemonic, "ex")
	*mnemonic = append(*mnemonic, "[sp]")
	*mnemonic = append(*mnemonic, "hl")
}

func decodeEX_DE_HL(r *bytes.Reader, instruction *[]uint8, mnemonic *[]string) {
	*mnemonic = append(*mnemonic, "ex")
	*mnemonic = append(*mnemonic, "de")
	*mnemonic = append(*mnemonic, "hl")
}

func decodeCALL_cc_nn(r *bytes.Reader, instruction *[]uint8, mnemonic *[]string) error {
	cc := ((*instruction)[0] & 0x38) >> 3
	*mnemonic = append(*mnemonic, "call")
	*mnemonic = append(*mnemonic, conditions[cc])
	operand, err := imm16(r, instruction)
	if err != nil {
		return err
	}
	*mnemonic = append(*mnemonic, operand)
	return nil
}

func decodePUSH_r16(r *bytes.Reader, instruction *[]uint8, mnemonic *[]string) {
	reg_index := ((*instruction)[0] & 0x30) >> 4
	*mnemonic = append(*mnemonic, "push")
	*mnemonic = append(*mnemonic, r16_af[reg_index])
}

func decodeCALL_nn(r *bytes.Reader, instruction *[]uint8, mnemonic *[]string) error {
	*mnemonic = append(*mnemonic, "call")
	operand, err := imm16(r, instruction)
	if err != nil {
		return err
	}
	*mnemonic = append(*mnemonic, operand)
	return nil
}

func decodeALU_n(r *bytes.Reader, instruction *[]uint8, mnemonic *[]string) error {
	alu_op := ((*instruction)[0] & 0x38) >> 3
	*mnemonic = append(*mnemonic, ALU[alu_op]...)
	operand, err := imm8(r, instruction)
	if err != nil {
		return err
	}
	*mnemonic = append(*mnemonic, operand)
	return nil
}

func decodeRST(r *bytes.Reader, instruction *[]uint8, mnemonic *[]string) {
	t := ((*instruction)[0] & 0x38) >> 3
	*mnemonic = append(*mnemonic, "rst")
	*mnemonic = append(*mnemonic, fmt.Sprintf("0x%02x", t*8))
}

func decodeRotateShift_r8(r *bytes.Reader, instruction *[]uint8, mnemonic *[]string) {
	op := ((*instruction)[1] & 0x38) >> 3
	reg_index := (*instruction)[1] & 0x07
	*mnemonic = append(*mnemonic, rotateShift[op])
	*mnemonic = append(*mnemonic, r8[reg_index])
}

func decodeBIT_b_r8(r *bytes.Reader, instruction *[]uint8, mnemonic *[]string) {
	bit := ((*instruction)[1] & 0x38) >> 3
	reg_index := (*instruction)[1] & 0x07
	*mnemonic = append(*mnemonic, "bit")
	*mnemonic = append(*mnemonic, fmt.Sprintf("%d", bit))
	*mnemonic = append(*mnemonic, r8[reg_index])
}

func decodeRES_b_r8(r *bytes.Reader, instruction *[]uint8, mnemonic *[]string) {
	bit := ((*instruction)[1] & 0x38) >> 3
	reg_index := (*instruction)[1] & 0x07
	*mnemonic = append(*mnemonic, "res")
	*mnemonic = append(*mnemonic, fmt.Sprintf("%d", bit))
	*mnemonic = append(*mnemonic, r8[reg_index])
}

func decodeSET_b_r8(r *bytes.Reader, instruction *[]uint8, mnemonic *[]string) {
	bit := ((*instruction)[1] & 0x38) >> 3
	reg_index := (*instruction)[1] & 0x07
	*mnemonic = append(*mnemonic, "set")
	*mnemonic = append(*mnemonic, fmt.Sprintf("%d", bit))
	*mnemonic = append(*mnemonic, r8[reg_index])
}

func decodeIN_C(r *bytes.Reader, instruction *[]uint8, mnemonic *[]string) {
	*mnemonic = append(*mnemonic, "in")
	*mnemonic = append(*mnemonic, "[c]")
}

func decodeIN_r8_C(r *bytes.Reader, instruction *[]uint8, mnemonic *[]string) {
	reg_index := ((*instruction)[1] & 0x38) >> 3
	*mnemonic = append(*mnemonic, "in")
	*mnemonic = append(*mnemonic, r8[reg_index])
	*mnemonic = append(*mnemonic, "[c]")
}

func decodeOUT_C(r *bytes.Reader, instruction *[]uint8, mnemonic *[]string) {
	*mnemonic = append(*mnemonic, "out")
	*mnemonic = append(*mnemonic, "[c]")
	*mnemonic = append(*mnemonic, "0")
}

func decodeOUT_r8_C(r *bytes.Reader, instruction *[]uint8, mnemonic *[]string) {
	reg_index := ((*instruction)[1] & 0x38) >> 3
	*mnemonic = append(*mnemonic, "out")
	*mnemonic = append(*mnemonic, "[c]")
	*mnemonic = append(*mnemonic, r8[reg_index])
}

func decodeSBC_HL_r16(r *bytes.Reader, instruction *[]uint8, mnemonic *[]string) {
	reg_index := ((*instruction)[1] & 0x30) >> 4
	*mnemonic = append(*mnemonic, "sbc")
	*mnemonic = append(*mnemonic, "hl")
	*mnemonic = append(*mnemonic, r16_sp[reg_index])
}

func decodeADC_HL_r16(r *bytes.Reader, instruction *[]uint8, mnemonic *[]string) {
	reg_index := ((*instruction)[1] & 0x30) >> 4
	*mnemonic = append(*mnemonic, "adc")
	*mnemonic = append(*mnemonic, "hl")
	*mnemonic = append(*mnemonic, r16_sp[reg_index])
}

func decodeLD_nn_SP(r *bytes.Reader, instruction *[]uint8, mnemonic *[]string) error {
	*mnemonic = append(*mnemonic, "ld")
	operand, err := imm16_addr(r, instruction)
	if err != nil {
		return err
	}
	*mnemonic = append(*mnemonic, operand)
	*mnemonic = append(*mnemonic, "sp")
	return nil
}

func decodeLD_nn_r16(r *bytes.Reader, instruction *[]uint8, mnemonic *[]string) error {
	reg_index := ((*instruction)[1] & 0x30) >> 4
	*mnemonic = append(*mnemonic, "ld")
	operand, err := imm16_addr(r, instruction)
	if err != nil {
		return err
	}
	*mnemonic = append(*mnemonic, operand)
	*mnemonic = append(*mnemonic, r16_sp[reg_index])
	return nil
}

func decodeLD_r16_nn_addr(r *bytes.Reader, instruction *[]uint8, mnemonic *[]string) error {
	reg_index := ((*instruction)[1] & 0x30) >> 4
	*mnemonic = append(*mnemonic, "ld")
	operand, err := imm16_addr(r, instruction)
	if err != nil {
		return err
	}
	*mnemonic = append(*mnemonic, r16_sp[reg_index])
	*mnemonic = append(*mnemonic, operand)
	return nil
}

func decodeIM_im(r *bytes.Reader, instruction *[]uint8, mnemonic *[]string) {
	im := ((*instruction)[1] & 0x38) >> 3
	*mnemonic = append(*mnemonic, "im")
	*mnemonic = append(*mnemonic, interruptModes[im])
}

func decodeLD_dst_src(dst string, src string, r *bytes.Reader, instruction *[]uint8, mnemonic *[]string) {
	*mnemonic = append(*mnemonic, "ld")
	*mnemonic = append(*mnemonic, dst)
	*mnemonic = append(*mnemonic, src)
}

func decodeBLI(r *bytes.Reader, instruction *[]uint8, mnemonic *[]string) {
	a := (((*instruction)[1] & 0x38) >> 3) - 4
	b := (*instruction)[1] & 0x07
	*mnemonic = append(*mnemonic, blockInstructions[a][b])
}

func decodePrefixCB(r *bytes.Reader, instruction *[]uint8, mnemonic *[]string) error {
	nextByte, err := r.ReadByte()
	if err != nil {
		return &Z80AsmError{errorType: Z80AsmErrorMalformedInstruction}
	}
	*instruction = append(*instruction, nextByte)

	switch nextByte & 0xc0 {
	case 0x00:
		/* assorted rotate & shift operations */
		decodeRotateShift_r8(r, instruction, mnemonic)
	case 0x40:
		/* bit b, r8 */
		decodeBIT_b_r8(r, instruction, mnemonic)
	case 0x80:
		/* res b, r8 */
		decodeRES_b_r8(r, instruction, mnemonic)
	case 0xc0:
		/* set b, r8 */
		decodeSET_b_r8(r, instruction, mnemonic)
	}
	return err
}

/*
 * Bumps the pointer in r
 * returns: the instruction bytes, the instruction mnemonic as an array of tokens
 */
func DecodeInstruction(r *bytes.Reader, addr uint32) (*GBInstruction, uint32) {
	/* If EOF, return empty string */
	var instruction []uint8
	nextByte, err := r.ReadByte()
	if err != nil {
		if err == io.EOF {
			return nil, addr
		}
	}

	instruction = append(instruction, nextByte)
	var mnemonic []string

	/* Switch on bits 6-7 */
	switch nextByte & 0xc0 {
	case 0x00:
		/* Switch on bits 0-2 */
		switch nextByte & 0x07 {
		case 0x00:
			/* Switch on bits 3-5 */
			switch nextByte & 0x38 {
			case 0x00:
				/* nop */
				mnemonic = append(mnemonic, "nop")
			case 0x08:
				/* LD [nn], sp */
				err = decodeLD_nn_SP(r, &instruction, &mnemonic)
			case 0x10:
				/*
				 * STOP
				 */
				mnemonic = append(mnemonic, "stop")
			case 0x18:
				/*
				 * jr E - jump to PC + E
				 */
				err = decodeJR_E(r, &instruction, &mnemonic)
			default:
				/* jr nz|z|nc|c, E*/
				err = decodeJR_cond_E(r, &instruction, &mnemonic)
			}
		case 0x01:
			/* switch on bit 3 */
			switch nextByte & 0x08 {
			case 0x00:
				/* ld rp[p], nn */
				err = decodeLD_r16_nn(r, &instruction, &mnemonic)
			case 0x08:
				/* add hl, rp[p] */
				decodeADD_hl_r16(r, &instruction, &mnemonic)
			}
		case 0x02:
			/* switch on bit 3 */
			switch nextByte & 0x08 {
			case 0x00:
				/* switch on bits 4-5 */
				switch nextByte & 0x30 {
				case 0x00:
					/* ld [bc], a */
					decodeLD_BC_A(r, &instruction, &mnemonic)
				case 0x10:
					/* ld [de], a */
					decodeLD_DE_A(r, &instruction, &mnemonic)
				case 0x20:
					/* LDI [HL], A */
					decodeLDI_HL_A(r, &instruction, &mnemonic)
				case 0x30:
					/* LDD [HL], A */
					decodeLDD_HL_A(r, &instruction, &mnemonic)
				}
			case 0x08:
				/* switch on bits 4-5 */
				switch nextByte & 0x30 {
				case 0x00:
					/* ld a, [bc] */
					decodeLD_A_BC(r, &instruction, &mnemonic)
				case 0x10:
					/* ld a, [de] */
					decodeLD_A_DE(r, &instruction, &mnemonic)
				case 0x20:
					/* ldi A, [HL] */
					decodeLDI_A_HL(r, &instruction, &mnemonic)
				case 0x30:
					/* ldd A, [HL] */
					decodeLDD_A_HL(r, &instruction, &mnemonic)
				}
			}
		case 0x03:
			/* switch on bit 3 */
			switch nextByte & 0x08 {
			case 0x00:
				/* inc r16 */
				decodeINC_r16(r, &instruction, &mnemonic)
			case 0x08:
				/* dec r16 */
				decodeDEC_r16(r, &instruction, &mnemonic)
			}
		case 0x04:
			/* inc r8 */
			decodeINC_r8(r, &instruction, &mnemonic)
		case 0x05:
			/* dec r8 */
			decodeDEC_r8(r, &instruction, &mnemonic)
		case 0x06:
			/* ld r8, n */
			err = decodeLD_r8_n(r, &instruction, &mnemonic)
		case 0x07:
			/* switch on bits 3-5 */
			switch nextByte & 0x38 {
			case 0x00:
				/* RLCA */
				mnemonic = append(mnemonic, "rlca")
			case 0x08:
				/* RRCA */
				mnemonic = append(mnemonic, "rrca")
			case 0x10:
				/* RLA */
				mnemonic = append(mnemonic, "rla")
			case 0x18:
				/* RRA */
				mnemonic = append(mnemonic, "rra")
			case 0x20:
				/* DAA */
				mnemonic = append(mnemonic, "daa")
			case 0x28:
				/* CPL */
				mnemonic = append(mnemonic, "cpl")
			case 0x30:
				/* SCF */
				mnemonic = append(mnemonic, "scf")
			case 0x38:
				/* CCF */
				mnemonic = append(mnemonic, "ccf")
			}
		}
	case 0x40:
		switch nextByte & 0x07 {
		case 0x6:
			switch nextByte & 0x38 {
			case 0x30:
				/* halt */
				mnemonic = append(mnemonic, "halt")
			default:
				/* ld r, r' */
				decodeLD_r8_r8(r, &instruction, &mnemonic)
			}
		default:
			/* ld r, r' */
			decodeLD_r8_r8(r, &instruction, &mnemonic)
		}
	case 0x80:
		/* assorted ALU instructions */
		decodeALU_r8(r, &instruction, &mnemonic)
	case 0xc0:
		switch nextByte & 0x07 {
		case 0x00:
			/* ret CC - conditional return */
			switch nextByte & 0x38 {
			case 0x00:
				fallthrough
			case 0x08:
				fallthrough
			case 0x10:
				fallthrough
			case 0x18:
				decodeRET_cc(r, &instruction, &mnemonic)
			case 0x20:
				err = decodeLD_n_A(r, &instruction, &mnemonic)
			case 0x28:
				err = decodeADD_SP_n(r, &instruction, &mnemonic)
			case 0x30:
				err = decodeLD_A_n(r, &instruction, &mnemonic)
			case 0x38:
				err = decodeLD_HL_SP(r, &instruction, &mnemonic)
			}
		case 0x01:
			switch nextByte & 0x08 {
			case 0x00:
				/* pop r16 */
				decodePOP_r16(r, &instruction, &mnemonic)
			case 0x08:
				switch nextByte & 0x30 {
				case 0x00:
					/* ret */
					mnemonic = append(mnemonic, "ret")
				case 0x10:
					/* reti */
					mnemonic = append(mnemonic, "reti")
				case 0x20:
					/* jp hl */
					decodeJP_HL(r, &instruction, &mnemonic)
				case 0x30:
					/* ld sp, hl */
					decodeLD_SP_HL(r, &instruction, &mnemonic)
				}
			}
		case 0x02:
			/* jp cc, nn - conditional absolute jump */
			switch nextByte & 0x38 {
			case 0x00:
				fallthrough
			case 0x08:
				fallthrough
			case 0x10:
				fallthrough
			case 0x18:
				err = decodeJP_cc_nn(r, &instruction, &mnemonic)
			case 0x20:
				decodeLD_C_A(r, &instruction, &mnemonic)
			case 0x28:
				err = decodeLD_nn_A(r, &instruction, &mnemonic)
			case 0x30:
				decodeLD_A_C(r, &instruction, &mnemonic)
			case 0x38:
				err = decodeLD_A_nn(r, &instruction, &mnemonic)
			}
		case 0x03:
			switch nextByte & 0x38 {
			case 0x00:
				/* jp nn */
				err = decodeJP_nn(r, &instruction, &mnemonic)
			case 0x08:
				/* 0xcb prefix */
				err = decodePrefixCB(r, &instruction, &mnemonic)
			case 0x10:
				/* out n, a */
				err = &Z80AsmError{errorType: Z80AsmErrorIllegalInstruction}
			case 0x18:
				/* in a, n */
				err = &Z80AsmError{errorType: Z80AsmErrorIllegalInstruction}
			case 0x20:
				/* ex sp, hl */
				err = &Z80AsmError{errorType: Z80AsmErrorIllegalInstruction}
			case 0x28:
				/* ex de, hl */
				err = &Z80AsmError{errorType: Z80AsmErrorIllegalInstruction}
			case 0x30:
				/* di */
				mnemonic = append(mnemonic, "di")
			case 0x38:
				/* ei */
				mnemonic = append(mnemonic, "ei")
			}
		case 0x04:
			/* call cc, nn - conditional call */
			switch nextByte & 0x38 {
			case 0x00:
				fallthrough
			case 0x08:
				fallthrough
			case 0x10:
				fallthrough
			case 0x18:
				err = decodeCALL_cc_nn(r, &instruction, &mnemonic)
			default:
				err = &Z80AsmError{errorType: Z80AsmErrorIllegalInstruction}
			}
		case 0x05:
			switch nextByte & 0x08 {
			case 0x00:
				/* push r16 */
				decodePUSH_r16(r, &instruction, &mnemonic)
			case 0x08:
				switch nextByte & 0x30 {
				case 0x00:
					/* call nn */
					err = decodeCALL_nn(r, &instruction, &mnemonic)
				case 0x10:
					/*
					 * DD prefix
					 */
					err = &Z80AsmError{errorType: Z80AsmErrorIllegalInstruction}
				case 0x20:
					/* ED prefix */
					err = &Z80AsmError{errorType: Z80AsmErrorIllegalInstruction}
				case 0x30:
					/* FD prefix */
					err = &Z80AsmError{errorType: Z80AsmErrorIllegalInstruction}
				}
			}
		case 0x06:
			/* assorted ALU instructions */
			err = decodeALU_n(r, &instruction, &mnemonic)
		case 0x07:
			/* rst p */
			decodeRST(r, &instruction, &mnemonic)
		}
	}
	addrPrev := addr
	addr += uint32(len(instruction))
	return &GBInstruction{
		Addr:        addrPrev,
		Instruction: instruction,
		Mnemonic:    mnemonic,
		Err:         err,
        Prev:        nil,
        Next:        nil,
	}, addr
}

func (i *GBInstruction) ToStr() string {
	instructionHex := make([]uint8, hex.EncodedLen(len(i.Instruction)))
	hex.Encode(instructionHex, i.Instruction)
	if i.Err != nil {
		return fmt.Sprintf("0x%04x: %-12s %-6s", i.Addr, instructionHex, i.Err.Error())
	} else {
		operands := ""
		if len(i.Mnemonic) > 1 {
			operands = strings.Join(i.Mnemonic[1:], ", ")
		}
		return fmt.Sprintf("0x%04x: %-12s %-6s %s", i.Addr, instructionHex, i.Mnemonic[0], operands)
	}
}

func DisassemblerLoop(r *bytes.Reader, start uint32, end uint32) int {
	var addr uint32 = start
	for gbInstruction, addr := DecodeInstruction(r, addr); gbInstruction != nil && gbInstruction.Addr < end; gbInstruction, addr = DecodeInstruction(r, addr) {
		/* Generate hex encoding of instruction */
		fmt.Printf("%s\n", gbInstruction.ToStr())

		if gbInstruction.Err != nil &&
			gbInstruction.Err.(*Z80AsmError).errorType != Z80AsmErrorIllegalInstruction &&
			gbInstruction.Err.(*Z80AsmError).errorType != Z80AsmErrorUnimplementedInstruction {
			return 1
		}

	}
	return 0
}

func GBROMPreamble(reader *bytes.Reader) int {
	/* 0x0000 - 0x0067 contains the RST and Interrupt tables */
	reader.Seek(int64(0x0000), 0)
	fmt.Printf("---------------- %-40s ----------------\n", "RST and Interrupt table")
	ret := DisassemblerLoop(reader, 0x0000, 0x0068)
	if ret != 0 {
		fmt.Printf("Oh noes!\n")
		return ret
	}

	/*
	 * Code entry point is at 0x0100-0x0103
	 * It is almost always nop followed by jp
	 */
	fmt.Printf("\n")
	fmt.Printf("---------------- %-40s ----------------\n", "Code Entry Point (Trampoline)")
	var addr uint32 = 0x0100
	reader.Seek(int64(addr), 0)
	var gbInstruction *GBInstruction
	for gbInstruction, addr = DecodeInstruction(reader, addr); gbInstruction != nil && gbInstruction.Instruction[0] == 0x00; /* while nops */
	gbInstruction, addr = DecodeInstruction(reader, addr) {
		fmt.Printf("%s\n", gbInstruction.ToStr())
	}
	fmt.Printf("%s\n", gbInstruction.ToStr())

	fmt.Printf("\n")
	fmt.Printf("---------------- %-40s ----------------\n", "Code Start")
	var target uint16
	switch gbInstruction.Instruction[0] {
	case 0xc3: /* jp */
		/* compute the offset of the jp */
		target = binary.LittleEndian.Uint16(gbInstruction.Instruction[1:])
		reader.Seek(int64(target), 0)
		return DisassemblerLoop(reader, uint32(target), uint32(0x8000))
	default:
		fmt.Printf("Oh noes!\n")
		return 1
	}
	return 1
}
