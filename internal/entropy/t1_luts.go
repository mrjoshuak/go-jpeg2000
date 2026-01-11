// Package entropy - t1_luts.go contains pre-computed lookup tables for T1 coding.
//
// These lookup tables provide O(1) context calculation instead of conditional branches.
// Generated from the JPEG2000 specification context rules.
package entropy

// lutZCCtx is the zero-coding context lookup table.
// Indexed by: bandType*256 + packed neighbor flags
// Packed neighbor flags bit layout:
//   bit 0: W significant    bit 4: NW significant
//   bit 1: E significant    bit 5: NE significant
//   bit 2: N significant    bit 6: SW significant
//   bit 3: S significant    bit 7: SE significant
//
// Returns context 0-8 for ZC coding.
// Band types: 0=LL, 1=HL, 2=LH, 3=HH
var lutZCCtx [4 * 256]uint8

// lutSCCtx is the sign-coding context lookup table.
// Indexed by: (hContrib+2)*5 + (vContrib+2)
// where hContrib and vContrib are in range [-2, 2].
// Returns: (ctx << 1) | prediction
var lutSCCtx [25]uint8

// Sign context lookup tables matching OpenJPEG.
// Index is built from 8 bits: W_sig, W_chi, E_sig, E_chi, N_sig, N_chi, S_sig, S_chi
// lutSignCtx gives context number (0-4 mapping to CtxSC0-CtxSC4)
// lutSignPred gives sign prediction bit
var lutSignCtx [256]uint8
var lutSignPred [256]uint8

func init() {
	// Generate ZC context LUT for all 4 band types
	// BandLL=0, BandHL=1, BandLH=2, BandHH=3
	for bandType := 0; bandType < 4; bandType++ {
		for packed := 0; packed < 256; packed++ {
			// Unpack neighbor flags
			w := (packed >> 0) & 1
			e := (packed >> 1) & 1
			n := (packed >> 2) & 1
			s := (packed >> 3) & 1
			nw := (packed >> 4) & 1
			ne := (packed >> 5) & 1
			sw := (packed >> 6) & 1
			se := (packed >> 7) & 1

			h := w + e              // horizontal count
			v := n + s              // vertical count
			d := nw + ne + sw + se  // diagonal count

			var ctx int
			switch bandType {
			case BandHL: // HL band - swap h and v
				h, v = v, h
				fallthrough
			case BandLL, BandLH: // LL/LH bands use same rules
				if h == 2 {
					ctx = 8
				} else if h == 1 {
					if v >= 1 {
						ctx = 7
					} else if d >= 1 {
						ctx = 6
					} else {
						ctx = 5
					}
				} else if v == 2 {
					ctx = 4
				} else if v == 1 {
					if d >= 1 {
						ctx = 3
					} else {
						ctx = 2
					}
				} else if d >= 2 {
					ctx = 1
				} else {
					ctx = 0
				}
			case BandHH: // HH band
				hv := h + v
				if hv >= 3 {
					ctx = 8
				} else if hv == 2 {
					if d >= 2 {
						ctx = 7
					} else if d >= 1 {
						ctx = 6
					} else {
						ctx = 5
					}
				} else if hv == 1 {
					if d >= 2 {
						ctx = 4
					} else {
						ctx = 3
					}
				} else {
					if d >= 2 {
						ctx = 2
					} else if d >= 1 {
						ctx = 1
					} else {
						ctx = 0
					}
				}
			}
			lutZCCtx[bandType*256+packed] = uint8(ctx)
		}
	}

	// Generate SC context LUT
	// hContrib and vContrib range from -2 to 2
	for hc := -2; hc <= 2; hc++ {
		for vc := -2; vc <= 2; vc++ {
			idx := (hc+2)*5 + (vc + 2)

			h := hc
			v := vc
			pred := 0
			if h < 0 {
				pred = 1
				h = -h
			}
			if h == 0 && v < 0 {
				pred = 1
				v = -v
			}

			var ctx int
			if h == 1 {
				if v == 1 {
					ctx = CtxSC4
				} else if v == 0 {
					ctx = CtxSC2
				} else {
					ctx = CtxSC1
				}
			} else if h == 0 {
				if v == 1 {
					ctx = CtxSC1
				} else if v == 0 {
					ctx = CtxSC0
				}
			} else if h == 2 {
				ctx = CtxSC3
			}

			lutSCCtx[idx] = uint8((ctx << 1) | pred)
		}
	}

	// Generate Sign context LUT from packed neighbor flags
	// Index bits: 0=W_sig, 1=W_chi, 2=E_sig, 3=E_chi, 4=N_sig, 5=N_chi, 6=S_sig, 7=S_chi
	for i := 0; i < 256; i++ {
		wSig := (i >> 0) & 1
		wChi := (i >> 1) & 1
		eSig := (i >> 2) & 1
		eChi := (i >> 3) & 1
		nSig := (i >> 4) & 1
		nChi := (i >> 5) & 1
		sSig := (i >> 6) & 1
		sChi := (i >> 7) & 1

		// Compute hc (horizontal contribution)
		hc := 0
		if wSig != 0 {
			if wChi != 0 {
				hc--
			} else {
				hc++
			}
		}
		if eSig != 0 {
			if eChi != 0 {
				hc--
			} else {
				hc++
			}
		}

		// Compute vc (vertical contribution)
		vc := 0
		if nSig != 0 {
			if nChi != 0 {
				vc--
			} else {
				vc++
			}
		}
		if sSig != 0 {
			if sChi != 0 {
				vc--
			} else {
				vc++
			}
		}

		// Compute prediction
		pred := 0
		if hc < 0 {
			pred = 1
			hc = -hc
		}
		if hc == 0 && vc < 0 {
			pred = 1
			vc = -vc
		}

		// Compute context (0-4)
		ctx := uint8(0) // CtxSC0 relative
		if hc == 1 {
			if vc == 1 {
				ctx = 4 // CtxSC4
			} else if vc == 0 {
				ctx = 2 // CtxSC2
			} else {
				ctx = 1 // CtxSC1
			}
		} else if hc == 0 {
			if vc == 1 {
				ctx = 1 // CtxSC1
			}
		} else if hc == 2 {
			ctx = 3 // CtxSC3
		}

		lutSignCtx[i] = ctx
		lutSignPred[i] = uint8(pred)
	}
}

// getZCContextFast returns ZC context using packed flags and LUT.
// This is an inline-friendly version for hot paths.
func getZCContextFast(packed uint8, bandType int) int {
	return int(lutZCCtx[bandType*256+int(packed)])
}

// getSCContextFast returns SC context and prediction from contribution values.
func getSCContextFast(hContrib, vContrib int) (ctx int, pred int) {
	// Clamp to valid range
	if hContrib < -2 {
		hContrib = -2
	} else if hContrib > 2 {
		hContrib = 2
	}
	if vContrib < -2 {
		vContrib = -2
	} else if vContrib > 2 {
		vContrib = 2
	}

	idx := (hContrib+2)*5 + (vContrib + 2)
	v := lutSCCtx[idx]
	return int(v >> 1), int(v & 1)
}
