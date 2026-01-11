//go:build arm64

#include "textflag.h"

// func clearFlags_neon(flags []T1Flags)
// Fast zeroing of T1Flags slice using NEON stores
TEXT ·clearFlags_neon(SB), NOSPLIT, $0-24
    MOVD flags_base+0(FP), R0    // flags pointer
    MOVD flags_len+8(FP), R1     // length

    CBZ  R1, done_clear

    // Zero register
    VEOR V0.B16, V0.B16, V0.B16
    VEOR V1.B16, V1.B16, V1.B16
    VEOR V2.B16, V2.B16, V2.B16
    VEOR V3.B16, V3.B16, V3.B16

    // Process 64 bytes at a time (64 T1Flags = 64 bytes)
    LSR  $6, R1, R2              // R2 = length / 64
    CBZ  R2, small_clear

    MOVD R2, R3                  // loop counter

vector_clear_loop:
    VST1.P [V0.B16, V1.B16, V2.B16, V3.B16], 64(R0)
    SUB  $1, R3, R3
    CBNZ R3, vector_clear_loop

small_clear:
    // Handle remaining elements
    AND  $63, R1, R2             // remaining = length % 64
    CBZ  R2, done_clear

    // Process 16 bytes at a time
    LSR  $4, R2, R3
    CBZ  R3, tiny_clear

sixteen_loop:
    VST1.P [V0.B16], 16(R0)
    SUB  $1, R3, R3
    CBNZ R3, sixteen_loop

tiny_clear:
    AND  $15, R2, R2
    CBZ  R2, done_clear

remaining_clear_loop:
    MOVB ZR, (R0)
    ADD  $1, R0, R0
    SUB  $1, R2, R2
    CBNZ R2, remaining_clear_loop

done_clear:
    RET
