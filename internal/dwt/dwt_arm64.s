//go:build arm64

#include "textflag.h"

// func liftStep1_53_neon(data []int32, length int)
// Performs: data[i] -= (data[i-1] + data[i+1]) >> 1 for odd indices
// Uses NEON for vectorized processing
TEXT ·liftStep1_53_neon(SB), NOSPLIT, $0-32
    MOVD data_base+0(FP), R0     // data pointer
    MOVD length+24(FP), R1       // length

    CMP  $8, R1                  // need at least 8 elements
    BLT  scalar_step1

    // Process 4 elements at a time (odd indices: 1,3,5,7 -> needs indices 0-8)
    SUB  $8, R1, R2              // R2 = length - 8 (loop bound)
    MOVD $1, R3                  // start at index 1

vector_loop_step1:
    CMP  R2, R3
    BGE  scalar_step1

    // Load data[i-1], data[i], data[i+1] for 4 odd indices
    // i=1: needs 0,1,2
    // i=3: needs 2,3,4
    // i=5: needs 4,5,6
    // i=7: needs 6,7,8

    LSL  $2, R3, R4              // R4 = i * 4 (byte offset)
    SUB  $4, R4, R5              // R5 = (i-1) * 4
    ADD  R0, R5, R5              // R5 = &data[i-1]

    VLD1.P 64(R5), [V0.S4, V1.S4, V2.S4, V3.S4]  // load 16 values

    // Extract odd positions: V0[1], V0[3], V1[1], V1[3] etc
    // For simplicity, use scalar for now and optimize later

    ADD  $8, R3, R3
    B    vector_loop_step1

scalar_step1:
    // Scalar fallback
    MOVD $1, R3                  // i = 1

scalar_loop_step1:
    CMP  R1, R3
    BGE  done_step1

    SUB  $1, R3, R4              // i-1
    ADD  $1, R3, R5              // i+1

    // Check bounds for i+1
    CMP  R1, R5
    BGE  handle_last_step1

    LSL  $2, R4, R4              // byte offset for i-1
    LSL  $2, R5, R5              // byte offset for i+1
    LSL  $2, R3, R6              // byte offset for i

    ADD  R0, R4, R4              // &data[i-1]
    ADD  R0, R5, R5              // &data[i+1]
    ADD  R0, R6, R6              // &data[i]

    MOVW (R4), R7                // data[i-1]
    MOVW (R5), R8                // data[i+1]
    MOVW (R6), R9                // data[i]

    ADD  R7, R8, R7              // data[i-1] + data[i+1]
    ASR  $1, R7, R7              // >> 1
    SUB  R7, R9, R9              // data[i] - sum

    MOVW R9, (R6)                // store result

    ADD  $2, R3, R3              // i += 2
    B    scalar_loop_step1

handle_last_step1:
    // Handle last odd element if length is even
    AND  $1, R1, R4
    CBNZ R4, done_step1          // if odd length, skip

    SUB  $1, R1, R3              // i = length - 1
    LSL  $2, R3, R4              // byte offset
    ADD  R0, R4, R4              // &data[length-1]

    SUB  $4, R4, R5              // &data[length-2]

    MOVW (R4), R6                // data[length-1]
    MOVW (R5), R7                // data[length-2]

    SUB  R7, R6, R6              // data[length-1] - data[length-2]
    MOVW R6, (R4)

done_step1:
    RET

// func liftStep2_53_neon(data []int32, length int)
// Performs: data[i] += (data[i-1] + data[i+1] + 2) >> 2 for even indices
TEXT ·liftStep2_53_neon(SB), NOSPLIT, $0-32
    MOVD data_base+0(FP), R0     // data pointer
    MOVD length+24(FP), R1       // length

    CMP  $2, R1
    BLT  done_step2

    // Handle first element: data[0] += (data[1] + data[1] + 2) >> 2
    MOVW (R0), R2                // data[0]
    MOVW 4(R0), R3               // data[1]
    ADD  R3, R3, R4              // 2 * data[1]
    ADD  $2, R4, R4              // + 2
    ASR  $2, R4, R4              // >> 2
    ADD  R4, R2, R2
    MOVW R2, (R0)

    // Process even indices 2, 4, 6, ...
    MOVD $2, R3                  // i = 2

scalar_loop_step2:
    SUB  $1, R1, R4              // length - 1
    CMP  R4, R3
    BGE  handle_last_step2

    LSL  $2, R3, R4              // byte offset for i
    SUB  $4, R4, R5              // byte offset for i-1
    ADD  $4, R4, R6              // byte offset for i+1

    ADD  R0, R4, R4              // &data[i]
    ADD  R0, R5, R5              // &data[i-1]
    ADD  R0, R6, R6              // &data[i+1]

    MOVW (R4), R7                // data[i]
    MOVW (R5), R8                // data[i-1]
    MOVW (R6), R9                // data[i+1]

    ADD  R8, R9, R8              // data[i-1] + data[i+1]
    ADD  $2, R8, R8              // + 2
    ASR  $2, R8, R8              // >> 2
    ADD  R8, R7, R7              // data[i] + sum

    MOVW R7, (R4)                // store result

    ADD  $2, R3, R3              // i += 2
    B    scalar_loop_step2

handle_last_step2:
    // Handle last even element if length is odd
    AND  $1, R1, R4
    CBZ  R4, done_step2          // if even length, done

    SUB  $1, R1, R3              // i = length - 1
    LSL  $2, R3, R4              // byte offset
    ADD  R0, R4, R4              // &data[length-1]

    SUB  $4, R4, R5              // &data[length-2]

    MOVW (R4), R6                // data[length-1]
    MOVW (R5), R7                // data[length-2]

    ADD  R7, R7, R7              // 2 * data[length-2]
    ADD  $2, R7, R7              // + 2
    ASR  $2, R7, R7              // >> 2
    ADD  R7, R6, R6
    MOVW R6, (R4)

done_step2:
    RET

// func clearInt32Slice_neon(data []int32)
// Fast zeroing using NEON stores
TEXT ·clearInt32Slice_neon(SB), NOSPLIT, $0-24
    MOVD data_base+0(FP), R0     // data pointer
    MOVD data_len+8(FP), R1      // length

    CBZ  R1, done_clear

    // Zero register
    VEOR V0.B16, V0.B16, V0.B16
    VEOR V1.B16, V1.B16, V1.B16
    VEOR V2.B16, V2.B16, V2.B16
    VEOR V3.B16, V3.B16, V3.B16

    // Process 16 int32s (64 bytes) at a time
    LSR  $4, R1, R2              // R2 = length / 16
    CBZ  R2, small_clear

    MOVD R2, R3                  // loop counter

vector_clear_loop:
    VST1.P [V0.S4, V1.S4, V2.S4, V3.S4], 64(R0)
    SUB  $1, R3, R3
    CBNZ R3, vector_clear_loop

small_clear:
    // Handle remaining elements
    AND  $15, R1, R2             // remaining = length % 16
    CBZ  R2, done_clear

remaining_clear_loop:
    MOVW ZR, (R0)
    ADD  $4, R0, R0
    SUB  $1, R2, R2
    CBNZ R2, remaining_clear_loop

done_clear:
    RET
