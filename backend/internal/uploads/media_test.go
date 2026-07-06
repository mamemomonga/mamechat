package uploads

import "testing"

func TestSniffFormat(t *testing.T) {
	pngSig := []byte{0x89, 'P', 'N', 'G', 0x0D, 0x0A, 0x1A, 0x0A}
	apng := append(append([]byte{}, pngSig...), []byte("....IHDR........acTL....IDAT")...)
	plainPNG := append(append([]byte{}, pngSig...), []byte("....IHDR........IDAT....")...)
	// acTL が IDAT より後に現れる壊れた/非アニメPNGはAPNGとみなさない。
	actlAfterIdat := append(append([]byte{}, pngSig...), []byte("....IHDR....IDAT....acTL")...)

	cases := []struct {
		name string
		data []byte
		want mediaFormat
	}{
		{"jpeg", []byte{0xFF, 0xD8, 0xFF, 0xE0, 0, 0, 0, 0, 0, 0, 0, 0}, formatJPEG},
		{"gif89a", []byte("GIF89a000000"), formatGIF},
		{"gif87a", []byte("GIF87a000000"), formatGIF},
		{"apng", apng, formatAPNG},
		{"plain png", plainPNG, formatUnknown},
		{"actl after idat", actlAfterIdat, formatUnknown},
		{"mp4", append([]byte{0, 0, 0, 0x18}, []byte("ftypisom....")...), formatMP4},
		{"too short", []byte{0xFF, 0xD8}, formatUnknown},
		{"unknown", []byte("not a media file at all"), formatUnknown},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := sniffFormat(tc.data); got != tc.want {
				t.Errorf("sniffFormat(%s) = %d, want %d", tc.name, got, tc.want)
			}
		})
	}
}
