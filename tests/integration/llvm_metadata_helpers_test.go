package integration_test

import (
	"encoding/hex"
	"fmt"
)

func decodeLLVMBytes(encoded []byte) ([]byte, error) {
	data := make([]byte, 0, len(encoded))
	for offset := 0; offset < len(encoded); offset++ {
		if encoded[offset] != '\\' {
			data = append(data, encoded[offset])
			continue
		}
		if offset+2 >= len(encoded) {
			return nil, fmt.Errorf("incomplete LLVM byte escape")
		}
		decoded, err := hex.DecodeString(string(encoded[offset+1 : offset+3]))
		if err != nil {
			return nil, err
		}
		data = append(data, decoded[0])
		offset += 2
	}
	return data, nil
}
