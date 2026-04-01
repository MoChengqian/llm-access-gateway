package zap

import (
	"fmt"
	"io"
	"log"
	"os"
	"strings"
	"time"
)

type Config struct {
	Level string
}

type Logger struct {
	logger *log.Logger
	level  string
}

type Field struct {
	Key   string
	Value any
}

func NewProductionConfig() Config {
	return Config{Level: "info"}
}

func (c Config) Build() (*Logger, error) {
	return &Logger{
		logger: log.New(os.Stderr, "", log.LstdFlags),
		level:  strings.ToLower(c.Level),
	}, nil
}

func NewNop() *Logger {
	return &Logger{
		logger: log.New(io.Discard, "", 0),
		level:  "info",
	}
}

func (l *Logger) Info(msg string, fields ...Field) {
	l.log("INFO", msg, fields...)
}

func (l *Logger) Error(msg string, fields ...Field) {
	l.log("ERROR", msg, fields...)
}

func (l *Logger) Fatal(msg string, fields ...Field) {
	l.log("FATAL", msg, fields...)
	os.Exit(1)
}

func (l *Logger) Sync() error {
	return nil
}

func (l *Logger) log(level string, msg string, fields ...Field) {
	if l == nil || l.logger == nil {
		return
	}

	var builder strings.Builder
	builder.WriteString(level)
	builder.WriteString(" ")
	builder.WriteString(msg)

	for _, field := range fields {
		builder.WriteString(" ")
		builder.WriteString(field.Key)
		builder.WriteString("=")
		builder.WriteString(fmt.Sprint(field.Value))
	}

	l.logger.Print(builder.String())
}

func String(key, value string) Field {
	return Field{Key: key, Value: value}
}

func Int(key string, value int) Field {
	return Field{Key: key, Value: value}
}

func Duration(key string, value time.Duration) Field {
	return Field{Key: key, Value: value}
}

func Error(err error) Field {
	return Field{Key: "error", Value: err}
}
