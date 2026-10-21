package nativebuild

import (
	"bytes"
	"context"
	"crypto/sha256"
	"debug/macho"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
)

// metalKernelObject selects the canonical producer member from the private archive.
func metalKernelObject(ctx context.Context, archive string) (object []byte, failure error) {
	input, err := os.Open(archive)
	if err != nil {
		return nil, err
	}
	defer func() { failure = errors.Join(failure, input.Close()) }()
	info, err := input.Stat()
	if err != nil {
		return nil, err
	}
	var magic [8]byte
	if _, err := input.ReadAt(magic[:], 0); err != nil || string(magic[:]) != "!<arch>\n" {
		return nil, fmt.Errorf("Metal runtime is not a native archive")
	}
	for cursor := int64(8); cursor < info.Size(); {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		var header [60]byte
		if _, err := input.ReadAt(header[:], cursor); err != nil || string(header[58:]) != "`\n" {
			return nil, fmt.Errorf("Metal runtime archive has an invalid member header")
		}
		size, err := strconv.ParseInt(strings.TrimSpace(string(header[48:58])), 10, 64)
		if err != nil || size < 0 || size > info.Size()-cursor-60 {
			return nil, fmt.Errorf("Metal runtime archive has an invalid member length")
		}
		name := strings.TrimSpace(string(header[:16]))
		start, length := cursor+60, size
		if strings.HasPrefix(name, "#1/") {
			n, err := strconv.ParseInt(strings.TrimPrefix(name, "#1/"), 10, 64)
			if err != nil || n < 0 || n > 4096 || n > size {
				return nil, fmt.Errorf("Metal runtime archive has an invalid member name")
			}
			encoded := make([]byte, n)
			if _, err := input.ReadAt(encoded, start); err != nil {
				return nil, err
			}
			name = strings.TrimRight(string(encoded), "\x00")
			start += n
			length -= n
		} else {
			name = strings.TrimSuffix(name, "/")
		}
		if name == "kernel.o" {
			if object != nil {
				return nil, fmt.Errorf("Metal runtime contains duplicate kernel members")
			}
			if length == 0 || length > MaxArtifactBytes {
				return nil, fmt.Errorf("Metal kernel object exceeds the artifact limit")
			}
			object = make([]byte, length)
			if _, err := io.ReadFull(llvmSnapshotReader{ctx: ctx, reader: io.NewSectionReader(input, start, length)}, object); err != nil {
				return nil, err
			}
		}
		if size&1 != 0 {
			var padding [1]byte
			if _, err := input.ReadAt(padding[:], cursor+60+size); err != nil || padding[0] != '\n' {
				return nil, fmt.Errorf("Metal runtime archive has invalid member alignment")
			}
		}
		cursor += 60 + size + (size & 1)
	}
	if object == nil {
		return nil, fmt.Errorf("Metal runtime has no embedded kernel member")
	}
	return object, nil
}

func validateMetalKernel(ctx context.Context, archive, expected string) error {
	object, err := metalKernelObject(ctx, archive)
	if err != nil {
		return err
	}
	file, err := macho.NewFile(bytes.NewReader(object))
	if err != nil {
		return fmt.Errorf("Metal kernel Mach-O: %w", err)
	}
	if file.Cpu != macho.CpuArm64 || file.Type != macho.TypeObj || file.Symtab == nil {
		return fmt.Errorf("Metal kernel member is not an arm64 object")
	}
	symbols := map[string]macho.Symbol{}
	for _, symbol := range file.Symtab.Syms {
		if _, exists := symbols[symbol.Name]; exists {
			return fmt.Errorf("Metal kernel member has duplicate symbols")
		}
		symbols[symbol.Name] = symbol
	}
	read := func(name string, count uint64) ([]byte, error) {
		symbol, exists := symbols[name]
		if !exists || symbol.Type&0x0f != 0x0f || symbol.Desc&0xc0 != 0 || symbol.Sect == 0 || int(symbol.Sect) > len(file.Sections) {
			return nil, fmt.Errorf("Metal kernel member has no defined %s", name)
		}
		section := file.Sections[int(symbol.Sect)-1]
		if symbol.Value < section.Addr || symbol.Value-section.Addr > section.Size || count > section.Size-(symbol.Value-section.Addr) {
			return nil, fmt.Errorf("Metal kernel symbol %s exceeds its section", name)
		}
		offset := uint64(section.Offset) + (symbol.Value - section.Addr)
		if offset > uint64(len(object)) || count > uint64(len(object))-offset {
			return nil, fmt.Errorf("Metal kernel symbol %s exceeds its object", name)
		}
		return object[offset : offset+count], nil
	}
	lengthBytes, err := read("_lo_native_metal_kernel_length", 8)
	if err != nil {
		return err
	}
	length := file.ByteOrder.Uint64(lengthBytes)
	if length == 0 || length > MaxArtifactBytes {
		return fmt.Errorf("Metal embedded kernel length exceeds the artifact limit")
	}
	kernel, err := read("_lo_native_metal_kernel_bytes", length)
	if err != nil {
		return err
	}
	digest, err := read("_lo_native_metal_kernel_digest", 65)
	if err != nil {
		return err
	}
	sum := sha256.Sum256(kernel)
	actual := hex.EncodeToString(sum[:])
	if actual != expected || string(digest[:64]) != actual || digest[64] != 0 {
		return fmt.Errorf("Metal archive embedded kernel differs from the selected identity")
	}
	for _, name := range []string{"_lo_native_metal_library", "_lo_native_metal_kernel_sha256"} {
		if _, err := read(name, 1); err != nil {
			return err
		}
	}
	return ctx.Err()
}
