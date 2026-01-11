//go:build amd64

#include "textflag.h"

// func liftStep1_53_avx(data []int32, length int)
// Performs: data[i] -= (data[i-1] + data[i+1]) >> 1 for odd indices
// Uses AVX2 for vectorized processing
TEXT ·liftStep1_53_avx(SB), NOSPLIT, $0-32
    MOVQ data_base+0(FP), SI     // data pointer
    MOVQ length+24(FP), CX       // length

    CMPQ CX, $2
    JL   done_step1

    // Process odd indices: 1, 3, 5, ...
    MOVQ $1, BX                  // i = 1

scalar_loop_step1:
    MOVQ CX, DX
    SUBQ $1, DX                  // length - 1
    CMPQ BX, DX
    JGE  handle_last_step1

    // Calculate offsets
    MOVQ BX, DX
    SHLQ $2, DX                  // i * 4
    LEAQ (SI)(DX*1), R8          // &data[i]
    LEAQ -4(R8), R9              // &data[i-1]
    LEAQ 4(R8), R10              // &data[i+1]

    // Load values
    MOVL (R8), AX                // data[i]
    MOVL (R9), R11               // data[i-1]
    MOVL (R10), R12              // data[i+1]

    // Compute: data[i] -= (data[i-1] + data[i+1]) >> 1
    ADDL R12, R11                // data[i-1] + data[i+1]
    SARL $1, R11                 // >> 1
    SUBL R11, AX                 // data[i] - sum

    MOVL AX, (R8)                // store result

    ADDQ $2, BX                  // i += 2
    JMP  scalar_loop_step1

handle_last_step1:
    // Handle last odd element if length is even
    MOVQ CX, DX
    ANDQ $1, DX
    JNZ  done_step1              // if odd length, skip

    MOVQ CX, BX
    SUBQ $1, BX                  // i = length - 1
    SHLQ $2, BX                  // byte offset
    LEAQ (SI)(BX*1), R8          // &data[length-1]

    MOVL (R8), AX                // data[length-1]
    MOVL -4(R8), R11             // data[length-2]

    SUBL R11, AX                 // data[length-1] - data[length-2]
    MOVL AX, (R8)

done_step1:
    RET

// func liftStep2_53_avx(data []int32, length int)
// Performs: data[i] += (data[i-1] + data[i+1] + 2) >> 2 for even indices
TEXT ·liftStep2_53_avx(SB), NOSPLIT, $0-32
    MOVQ data_base+0(FP), SI     // data pointer
    MOVQ length+24(FP), CX       // length

    CMPQ CX, $2
    JL   done_step2

    // Handle first element: data[0] += (data[1] + data[1] + 2) >> 2
    MOVL (SI), AX                // data[0]
    MOVL 4(SI), R11              // data[1]
    ADDL R11, R11                // 2 * data[1]
    ADDL $2, R11                 // + 2
    SARL $2, R11                 // >> 2
    ADDL R11, AX
    MOVL AX, (SI)

    // Process even indices 2, 4, 6, ...
    MOVQ $2, BX                  // i = 2

scalar_loop_step2:
    MOVQ CX, DX
    SUBQ $1, DX                  // length - 1
    CMPQ BX, DX
    JGE  handle_last_step2

    // Calculate offsets
    MOVQ BX, DX
    SHLQ $2, DX                  // i * 4
    LEAQ (SI)(DX*1), R8          // &data[i]

    // Load values
    MOVL (R8), AX                // data[i]
    MOVL -4(R8), R11             // data[i-1]
    MOVL 4(R8), R12              // data[i+1]

    // Compute: data[i] += (data[i-1] + data[i+1] + 2) >> 2
    ADDL R12, R11                // data[i-1] + data[i+1]
    ADDL $2, R11                 // + 2
    SARL $2, R11                 // >> 2
    ADDL R11, AX                 // data[i] + sum

    MOVL AX, (R8)                // store result

    ADDQ $2, BX                  // i += 2
    JMP  scalar_loop_step2

handle_last_step2:
    // Handle last even element if length is odd
    MOVQ CX, DX
    ANDQ $1, DX
    JZ   done_step2              // if even length, done

    MOVQ CX, BX
    SUBQ $1, BX                  // i = length - 1
    SHLQ $2, BX                  // byte offset
    LEAQ (SI)(BX*1), R8          // &data[length-1]

    MOVL (R8), AX                // data[length-1]
    MOVL -4(R8), R11             // data[length-2]

    ADDL R11, R11                // 2 * data[length-2]
    ADDL $2, R11                 // + 2
    SARL $2, R11                 // >> 2
    ADDL R11, AX
    MOVL AX, (R8)

done_step2:
    RET

// func clearInt32Slice_avx(data []int32)
// Fast zeroing using AVX stores
TEXT ·clearInt32Slice_avx(SB), NOSPLIT, $0-24
    MOVQ data_base+0(FP), SI     // data pointer
    MOVQ data_len+8(FP), CX      // length

    TESTQ CX, CX
    JZ   done_clear

    // Zero XMM register
    VPXOR X0, X0, X0
    VPXOR Y0, Y0, Y0             // AVX 256-bit zero

    // Process 8 int32s (32 bytes) at a time with AVX
    MOVQ CX, DX
    SHRQ $3, DX                  // length / 8
    JZ   small_clear

avx_clear_loop:
    VMOVDQU Y0, (SI)
    ADDQ $32, SI
    DECQ DX
    JNZ  avx_clear_loop

small_clear:
    // Handle remaining elements (0-7)
    ANDQ $7, CX
    JZ   done_clear

remaining_clear_loop:
    MOVL $0, (SI)
    ADDQ $4, SI
    DECQ CX
    JNZ  remaining_clear_loop

done_clear:
    VZEROUPPER                   // Clear upper YMM bits
    RET
