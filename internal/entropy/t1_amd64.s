//go:build amd64

#include "textflag.h"

// func clearFlags_avx(flags []T1Flags)
// Fast zeroing of T1Flags slice using AVX stores
TEXT ·clearFlags_avx(SB), NOSPLIT, $0-24
    MOVQ flags_base+0(FP), SI    // flags pointer
    MOVQ flags_len+8(FP), CX     // length

    TESTQ CX, CX
    JZ   done_clear

    // Zero YMM register
    VPXOR Y0, Y0, Y0

    // Process 32 bytes at a time with AVX
    MOVQ CX, DX
    SHRQ $5, DX                  // length / 32
    JZ   small_clear

avx_clear_loop:
    VMOVDQU Y0, (SI)
    ADDQ $32, SI
    DECQ DX
    JNZ  avx_clear_loop

small_clear:
    // Handle remaining elements (0-31)
    ANDQ $31, CX
    JZ   done_clear

    // Process 8 bytes at a time
    MOVQ CX, DX
    SHRQ $3, DX
    JZ   tiny_clear

eight_loop:
    MOVQ $0, (SI)
    ADDQ $8, SI
    DECQ DX
    JNZ  eight_loop

tiny_clear:
    ANDQ $7, CX
    JZ   done_clear

remaining_clear_loop:
    MOVB $0, (SI)
    INCQ SI
    DECQ CX
    JNZ  remaining_clear_loop

done_clear:
    VZEROUPPER
    RET
