package statemachine

import "context"

type Context struct {
	ctx  context.Context
	send func(evt any) error
}

func (ctx *Context) Context() context.Context {
	return ctx.ctx
}

func (ctx *Context) Send(evt any) error {
	return ctx.send(evt)
}
