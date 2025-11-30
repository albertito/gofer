package nettrace

// To allow users to log the trace data, we provide a simple logging interface.
// This is global and not lock-protected, so if used, it must be set before any
// tracing is done.
var Log func(family, title, id, msg string, err bool) = nil
