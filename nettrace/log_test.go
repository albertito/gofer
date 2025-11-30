package nettrace

import (
	"fmt"
	"regexp"
	"testing"
)

func TestLog(t *testing.T) {
	log := ""
	Log = func(f, t, id, msg string, isErr bool) {
		log += fmt.Sprintf("%s %s %s %s %v\n", f, t, id, msg, isErr)
	}
	defer func() { Log = nil }()

	var tr Trace = New("TLFamily", "TLTitle")
	tr.Printf("hola marola")
	tr.Finish()

	expectRe := regexp.MustCompile(
		`^TLFamily TLTitle [\w\d!]+ hola marola false\n$`)
	if !expectRe.MatchString(log) {
		t.Errorf("log does not match expected: %q", log)
	}
}
