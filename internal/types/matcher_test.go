package types_test

import (
	"testing"

	"github.com/MarcoPoloResearchLab/Lo/internal/types"
)

func TestMatcherReadTypeInvariant(t *testing.T) {
	for _, kind := range []types.Kind{types.MatchKind, types.MatchCaseKind} {
		pure, err := types.NewMatcher(kind, types.Int(), types.String(), false)
		if err != nil {
			t.Fatal(err)
		}
		reading, err := types.NewMatcher(kind, types.Int(), types.String(), true)
		if err != nil {
			t.Fatal(err)
		}
		if pure.Reads() || !reading.Reads() || types.Equal(pure, reading) {
			t.Fatalf("effect qualifier lost for %s", kind)
		}
		if pure.String() != string(kind)+"<Int, String>" || reading.String() != string(kind)+"<Int, String, Read>" {
			t.Fatalf("unexpected matcher names %s, %s", pure, reading)
		}
		if !types.MatcherReadWidening(pure, reading) || types.MatcherReadWidening(reading, pure) {
			t.Fatalf("matcher effect widening differs for %s", kind)
		}
	}
}

func TestMatcherWideningKeepsSignatureExact(t *testing.T) {
	pure, err := types.NewMatcher(types.MatchKind, types.Int(), types.String(), false)
	if err != nil {
		t.Fatal(err)
	}
	reading, err := types.NewMatcher(types.MatchKind, types.Int(), types.String(), true)
	if err != nil {
		t.Fatal(err)
	}
	pureInput, err := types.NewMatcher(types.MatchKind, pure, types.String(), false)
	if err != nil {
		t.Fatal(err)
	}
	pureOutput, err := types.NewMatcher(types.MatchKind, types.Int(), pure, false)
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name string
		from types.Matcher
		kind types.Kind
		in   types.Type
		out  types.Type
	}{
		{"different kind", pure, types.MatchCaseKind, types.Int(), types.String()},
		{"different input", pure, types.MatchKind, types.Bool(), types.String()},
		{"different output", pure, types.MatchKind, types.Int(), types.Int()},
		{"nested input effect", pureInput, types.MatchKind, reading, types.String()},
		{"nested output effect", pureOutput, types.MatchKind, types.Int(), reading},
	} {
		t.Run(test.name, func(t *testing.T) {
			to, err := types.NewMatcher(test.kind, test.in, test.out, true)
			if err != nil {
				t.Fatal(err)
			}
			if types.MatcherReadWidening(test.from, to) {
				t.Fatalf("widening changed the matcher signature from %s to %s", test.from, to)
			}
		})
	}
}

func TestMatcherTypeRejectsIncompleteSignatures(t *testing.T) {
	for _, test := range []struct {
		kind types.Kind
		in   types.Type
		out  types.Type
	}{
		{types.IntKind, types.Int(), types.String()},
		{types.MatchKind, nil, types.String()},
		{types.MatchCaseKind, types.Int(), nil},
	} {
		if typ, err := types.NewMatcher(test.kind, test.in, test.out, false); err == nil || typ != nil {
			t.Fatalf("accepted incomplete matcher signature %+v", test)
		}
	}
}
