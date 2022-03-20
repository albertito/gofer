package nettrace

import "context"

type ctxKeyT string

const ctxKey ctxKeyT = "blitiri.com.ar/go/srv/trace"

func NewContext(ctx context.Context, tr Trace) context.Context {
	return context.WithValue(ctx, ctxKey, tr)
}

func FromContext(ctx context.Context) (Trace, bool) {
	tr, ok := ctx.Value(ctxKey).(Trace)
	return tr, ok
}

func FromContextOrNew(ctx context.Context, family, title string) (Trace, context.Context) {
	tr, ok := FromContext(ctx)
	if ok {
		return tr, ctx
	}

	tr = New(family, title)
	return tr, NewContext(ctx, tr)
}

func ChildFromContext(ctx context.Context, family, title string) Trace {
	parent, ok := FromContext(ctx)
	if ok {
		return parent.NewChild(family, title)
	}
	return New(family, title)
}
