package nettrace

import "fmt"

// Disable is a flag to disable all tracing functions. It can be used to
// minimize the performance impact of the tracing functions when they are not
// needed (e.g. no debug server or logging).
var Disable = false

type disabledTrace struct{}

func (disabledTrace) NewChild(family, title string) Trace {
	return disabledTrace{}
}

func (disabledTrace) Link(other Trace, msg string) {}

func (disabledTrace) SetMaxEvents(n int) {}

func (disabledTrace) SetError() {}

func (disabledTrace) Print(s string) {}

func (disabledTrace) Printf(format string, a ...interface{}) {}

func (disabledTrace) Error(err error) error {
	return err
}

func (disabledTrace) Errorf(format string, a ...interface{}) error {
	return fmt.Errorf(format, a...)
}

func (disabledTrace) Finish() {}

// Static check that disabledTrace implements Trace.
var _ Trace = disabledTrace{}
