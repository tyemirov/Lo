package integration_test

import (
	"reflect"
	"testing"

	"github.com/MarcoPoloResearchLab/Lo/internal/catalog"
)

func TestAdapterProcessActivationContract(t *testing.T) {
	operation, ok := catalog.Lookup(catalog.ProcessCommandNew)
	if !ok || operation.Capability() != catalog.ProcessRun || !reflect.DeepEqual(operation.ActivationKinds(), []string{"Process.ExecutionFailed", "Process.Timeout"}) {
		t.Fatal("Process command does not declare its recoverable activation errors")
	}
}
