package statemachine

import "context"

type Context struct {
	context.Context
	send func(evt any) error
}

func (ctx *Context) Send(evt any) error {
	return ctx.send(evt)
}
