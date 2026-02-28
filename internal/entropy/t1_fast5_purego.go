//go:build purego

package entropy

// EncodeFast5 falls back to EncodeSafe when built with purego tag.
func (t *T1) EncodeFast5(bandType int) []byte {
	return t.EncodeSafe(bandType)
}
