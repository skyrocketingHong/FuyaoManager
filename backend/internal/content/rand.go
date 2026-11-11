package content

import "crypto/rand"

func cryptoRead(buf []byte) (int, error) {
	return rand.Read(buf)
}
