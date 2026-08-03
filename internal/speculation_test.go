package internal

import "testing"

func TestSpeculationControlArgs(t *testing.T) {
	if got, want := speculationControlArgs(), (speculationControlArgsValues{option: 53, suboption: 0, disable: 4}); got != want {
		t.Fatalf("unexpected speculation control args: got %#v want %#v", got, want)
	}
}
