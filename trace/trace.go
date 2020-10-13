// Package trace extends golang.org/x/net/trace.
package trace

import (
	"context"
	"fmt"
	"strings"

	"blitiri.com.ar/go/log"

	nettrace "golang.org/x/net/trace"
)

type key string

const contextKey key = "blitiri.com.ar/go/gofer/trace.Trace"

// A Trace represents an active request.
type Trace struct {
	family string
	title  string
	t      nettrace.Trace
}

// New trace.
func New(family, title string) *Trace {
	return &Trace{family, title, nettrace.New(family, title)}
}

func NewContext(ctx context.Context, tr *Trace) context.Context {
	return context.WithValue(ctx, contextKey, tr)
}

func FromContext(ctx context.Context) (tr *Trace, ok bool) {
	tr, ok = ctx.Value(contextKey).(*Trace)
	return
}

// Printf adds this message to the trace's log.
func (t *Trace) Printf(format string, a ...interface{}) {
	t.t.LazyPrintf(format, a...)

	log.Log(log.Debug, 1, "%#p %s %s: %s", t, t.family, t.title,
		fmt.Sprintf(format, a...))
}

// Debugf adds this message to the trace's log, with a debugging level.
func (t *Trace) Debugf(format string, a ...interface{}) {
	t.t.LazyPrintf(format, a...)

	log.Log(log.Debug, 1, "%#p %s %s: %s",
		t, t.family, t.title, fmt.Sprintf(format, a...))
}

// Errorf adds this message to the trace's log, with an error level.
func (t *Trace) Errorf(format string, a ...interface{}) error {
	// Note we can't just call t.Error here, as it breaks caller logging.
	err := fmt.Errorf(format, a...)
	t.t.SetError()
	t.t.LazyPrintf("error: %v", err)

	log.Log(log.Info, 1, "%#p %s %s error: %s", t, t.family, t.title,
		err.Error())
	return err
}

// SetError marks the trace as having received an error, without emitting any
// particular output.
func (t *Trace) SetError() {
	t.t.SetError()
}

// Finish the trace. It should not be changed after this is called.
func (t *Trace) Finish() {
	t.t.Finish()
}

// Write so Trace implements io.Writer, which means it can be used as output
// for log.Logger.
func (t *Trace) Write(p []byte) (n int, err error) {
	lines := strings.Split(string(p), "\n")
	for _, line := range lines {
		if strings.TrimSpace(line) == "" {
			continue
		}
		t.Printf("%s", line)
	}
	return len(p), nil
}

// EventLog is used for tracing long-lived objects.
type EventLog struct {
	family string
	title  string
	e      nettrace.EventLog
}

// NewEventLog returns a new EventLog.
func NewEventLog(family, title string) *EventLog {
	return &EventLog{family, title, nettrace.NewEventLog(family, title)}
}

// Printf adds the message to the EventLog.
func (e *EventLog) Printf(format string, a ...interface{}) {
	e.e.Printf(format, a...)

	log.Log(log.Debug, 1, "%#p %s %s: %s", e, e.family, e.title,
		fmt.Sprintf(format, a...))
}

// Errorf adds the message to the EventLog, with an error level.
func (e *EventLog) Errorf(format string, a ...interface{}) error {
	err := fmt.Errorf(format, a...)
	e.e.Errorf("error: %v", err)

	log.Log(log.Info, 1, "%#p %s %s: error: %s",
		e, e.family, e.title, err.Error())

	return err
}

// Write so EventLog implements io.Writer, which means it can be used as
// output for log.Logger.
func (e *EventLog) Write(p []byte) (n int, err error) {
	lines := strings.Split(string(p), "\n")
	for _, line := range lines {
		if strings.TrimSpace(line) == "" {
			continue
		}
		e.Printf("%s", line)
	}
	return len(p), nil
}
