package clioptions

import (
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
)

func unlimitedStep() error { return nil }

func TestFailureEvidenceCanonicalOrderAndIsolation(t *testing.T) {
	flags := []Flag{
		{Name: "--report", Aliases: []string{"--review"}},
		{Name: "--remove", Aliases: []string{"--removal"}},
	}
	_, err := Parse([]string{"--re"}, flags, "test", "", true, unlimitedStep)
	var failure *Failure
	if !errors.As(err, &failure) || failure.Kind() != InvalidArguments || failure.Reason() != "ambiguous-option" || failure.Index() != 0 {
		t.Fatalf("ambiguous evidence: %#v, %v", failure, err)
	}
	want := []string{"--report", "--remove"}
	if !reflect.DeepEqual(failure.Candidates(), want) {
		t.Fatalf("canonical candidates: %#v", failure.Candidates())
	}
	changed := failure.Candidates()
	changed[0] = "mutated"
	if !reflect.DeepEqual(failure.Candidates(), want) {
		t.Fatal("caller mutation changed failure evidence")
	}
	if strings.Contains(err.Error(), "--re") {
		t.Fatal("failure message disclosed the supplied token")
	}
}

func TestSelectionAndHelpDoNotRetainInputSlices(t *testing.T) {
	flags := []Flag{{Name: "--verbose", Aliases: []string{"-v"}, Help: "show details"}}
	arguments := []string{"-v", "--help"}
	got, err := Parse(arguments, flags, "test", "Example", false, unlimitedStep)
	if err != nil {
		t.Fatal(err)
	}
	flags[0].Name = "--changed"
	flags[0].Aliases[0] = "-x"
	arguments[0] = "--changed"
	if !got.HelpRequested || !reflect.DeepEqual(got.Flags, []Option{{Name: "--verbose", Selected: true}}) || !strings.Contains(got.HelpText, "  --verbose, -v  show details\n") {
		t.Fatalf("immutable result: %#v", got)
	}
}

func TestWorkFailurePropagatesWithoutParserConversion(t *testing.T) {
	flags := []Flag{{Name: "--preview", Aliases: []string{"--preview-all"}, Help: "show work"}}
	arguments := []string{"--pre"}
	steps := 0
	if _, err := Parse(arguments, flags, "test", "description", true, func() error { steps++; return nil }); err != nil {
		t.Fatal(err)
	}
	sentinel := errors.New("work interrupted")
	for _, stop := range []int{1, steps / 2, steps} {
		t.Run(fmt.Sprint(stop), func(t *testing.T) {
			calls := 0
			_, err := Parse(arguments, flags, "test", "description", true, func() error {
				calls++
				if calls == stop {
					return sentinel
				}
				return nil
			})
			if !errors.Is(err, sentinel) || calls != stop {
				t.Fatalf("work failure conversion or extra work: calls=%d err=%v", calls, err)
			}
		})
	}
}

func TestHelpSizeDoesNotMultiplyLongestSpelling(t *testing.T) {
	flags := []Flag{{Name: "--" + strings.Repeat("x", 8192), Help: "long name"}}
	inputBytes := len(flags[0].Name) + len(flags[0].Help)
	for index := range 200 {
		flag := Flag{Name: fmt.Sprintf("--flag%d", index), Help: "small"}
		inputBytes += len(flag.Name) + len(flag.Help)
		flags = append(flags, flag)
	}
	got, err := Parse(nil, flags, "test", "", false, unlimitedStep)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.HelpText) > inputBytes+6*len(flags)+200 {
		t.Fatalf("help grew beyond fixed row separators: input=%d output=%d", inputBytes, len(got.HelpText))
	}
}
