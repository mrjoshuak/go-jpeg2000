//go:build !purego

package entropy

import (
	"unsafe"
)

// EncodeFast5 uses pointer increments in inner loops for better performance.
func (t *T1) EncodeFast5(bandType int) []byte {
	t.bandType = bandType

	// Find number of bit-planes
	maxVal := int32(0)
	for _, v := range t.data {
		if v > maxVal {
			maxVal = v
		}
	}
	if maxVal == 0 {
		return nil
	}
	numBPS := 0
	for maxVal > 0 {
		numBPS++
		maxVal >>= 1
	}
	t.numBPS = numBPS

	width := t.width
	height := t.height
	stride := width + 2
	bandOffset := bandType * 256

	// Pre-compute constant offsets
	offsetN := -stride
	offsetS := stride
	offsetNW := -stride - 1
	offsetNE := -stride + 1
	offsetSW := stride - 1
	offsetSE := stride + 1

	// Initialize MQ state as locals
	mqA := uint32(0x8000)
	mqC := uint32(0)
	mqCT := uint32(12)
	estimatedSize := width*height*2 + 1024
	if estimatedSize < 16384 {
		estimatedSize = 16384
	}
	if cap(t.mqBuf) >= estimatedSize {
		t.mqBuf = t.mqBuf[:cap(t.mqBuf)]
	} else {
		t.mqBuf = make([]byte, estimatedSize)
	}
	t.mqBuf[0] = 0
	mqBp := 0
	mqBuf := t.mqBuf
	var mqContexts [NumContexts]uint8
	mqContexts[CtxUni] = 92

	flags := t.flags
	data := t.data
	flagsBase := unsafe.Pointer(&flags[0])
	dataBase := unsafe.Pointer(&data[0])

	// Encode each bit-plane
	for bp := numBPS - 1; bp >= 0; bp-- {
		bit := int32(1) << bp

		// ============ SIGNIFICANCE PROPAGATION PASS ============
		for y := 0; y < height; y++ {
			rowStart := (y + 1) * stride
			dataRowStart := y * width
			isFirstRow := y == 0
			isLastRow := y == height-1

			// Get pointer to start of row (at x=0, which is index rowStart+1)
			fRowPtr := unsafe.Add(flagsBase, rowStart+1)
			dRowPtr := unsafe.Add(dataBase, dataRowStart*4)

			for x := 0; x < width; x++ {
				fPtr := unsafe.Add(fRowPtr, x)
				f := *(*T1Flags)(fPtr)

				if f&T1Sig != 0 {
					continue
				}

				// Quick check using cardinal neighbor flags
				cardinalSigs := f & (T1SigN | T1SigS | T1SigE | T1SigW)

				var fW, fE, fN, fS, fNW, fNE, fSW, fSE T1Flags
				if cardinalSigs == 0 {
					// No cardinal neighbors significant - only check diagonals
					fNW = *(*T1Flags)(unsafe.Add(fPtr, offsetNW))
					fNE = *(*T1Flags)(unsafe.Add(fPtr, offsetNE))
					fSW = *(*T1Flags)(unsafe.Add(fPtr, offsetSW))
					fSE = *(*T1Flags)(unsafe.Add(fPtr, offsetSE))
					if (fNW|fNE|fSW|fSE)&T1Sig == 0 {
						continue
					}
				} else {
					fW = *(*T1Flags)(unsafe.Add(fPtr, -1))
					fE = *(*T1Flags)(unsafe.Add(fPtr, 1))
					fN = *(*T1Flags)(unsafe.Add(fPtr, offsetN))
					fS = *(*T1Flags)(unsafe.Add(fPtr, offsetS))
					fNW = *(*T1Flags)(unsafe.Add(fPtr, offsetNW))
					fNE = *(*T1Flags)(unsafe.Add(fPtr, offsetNE))
					fSW = *(*T1Flags)(unsafe.Add(fPtr, offsetSW))
					fSE = *(*T1Flags)(unsafe.Add(fPtr, offsetSE))
				}

				coeff := *(*int32)(unsafe.Add(dRowPtr, x*4))
				sig := int(coeff>>bp) & 1

				// Build ZC context
				packed := uint8(fW&T1Sig) |
					(uint8(fE&T1Sig) << 1) |
					(uint8(fN&T1Sig) << 2) |
					(uint8(fS&T1Sig) << 3) |
					(uint8(fNW&T1Sig) << 4) |
					(uint8(fNE&T1Sig) << 5) |
					(uint8(fSW&T1Sig) << 6) |
					(uint8(fSE&T1Sig) << 7)
				ctx := int(lutZCCtx[bandOffset+int(packed)])

				// INLINE MQ ENCODE
				stateIdx := mqContexts[ctx]
				qe := mqQe[stateIdx]
				mps := stateIdx & 1
				mqA -= qe

				if uint8(sig) == mps {
					if (mqA & 0x8000) == 0 {
						if mqA < qe {
							mqA = qe
						} else {
							mqC += qe
						}
						mqContexts[ctx] = mqNMPS[stateIdx]
						for (mqA & 0x8000) == 0 {
							mqA <<= 1
							mqC <<= 1
							mqCT--
							if mqCT == 0 {
								mqBp, mqC, mqCT = mqByteOutLocal(mqBuf, mqBp, mqC)
							}
						}
					} else {
						mqC += qe
					}
				} else {
					if mqA < qe {
						mqC += qe
					} else {
						mqA = qe
					}
					mqContexts[ctx] = mqNLPS[stateIdx]
					for (mqA & 0x8000) == 0 {
						mqA <<= 1
						mqC <<= 1
						mqCT--
						if mqCT == 0 {
							mqBp, mqC, mqCT = mqByteOutLocal(mqBuf, mqBp, mqC)
						}
					}
				}

				if sig != 0 {
					wSig := int(fW&T1Sig) >> 0
					wChi := int(fW&T1SignNeg) >> 3
					eSig := int(fE&T1Sig) >> 0
					eChi := int(fE&T1SignNeg) >> 3
					nSig := int(fN&T1Sig) >> 0
					nChi := int(fN&T1SignNeg) >> 3
					sSig := int(fS&T1Sig) >> 0
					sChi := int(fS&T1SignNeg) >> 3

					scIdx := wSig | (wChi << 1) | (eSig << 2) | (eChi << 3) |
						(nSig << 4) | (nChi << 5) | (sSig << 6) | (sChi << 7)

					ctx := int(lutSignCtx[scIdx]) + CtxSC0
					pred := int(lutSignPred[scIdx])

					sign := 0
					if f&T1SignNeg != 0 {
						sign = 1
					}
					decision := sign ^ pred

					stateIdx := mqContexts[ctx]
					qe := mqQe[stateIdx]
					mps := stateIdx & 1
					mqA -= qe

					if uint8(decision) == mps {
						if (mqA & 0x8000) == 0 {
							if mqA < qe {
								mqA = qe
							} else {
								mqC += qe
							}
							mqContexts[ctx] = mqNMPS[stateIdx]
							for (mqA & 0x8000) == 0 {
								mqA <<= 1
								mqC <<= 1
								mqCT--
								if mqCT == 0 {
									mqBp, mqC, mqCT = mqByteOutLocal(mqBuf, mqBp, mqC)
								}
							}
						} else {
							mqC += qe
						}
					} else {
						if mqA < qe {
							mqC += qe
						} else {
							mqA = qe
						}
						mqContexts[ctx] = mqNLPS[stateIdx]
						for (mqA & 0x8000) == 0 {
							mqA <<= 1
							mqC <<= 1
							mqCT--
							if mqCT == 0 {
								mqBp, mqC, mqCT = mqByteOutLocal(mqBuf, mqBp, mqC)
							}
						}
					}

					*(*T1Flags)(fPtr) |= T1Sig
					if !isFirstRow {
						*(*T1Flags)(unsafe.Add(fPtr, offsetN)) |= T1SigS
					}
					if !isLastRow {
						*(*T1Flags)(unsafe.Add(fPtr, offsetS)) |= T1SigN
					}
					if x > 0 {
						*(*T1Flags)(unsafe.Add(fPtr, -1)) |= T1SigE
					}
					if x < width-1 {
						*(*T1Flags)(unsafe.Add(fPtr, 1)) |= T1SigW
					}
				}
				*(*T1Flags)(fPtr) |= T1Visit
			}
		}

		// ============ MAGNITUDE REFINEMENT PASS ============
		for y := 0; y < height; y++ {
			rowStart := (y + 1) * stride
			dataRowStart := y * width

			fRowPtr := unsafe.Add(flagsBase, rowStart+1)
			dRowPtr := unsafe.Add(dataBase, dataRowStart*4)

			for x := 0; x < width; x++ {
				fPtr := unsafe.Add(fRowPtr, x)
				f := *(*T1Flags)(fPtr)

				if f&T1Sig == 0 || f&T1Visit != 0 {
					continue
				}

				coeff := *(*int32)(unsafe.Add(dRowPtr, x*4))
				refBit := 0
				if coeff&bit != 0 {
					refBit = 1
				}

				var ctx int
				if f&T1Refine == 0 {
					fW := *(*T1Flags)(unsafe.Add(fPtr, -1))
					fE := *(*T1Flags)(unsafe.Add(fPtr, 1))
					fN := *(*T1Flags)(unsafe.Add(fPtr, offsetN))
					fS := *(*T1Flags)(unsafe.Add(fPtr, offsetS))
					fNW := *(*T1Flags)(unsafe.Add(fPtr, offsetNW))
					fNE := *(*T1Flags)(unsafe.Add(fPtr, offsetNE))
					fSW := *(*T1Flags)(unsafe.Add(fPtr, offsetSW))
					fSE := *(*T1Flags)(unsafe.Add(fPtr, offsetSE))
					if (fW|fE|fN|fS|fNW|fNE|fSW|fSE)&T1Sig != 0 {
						ctx = CtxMag1
					} else {
						ctx = CtxMag0
					}
				} else {
					ctx = CtxMag2
				}

				stateIdx := mqContexts[ctx]
				qe := mqQe[stateIdx]
				mps := stateIdx & 1
				mqA -= qe

				if uint8(refBit) == mps {
					if (mqA & 0x8000) == 0 {
						if mqA < qe {
							mqA = qe
						} else {
							mqC += qe
						}
						mqContexts[ctx] = mqNMPS[stateIdx]
						for (mqA & 0x8000) == 0 {
							mqA <<= 1
							mqC <<= 1
							mqCT--
							if mqCT == 0 {
								mqBp, mqC, mqCT = mqByteOutLocal(mqBuf, mqBp, mqC)
							}
						}
					} else {
						mqC += qe
					}
				} else {
					if mqA < qe {
						mqC += qe
					} else {
						mqA = qe
					}
					mqContexts[ctx] = mqNLPS[stateIdx]
					for (mqA & 0x8000) == 0 {
						mqA <<= 1
						mqC <<= 1
						mqCT--
						if mqCT == 0 {
							mqBp, mqC, mqCT = mqByteOutLocal(mqBuf, mqBp, mqC)
						}
					}
				}

				*(*T1Flags)(fPtr) |= T1Refine
			}
		}

		// ============ CLEANUP PASS ============
		for y := 0; y < height; y += 4 {
			for x := 0; x < width; x++ {
				canRL := y+4 <= height
				if canRL {
					for yy := 0; yy < 4; yy++ {
						idx := (y+yy+1)*stride + x + 1
						fPtr := unsafe.Add(flagsBase, idx)
						f := *(*T1Flags)(fPtr)
						if f&(T1Sig|T1Visit) != 0 {
							canRL = false
							break
						}
						fW := *(*T1Flags)(unsafe.Add(fPtr, -1))
						fE := *(*T1Flags)(unsafe.Add(fPtr, 1))
						fN := *(*T1Flags)(unsafe.Add(fPtr, offsetN))
						fS := *(*T1Flags)(unsafe.Add(fPtr, offsetS))
						fNW := *(*T1Flags)(unsafe.Add(fPtr, offsetNW))
						fNE := *(*T1Flags)(unsafe.Add(fPtr, offsetNE))
						fSW := *(*T1Flags)(unsafe.Add(fPtr, offsetSW))
						fSE := *(*T1Flags)(unsafe.Add(fPtr, offsetSE))
						if (fW|fE|fN|fS|fNW|fNE|fSW|fSE)&T1Sig != 0 {
							canRL = false
							break
						}
					}
				}

				if canRL {
					firstSig := -1
					for i := 0; i < 4; i++ {
						coeff := *(*int32)(unsafe.Add(dataBase, ((y+i)*width+x)*4))
						if coeff&bit != 0 {
							firstSig = i
							break
						}
					}

					decision := 0
					if firstSig >= 0 {
						decision = 1
					}

					ctx := CtxRL
					stateIdx := mqContexts[ctx]
					qe := mqQe[stateIdx]
					mps := stateIdx & 1
					mqA -= qe

					if uint8(decision) == mps {
						if (mqA & 0x8000) == 0 {
							if mqA < qe {
								mqA = qe
							} else {
								mqC += qe
							}
							mqContexts[ctx] = mqNMPS[stateIdx]
							for (mqA & 0x8000) == 0 {
								mqA <<= 1
								mqC <<= 1
								mqCT--
								if mqCT == 0 {
									mqBp, mqC, mqCT = mqByteOutLocal(mqBuf, mqBp, mqC)
								}
							}
						} else {
							mqC += qe
						}
					} else {
						if mqA < qe {
							mqC += qe
						} else {
							mqA = qe
						}
						mqContexts[ctx] = mqNLPS[stateIdx]
						for (mqA & 0x8000) == 0 {
							mqA <<= 1
							mqC <<= 1
							mqCT--
							if mqCT == 0 {
								mqBp, mqC, mqCT = mqByteOutLocal(mqBuf, mqBp, mqC)
							}
						}
					}

					if firstSig < 0 {
						continue
					}

					// Position bits
					for _, posBit := range []int{(firstSig >> 1) & 1, firstSig & 1} {
						ctx := CtxUni
						stateIdx := mqContexts[ctx]
						qe := mqQe[stateIdx]
						mps := stateIdx & 1
						mqA -= qe

						if uint8(posBit) == mps {
							if (mqA & 0x8000) == 0 {
								if mqA < qe {
									mqA = qe
								} else {
									mqC += qe
								}
								mqContexts[ctx] = mqNMPS[stateIdx]
								for (mqA & 0x8000) == 0 {
									mqA <<= 1
									mqC <<= 1
									mqCT--
									if mqCT == 0 {
										mqBp, mqC, mqCT = mqByteOutLocal(mqBuf, mqBp, mqC)
									}
								}
							} else {
								mqC += qe
							}
						} else {
							if mqA < qe {
								mqC += qe
							} else {
								mqA = qe
							}
							mqContexts[ctx] = mqNLPS[stateIdx]
							for (mqA & 0x8000) == 0 {
								mqA <<= 1
								mqC <<= 1
								mqCT--
								if mqCT == 0 {
									mqBp, mqC, mqCT = mqByteOutLocal(mqBuf, mqBp, mqC)
								}
							}
						}
					}

					// First significant sample
					yy := y + firstSig
					idx := (yy+1)*stride + x + 1
					fPtr := unsafe.Add(flagsBase, idx)
					f := *(*T1Flags)(fPtr)
					fW := *(*T1Flags)(unsafe.Add(fPtr, -1))
					fE := *(*T1Flags)(unsafe.Add(fPtr, 1))
					fN := *(*T1Flags)(unsafe.Add(fPtr, offsetN))
					fS := *(*T1Flags)(unsafe.Add(fPtr, offsetS))

					wSig := int(fW&T1Sig) >> 0
					wChi := int(fW&T1SignNeg) >> 3
					eSig := int(fE&T1Sig) >> 0
					eChi := int(fE&T1SignNeg) >> 3
					nSig := int(fN&T1Sig) >> 0
					nChi := int(fN&T1SignNeg) >> 3
					sSig := int(fS&T1Sig) >> 0
					sChi := int(fS&T1SignNeg) >> 3

					scIdx := wSig | (wChi << 1) | (eSig << 2) | (eChi << 3) |
						(nSig << 4) | (nChi << 5) | (sSig << 6) | (sChi << 7)

					signCtx := int(lutSignCtx[scIdx]) + CtxSC0
					pred := int(lutSignPred[scIdx])

					sign := 0
					if f&T1SignNeg != 0 {
						sign = 1
					}

					ctx = signCtx
					stateIdx = mqContexts[ctx]
					qe = mqQe[stateIdx]
					mps = stateIdx & 1
					mqA -= qe
					decision = sign ^ pred

					if uint8(decision) == mps {
						if (mqA & 0x8000) == 0 {
							if mqA < qe {
								mqA = qe
							} else {
								mqC += qe
							}
							mqContexts[ctx] = mqNMPS[stateIdx]
							for (mqA & 0x8000) == 0 {
								mqA <<= 1
								mqC <<= 1
								mqCT--
								if mqCT == 0 {
									mqBp, mqC, mqCT = mqByteOutLocal(mqBuf, mqBp, mqC)
								}
							}
						} else {
							mqC += qe
						}
					} else {
						if mqA < qe {
							mqC += qe
						} else {
							mqA = qe
						}
						mqContexts[ctx] = mqNLPS[stateIdx]
						for (mqA & 0x8000) == 0 {
							mqA <<= 1
							mqC <<= 1
							mqCT--
							if mqCT == 0 {
								mqBp, mqC, mqCT = mqByteOutLocal(mqBuf, mqBp, mqC)
							}
						}
					}

					*(*T1Flags)(fPtr) |= T1Sig
					if yy > 0 {
						*(*T1Flags)(unsafe.Add(fPtr, offsetN)) |= T1SigS
					}
					if yy < height-1 {
						*(*T1Flags)(unsafe.Add(fPtr, offsetS)) |= T1SigN
					}
					if x > 0 {
						*(*T1Flags)(unsafe.Add(fPtr, -1)) |= T1SigE
					}
					if x < width-1 {
						*(*T1Flags)(unsafe.Add(fPtr, 1)) |= T1SigW
					}

					// Remaining samples
					for i := firstSig + 1; i < 4; i++ {
						yy := y + i
						idx := (yy+1)*stride + x + 1
						fPtr := unsafe.Add(flagsBase, idx)
						f := *(*T1Flags)(fPtr)

						coeff := *(*int32)(unsafe.Add(dataBase, (yy*width+x)*4))
						sig := 0
						if coeff&bit != 0 {
							sig = 1
						}

						fW := *(*T1Flags)(unsafe.Add(fPtr, -1))
						fE := *(*T1Flags)(unsafe.Add(fPtr, 1))
						fN := *(*T1Flags)(unsafe.Add(fPtr, offsetN))
						fS := *(*T1Flags)(unsafe.Add(fPtr, offsetS))
						fNW := *(*T1Flags)(unsafe.Add(fPtr, offsetNW))
						fNE := *(*T1Flags)(unsafe.Add(fPtr, offsetNE))
						fSW := *(*T1Flags)(unsafe.Add(fPtr, offsetSW))
						fSE := *(*T1Flags)(unsafe.Add(fPtr, offsetSE))

						packed := uint8(fW&T1Sig) |
							(uint8(fE&T1Sig) << 1) |
							(uint8(fN&T1Sig) << 2) |
							(uint8(fS&T1Sig) << 3) |
							(uint8(fNW&T1Sig) << 4) |
							(uint8(fNE&T1Sig) << 5) |
							(uint8(fSW&T1Sig) << 6) |
							(uint8(fSE&T1Sig) << 7)
						ctx := int(lutZCCtx[bandOffset+int(packed)])

						stateIdx := mqContexts[ctx]
						qe := mqQe[stateIdx]
						mps := stateIdx & 1
						mqA -= qe

						if uint8(sig) == mps {
							if (mqA & 0x8000) == 0 {
								if mqA < qe {
									mqA = qe
								} else {
									mqC += qe
								}
								mqContexts[ctx] = mqNMPS[stateIdx]
								for (mqA & 0x8000) == 0 {
									mqA <<= 1
									mqC <<= 1
									mqCT--
									if mqCT == 0 {
										mqBp, mqC, mqCT = mqByteOutLocal(mqBuf, mqBp, mqC)
									}
								}
							} else {
								mqC += qe
							}
						} else {
							if mqA < qe {
								mqC += qe
							} else {
								mqA = qe
							}
							mqContexts[ctx] = mqNLPS[stateIdx]
							for (mqA & 0x8000) == 0 {
								mqA <<= 1
								mqC <<= 1
								mqCT--
								if mqCT == 0 {
									mqBp, mqC, mqCT = mqByteOutLocal(mqBuf, mqBp, mqC)
								}
							}
						}

						if sig != 0 {
							wSig := int(fW&T1Sig) >> 0
							wChi := int(fW&T1SignNeg) >> 3
							eSig := int(fE&T1Sig) >> 0
							eChi := int(fE&T1SignNeg) >> 3
							nSig := int(fN&T1Sig) >> 0
							nChi := int(fN&T1SignNeg) >> 3
							sSig := int(fS&T1Sig) >> 0
							sChi := int(fS&T1SignNeg) >> 3

							scIdx := wSig | (wChi << 1) | (eSig << 2) | (eChi << 3) |
								(nSig << 4) | (nChi << 5) | (sSig << 6) | (sChi << 7)

							signCtx := int(lutSignCtx[scIdx]) + CtxSC0
							pred := int(lutSignPred[scIdx])

							sign := 0
							if f&T1SignNeg != 0 {
								sign = 1
							}
							decision := sign ^ pred

							stateIdx := mqContexts[signCtx]
							qe := mqQe[stateIdx]
							mps := stateIdx & 1
							mqA -= qe

							if uint8(decision) == mps {
								if (mqA & 0x8000) == 0 {
									if mqA < qe {
										mqA = qe
									} else {
										mqC += qe
									}
									mqContexts[signCtx] = mqNMPS[stateIdx]
									for (mqA & 0x8000) == 0 {
										mqA <<= 1
										mqC <<= 1
										mqCT--
										if mqCT == 0 {
											mqBp, mqC, mqCT = mqByteOutLocal(mqBuf, mqBp, mqC)
										}
									}
								} else {
									mqC += qe
								}
							} else {
								if mqA < qe {
									mqC += qe
								} else {
									mqA = qe
								}
								mqContexts[signCtx] = mqNLPS[stateIdx]
								for (mqA & 0x8000) == 0 {
									mqA <<= 1
									mqC <<= 1
									mqCT--
									if mqCT == 0 {
										mqBp, mqC, mqCT = mqByteOutLocal(mqBuf, mqBp, mqC)
									}
								}
							}

							*(*T1Flags)(fPtr) |= T1Sig
							if yy > 0 {
								*(*T1Flags)(unsafe.Add(fPtr, offsetN)) |= T1SigS
							}
							if yy < height-1 {
								*(*T1Flags)(unsafe.Add(fPtr, offsetS)) |= T1SigN
							}
							if x > 0 {
								*(*T1Flags)(unsafe.Add(fPtr, -1)) |= T1SigE
							}
							if x < width-1 {
								*(*T1Flags)(unsafe.Add(fPtr, 1)) |= T1SigW
							}
						}
					}
					continue
				}

				// Non run-length cleanup
				yEnd := y + 4
				if yEnd > height {
					yEnd = height
				}
				for yy := y; yy < yEnd; yy++ {
					idx := (yy+1)*stride + x + 1
					fPtr := unsafe.Add(flagsBase, idx)
					f := *(*T1Flags)(fPtr)

					if f&T1Visit != 0 {
						*(*T1Flags)(fPtr) &^= T1Visit
						continue
					}
					if f&T1Sig != 0 {
						continue
					}

					coeff := *(*int32)(unsafe.Add(dataBase, (yy*width+x)*4))
					sig := 0
					if coeff&bit != 0 {
						sig = 1
					}

					fW := *(*T1Flags)(unsafe.Add(fPtr, -1))
					fE := *(*T1Flags)(unsafe.Add(fPtr, 1))
					fN := *(*T1Flags)(unsafe.Add(fPtr, offsetN))
					fS := *(*T1Flags)(unsafe.Add(fPtr, offsetS))
					fNW := *(*T1Flags)(unsafe.Add(fPtr, offsetNW))
					fNE := *(*T1Flags)(unsafe.Add(fPtr, offsetNE))
					fSW := *(*T1Flags)(unsafe.Add(fPtr, offsetSW))
					fSE := *(*T1Flags)(unsafe.Add(fPtr, offsetSE))

					packed := uint8(fW&T1Sig) |
						(uint8(fE&T1Sig) << 1) |
						(uint8(fN&T1Sig) << 2) |
						(uint8(fS&T1Sig) << 3) |
						(uint8(fNW&T1Sig) << 4) |
						(uint8(fNE&T1Sig) << 5) |
						(uint8(fSW&T1Sig) << 6) |
						(uint8(fSE&T1Sig) << 7)
					ctx := int(lutZCCtx[bandOffset+int(packed)])

					stateIdx := mqContexts[ctx]
					qe := mqQe[stateIdx]
					mps := stateIdx & 1
					mqA -= qe

					if uint8(sig) == mps {
						if (mqA & 0x8000) == 0 {
							if mqA < qe {
								mqA = qe
							} else {
								mqC += qe
							}
							mqContexts[ctx] = mqNMPS[stateIdx]
							for (mqA & 0x8000) == 0 {
								mqA <<= 1
								mqC <<= 1
								mqCT--
								if mqCT == 0 {
									mqBp, mqC, mqCT = mqByteOutLocal(mqBuf, mqBp, mqC)
								}
							}
						} else {
							mqC += qe
						}
					} else {
						if mqA < qe {
							mqC += qe
						} else {
							mqA = qe
						}
						mqContexts[ctx] = mqNLPS[stateIdx]
						for (mqA & 0x8000) == 0 {
							mqA <<= 1
							mqC <<= 1
							mqCT--
							if mqCT == 0 {
								mqBp, mqC, mqCT = mqByteOutLocal(mqBuf, mqBp, mqC)
							}
						}
					}

					if sig != 0 {
						wSig := int(fW&T1Sig) >> 0
						wChi := int(fW&T1SignNeg) >> 3
						eSig := int(fE&T1Sig) >> 0
						eChi := int(fE&T1SignNeg) >> 3
						nSig := int(fN&T1Sig) >> 0
						nChi := int(fN&T1SignNeg) >> 3
						sSig := int(fS&T1Sig) >> 0
						sChi := int(fS&T1SignNeg) >> 3

						scIdx := wSig | (wChi << 1) | (eSig << 2) | (eChi << 3) |
							(nSig << 4) | (nChi << 5) | (sSig << 6) | (sChi << 7)

						signCtx := int(lutSignCtx[scIdx]) + CtxSC0
						pred := int(lutSignPred[scIdx])

						sign := 0
						if f&T1SignNeg != 0 {
							sign = 1
						}
						decision := sign ^ pred

						stateIdx := mqContexts[signCtx]
						qe := mqQe[stateIdx]
						mps := stateIdx & 1
						mqA -= qe

						if uint8(decision) == mps {
							if (mqA & 0x8000) == 0 {
								if mqA < qe {
									mqA = qe
								} else {
									mqC += qe
								}
								mqContexts[signCtx] = mqNMPS[stateIdx]
								for (mqA & 0x8000) == 0 {
									mqA <<= 1
									mqC <<= 1
									mqCT--
									if mqCT == 0 {
										mqBp, mqC, mqCT = mqByteOutLocal(mqBuf, mqBp, mqC)
									}
								}
							} else {
								mqC += qe
							}
						} else {
							if mqA < qe {
								mqC += qe
							} else {
								mqA = qe
							}
							mqContexts[signCtx] = mqNLPS[stateIdx]
							for (mqA & 0x8000) == 0 {
								mqA <<= 1
								mqC <<= 1
								mqCT--
								if mqCT == 0 {
									mqBp, mqC, mqCT = mqByteOutLocal(mqBuf, mqBp, mqC)
								}
							}
						}

						*(*T1Flags)(fPtr) |= T1Sig
						if yy > 0 {
							*(*T1Flags)(unsafe.Add(fPtr, offsetN)) |= T1SigS
						}
						if yy < height-1 {
							*(*T1Flags)(unsafe.Add(fPtr, offsetS)) |= T1SigN
						}
						if x > 0 {
							*(*T1Flags)(unsafe.Add(fPtr, -1)) |= T1SigE
						}
						if x < width-1 {
							*(*T1Flags)(unsafe.Add(fPtr, 1)) |= T1SigW
						}
					}
				}
			}
		}
	}

	// Flush MQ encoder
	tempC := mqC + mqA
	mqC |= 0xFFFF
	if mqC >= tempC {
		mqC -= 0x8000
	}

	mqC <<= mqCT
	mqBp, mqC, mqCT = mqByteOutLocal(mqBuf, mqBp, mqC)
	mqC <<= mqCT
	mqBp, _, _ = mqByteOutLocal(mqBuf, mqBp, mqC)

	endPos := mqBp + 1
	if endPos > 0 && mqBuf[endPos-1] == 0xFF {
		endPos--
	}

	if endPos > 1 {
		return mqBuf[1:endPos]
	}
	return nil
}
