package publish

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"syscall"
)

func readManagedReleaseMetadata(path string) ([]byte, error) {
	file, err := os.OpenFile(path, os.O_RDONLY|syscall.O_NOFOLLOW, 0)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Size() > 64<<10 {
		return nil, ErrBuildNotPublishable
	}
	data, err := io.ReadAll(io.LimitReader(file, 64<<10+1))
	if err != nil || len(data) > 64<<10 {
		return nil, ErrBuildNotPublishable
	}
	return data, nil
}
func sameFrozenReleaseIdentity(left, right []byte) bool {
	var a, b map[string]any
	if json.Unmarshal(left, &a) != nil || json.Unmarshal(right, &b) != nil {
		return false
	}
	// A build's first sealed product owns its creation metadata. Subsequent
	// activation retries reuse those bytes instead of resealing another product.
	delete(a, "createdAt")
	delete(a, "reasons")
	delete(b, "createdAt")
	delete(b, "reasons")
	x, err := json.Marshal(a)
	if err != nil {
		return false
	}
	y, err := json.Marshal(b)
	return err == nil && bytes.Equal(x, y)
}
