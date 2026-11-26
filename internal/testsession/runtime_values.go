package testsession

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"time"
)

var valueSourceFilename = regexp.MustCompile(`(?m)^source_filename = "[^"\n]*"$`)
var valueDebugFile = regexp.MustCompile(`(?m)^(![0-9]+ = !DIFile\()filename: "[^"\n]*", directory: "[^"\n]*"(\))$`)
var valueTargetTriple = regexp.MustCompile(`(?m)^target triple = "[^"\n]*"$`)

// Runtime-value fixtures retain real JSON source identities and lexical spans.
// Their machine-code debug filename is a synthetic fixture label; production
// compilation and source/debug identity fixtures use SourceIdentity instead.
func splitRuntimeValueModule(module []byte) ([]byte, []byte, error) {
	metadata := metadataDeclaration.FindAll(module, -1)
	sizes := metadataLength.FindAll(module, -1)
	triples := valueTargetTriple.FindAll(module, -1)
	if len(metadata) != 1 || len(sizes) != 1 || len(triples) != 1 || len(valueSourceFilename.FindAll(module, -1)) != 1 {
		return nil, nil, errors.New("runtime value module requires unique metadata, size, source and target declarations")
	}
	private := []byte(string(triples[0]) + "\n" + string(metadata[0]) + "\n" + string(sizes[0]) + "\n")
	code := metadataDeclaration.ReplaceAll(module, nil)
	code = metadataLength.ReplaceAll(code, nil)
	code = valueSourceFilename.ReplaceAllLiteral(code, []byte(`source_filename = "runtime-value-fixture.lo"`))
	code = valueDebugFile.ReplaceAll(code, []byte(`${1}filename: "runtime-value-fixture.lo", directory: ""${2}`))
	return code, private, nil
}

func (server *Server) runtimeValueCode(ctx context.Context, selected CompileRequest, target targetInputs, compiler string, code []byte, private string) error {
	encoded, err := json.Marshal([]any{server.identity, selected.Target, selected.Backend, compileFlags(target, selected.Target), compiler, code})
	if err != nil {
		return err
	}
	key := fmt.Sprintf("%x", sha256.Sum256(encoded))
	server.mu.Lock()
	record := server.valueCodes[key]
	if record == nil {
		buildCtx, cancel := context.WithCancel(server.ctx)
		record = &buildRecord{done: make(chan struct{}), cancel: cancel}
		server.valueCodes[key] = record
		server.stats.CodeBuilds++
		server.workers.Go(func() { server.buildRuntimeValueCode(buildCtx, key, selected, target, compiler, code, record) })
	} else {
		server.stats.CodeReused++
	}
	record.waiters++
	server.clock++
	record.lastUsed = server.clock
	server.mu.Unlock()
	release := func() bool {
		server.mu.Lock()
		defer server.mu.Unlock()
		record.waiters--
		if record.waiters == 0 {
			select {
			case <-record.done:
			default:
				record.cancel()
				if server.valueCodes[key] == record {
					delete(server.valueCodes, key)
				}
				return true
			}
		}
		return false
	}
	select {
	case <-ctx.Done():
		if release() {
			<-record.done
		}
		return fmt.Errorf("runtime value code requester canceled: %w", ctx.Err())
	case <-record.done:
		defer release()
		server.mu.Lock()
		defer server.mu.Unlock()
		if record.err != nil {
			return record.err
		}
		if err := os.Link(record.result.Artifact, private); err != nil {
			return fmt.Errorf("retain runtime value code: %w", err)
		}
		return nil
	}
}

func (server *Server) buildRuntimeValueCode(ctx context.Context, key string, selected CompileRequest, target targetInputs, compiler string, code []byte, record *buildRecord) {
	defer record.cancel()
	_, _, err := server.compileRuntimeValueCode(ctx, key, selected, target, compiler, code, record)
	server.mu.Lock()
	defer server.mu.Unlock()
	select {
	case <-record.done:
	default:
		record.err = err
		if err != nil && server.valueCodes[key] == record {
			delete(server.valueCodes, key)
		}
		close(record.done)
	}
}

func (server *Server) compileRuntimeValueCode(ctx context.Context, key string, selected CompileRequest, target targetInputs, compiler string, code []byte, record *buildRecord) (_ string, _ int64, failure error) {
	ctx, cancel := context.WithTimeout(ctx, 180*time.Second)
	defer cancel()
	root := filepath.Join(server.directory, "value-code")
	if err := os.MkdirAll(root, os.ModePerm); err != nil {
		return "", 0, err
	}
	work, err := os.MkdirTemp(root, "compile-")
	if err != nil {
		return "", 0, err
	}
	defer func() {
		if work != "" {
			failure = errors.Join(failure, os.RemoveAll(work))
		}
	}()
	source, object := filepath.Join(work, "code.ll"), filepath.Join(work, "code.o")
	if err := writeBytes(source, code); err != nil {
		return "", 0, err
	}
	flags := append(compileFlags(target, selected.Target), "-c", source, "-o", object)
	if err := server.backendCommand(ctx, "compile reusable runtime value code", work, compiler, flags...); err != nil {
		return "", 0, err
	}
	info, err := os.Stat(object)
	if err != nil {
		return "", 0, err
	}
	staging, err := os.CreateTemp(root, ".staged-")
	if err != nil {
		return "", 0, err
	}
	staged := staging.Name()
	defer func() {
		if staged != "" {
			failure = errors.Join(failure, os.Remove(staged))
		}
	}()
	if err := staging.Close(); err != nil {
		return "", 0, err
	}
	if err := os.Rename(object, staged); err != nil {
		return "", 0, err
	}
	if err := os.RemoveAll(work); err != nil {
		return "", 0, err
	}
	work = ""
	server.mu.Lock()
	defer server.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return "", 0, err
	}
	if server.valueCodes[key] != record || record.waiters == 0 {
		return "", 0, errors.New("runtime value code build was retired")
	}
	if err := server.makeSpaceLocked(info.Size()); err != nil {
		return "", 0, err
	}
	output := filepath.Join(root, key+".o")
	if err := os.Rename(staged, output); err != nil {
		return "", 0, err
	}
	staged = ""
	server.stats.Bytes += info.Size()
	record.result = CompileResult{Artifact: output}
	record.bytes = info.Size()
	close(record.done)
	return output, info.Size(), nil
}
