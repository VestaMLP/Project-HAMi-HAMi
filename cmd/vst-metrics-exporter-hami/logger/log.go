package logger

import "log"

var Fatal = func(msg string, args ...any) {
	log.Fatalf(msg, args...)
}

var Error = func(msg string, args ...any) {
	log.Printf(msg, args...)
}

var Info = func(msg string, args ...any) {
	log.Printf(msg, args...)
}
