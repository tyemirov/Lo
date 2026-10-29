package value

import (
	"encoding/binary"
	"fmt"
	"math"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"github.com/MarcoPoloResearchLab/Lo/internal/types"
)

const (
	MaxInvocationArguments      = 4096
	MaxInvocationArgumentBytes  = 65536
	MaxInvocationTransportBytes = 1048576
)

// HostInvocation contains one immutable capture of public invocation inputs.
type HostInvocation interface {
	Value
	FieldsRecord() Record
	ArgumentCount() int
	Transport() []byte
	hostInvocation()
}

type hostInvocation struct {
	base
	fields        Record
	argumentCount int
	transport     []byte
}

func (hostInvocation) hostInvocation()        {}
func (v hostInvocation) FieldsRecord() Record { return v.fields }
func (v hostInvocation) ArgumentCount() int   { return v.argumentCount }
func (v hostInvocation) Transport() []byte    { return append([]byte(nil), v.transport...) }
func (v hostInvocation) MarshalJSON() ([]byte, error) {
	return encode(v, struct {
		ArgumentCount int   `json:"argument_count"`
		RealUID       Value `json:"real_uid"`
		EffectiveUID  Value `json:"effective_uid"`
		NowUnixNS     Value `json:"now_unix_ns"`
		Redacted      bool  `json:"redacted"`
	}{v.argumentCount, v.fields.Field("real_uid"), v.fields.Field("effective_uid"), v.fields.Field("now_unix_ns"), true})
}

// ValidateInvocationArguments enforces CLI argument admission without reading the host.
func ValidateInvocationArguments(arguments []string) error {
	if len(arguments) > MaxInvocationArguments {
		return fmt.Errorf("program arguments exceed the count limit")
	}
	weight := 4
	for _, argument := range arguments {
		if !utf8.ValidString(argument) || strings.ContainsRune(argument, 0) {
			return fmt.Errorf("program argument requires valid UTF-8 without NUL")
		}
		if len(argument)+1 > MaxInvocationArgumentBytes {
			return fmt.Errorf("program argument exceeds the byte limit")
		}
		if len(argument)+5 > MaxInvocationTransportBytes-weight {
			return fmt.Errorf("program argument vector exceeds the byte limit")
		}
		weight += len(argument) + 5
	}
	return nil
}

// NewHostInvocation validates captured inputs and constructs their canonical transport.
func NewHostInvocation(arguments []string, home Text, realUID, effectiveUID, nowUnixNS Integer) (HostInvocation, error) {
	if err := ValidateInvocationArguments(arguments); err != nil {
		return nil, err
	}
	if home == nil || !types.Equal(home.Type(), types.Path()) || !filepath.IsAbs(home.Text()) || !utf8.ValidString(home.Text()) || strings.ContainsRune(home.Text(), 0) {
		return nil, fmt.Errorf("capture host home: require an absolute UTF-8 path without NUL")
	}
	for _, uid := range []Integer{realUID, effectiveUID} {
		if uid == nil || !uid.Big().IsUint64() || uid.Big().Uint64() > math.MaxUint32 {
			return nil, fmt.Errorf("capture host UID: outside the unsigned 32-bit range")
		}
	}
	if nowUnixNS == nil || !nowUnixNS.Big().IsInt64() {
		return nil, fmt.Errorf("capture host clock: outside the signed 64-bit range")
	}
	transportSize := 4 + 4 + len(home.Text()) + 1 + 4 + 4 + 8
	for _, argument := range arguments {
		transportSize += 4 + len(argument) + 1
	}
	if transportSize > MaxInvocationTransportBytes {
		return nil, fmt.Errorf("capture host snapshot: exceeds the transport limit")
	}
	transport := make([]byte, 0, transportSize)
	append32 := func(number uint32) { transport = binary.BigEndian.AppendUint32(transport, number) }
	appendText := func(text string) {
		append32(uint32(len(text) + 1))
		transport = append(transport, text...)
		transport = append(transport, 0)
	}
	append32(uint32(len(arguments)))
	items := make([]Value, len(arguments))
	for index, argument := range arguments {
		items[index] = must(NewText(types.String(), argument))
		appendText(argument)
	}
	appendText(home.Text())
	append32(uint32(realUID.Big().Uint64()))
	append32(uint32(effectiveUID.Big().Uint64()))
	transport = binary.BigEndian.AppendUint64(transport, uint64(nowUnixNS.Big().Int64()))
	fields := must(NewRecord([]Field{
		{"arguments", must(NewList(must(types.NewList(types.String())), items))},
		{"home", home}, {"real_uid", realUID}, {"effective_uid", effectiveUID}, {"now_unix_ns", nowUnixNS},
	}))
	return hostInvocation{newBase(types.HostInvocation(), addWeight(fields.Weight(), uint64(len(transport)))), fields, len(arguments), transport}, nil
}

// DecodeHostInvocation validates the exact canonical snapshot body at the host boundary.
func DecodeHostInvocation(data []byte) (HostInvocation, error) {
	if len(data) > MaxInvocationTransportBytes {
		return nil, fmt.Errorf("decode host snapshot: transport limit")
	}
	offset := 0
	read32 := func() (uint32, error) {
		if len(data)-offset < 4 {
			return 0, fmt.Errorf("decode host snapshot: truncated integer")
		}
		result := binary.BigEndian.Uint32(data[offset:])
		offset += 4
		return result, nil
	}
	readText := func() (string, error) {
		length, err := read32()
		if err != nil {
			return "", err
		}
		if length == 0 || uint64(length) > uint64(len(data)-offset) {
			return "", fmt.Errorf("decode host snapshot: invalid text length")
		}
		payload := data[offset : offset+int(length)]
		offset += int(length)
		if payload[len(payload)-1] != 0 || strings.ContainsRune(string(payload[:len(payload)-1]), 0) || !utf8.Valid(payload[:len(payload)-1]) {
			return "", fmt.Errorf("decode host snapshot: invalid text")
		}
		return string(payload[:len(payload)-1]), nil
	}
	count, err := read32()
	if err != nil {
		return nil, err
	}
	if count > MaxInvocationArguments {
		return nil, fmt.Errorf("decode host snapshot: argument count limit")
	}
	arguments := make([]string, int(count))
	for index := range arguments {
		arguments[index], err = readText()
		if err != nil {
			return nil, err
		}
	}
	home, err := readText()
	if err != nil {
		return nil, err
	}
	realUID, err := read32()
	if err != nil {
		return nil, err
	}
	effectiveUID, err := read32()
	if err != nil {
		return nil, err
	}
	if len(data)-offset != 8 {
		return nil, fmt.Errorf("decode host snapshot: clock or trailing bytes")
	}
	now := int64(binary.BigEndian.Uint64(data[offset:]))
	path, err := NewText(types.Path(), home)
	if err != nil {
		return nil, fmt.Errorf("decode host snapshot: invalid home")
	}
	return NewHostInvocation(arguments, path, Int64(int64(realUID)), Int64(int64(effectiveUID)), Int64(now))
}

func redactedHostInvocation(v HostInvocation) HostInvocation {
	fields := must(NewRecord([]Field{
		{"arguments", must(NewList(must(types.NewList(types.String())), nil))},
		{"home", must(NewText(types.Path(), "/"))},
		{"real_uid", v.FieldsRecord().Field("real_uid")}, {"effective_uid", v.FieldsRecord().Field("effective_uid")}, {"now_unix_ns", v.FieldsRecord().Field("now_unix_ns")},
	}))
	return hostInvocation{newBase(types.HostInvocation(), fields.Weight()), fields, v.ArgumentCount(), nil}
}

func hostInvocationSnapshotWeight(v HostInvocation) uint64 {
	weight := uint64(16)
	for _, field := range v.FieldsRecord().Fields() {
		weight = addWeight(weight, uint64(len(field.Name)))
		switch field.Name {
		case "arguments":
			weight = addWeight(weight, 16)
		case "home":
			weight = addWeight(weight, 17)
		default:
			weight = addWeight(weight, field.Value.Weight())
		}
	}
	return addWeight(16, weight)
}
