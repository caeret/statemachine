package statemachine

type Logger interface {
	Debug(msg string, kvs ...any)
	Error(msg string, kvs ...any)
}
