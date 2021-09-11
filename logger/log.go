package logger

import (
	"fmt"
	"log"
	"os"
)

type Logger struct {
	logger     *log.Logger
	debug      bool
	trace      bool
	infoLabel  string
	errorLabel string
	fatalLabel string
	debugLabel string
	traceLabel string
}

func NewStdLogger(time, debug, trace, colors, pid bool) *Logger {
	flags := 0
	if time {
		flags = log.LstdFlags | log.Lmicroseconds
	}

	pre := ""
	if pid {
		pre = pidPrefix()
	}

	l := &Logger{
		logger: log.New(os.Stderr, pre, flags),
		debug:  debug,
		trace:  trace,
	}

	if colors {
		setColoredLabelFormats(l)
	} else {
		setPlainLabelFormats(l)
	}

	return l
}

func NewFileLogger(filename string, time, debug, trace, pid bool) *Logger {
	fileflags := os.O_WRONLY | os.O_APPEND | os.O_CREATE
	f, err := os.OpenFile(filename, fileflags, 0660)
	if err != nil {
		log.Fatalf("error opening file:%s", err)
	}

	flags := 0
	if time {
		flags = log.LstdFlags | log.Lmicroseconds
	}

	pre := ""
	if pid {
		pre = pidPrefix()
	}

	l := &Logger{
		logger: log.New(f, pre, flags),
		debug:  debug,
		trace:  trace,
	}

	setPlainLabelFormats(l)

	return l
}

func pidPrefix() string {
	return fmt.Sprintf("[%d] ", os.Getpid())
}

func setColoredLabelFormats(l *Logger) {
	colorFormat := "[\x1b[%dm%s\x1b[0m] "
	l.infoLabel = fmt.Sprintf(colorFormat, 32, "INF")
	l.debugLabel = fmt.Sprintf(colorFormat, 36, "DBG")
	l.errorLabel = fmt.Sprintf(colorFormat, 31, "ERR")
	l.fatalLabel = fmt.Sprintf(colorFormat, 31, "FTL")
	l.traceLabel = fmt.Sprintf(colorFormat, 33, "TRC")
}

func setPlainLabelFormats(l *Logger) {
	l.infoLabel = "[INF] "
	l.debugLabel = "[DBG] "
	l.errorLabel = "[ERR] "
	l.fatalLabel = "[FTL] "
	l.traceLabel = "[TRC] "
}

// Noticef logs a notice statement
func (l *Logger) Noticef(format string, v ...interface{}) {
	l.logger.Printf(l.infoLabel+format, v...)
}

// Errorf logs an error statement
func (l *Logger) Errorf(format string, v ...interface{}) {
	l.logger.Printf(l.errorLabel+format, v...)
}

// Fatalf logs a fatal error
func (l *Logger) Fatalf(format string, v ...interface{}) {
	l.logger.Fatalf(l.fatalLabel+format, v...)
}

// Debugf logs a debug statement
func (l *Logger) Debugf(format string, v ...interface{}) {
	if l.debug {
		l.logger.Printf(l.debugLabel+format, v...)
	}
}

// Tracef logs a trace statement
func (l *Logger) Tracef(format string, v ...interface{}) {
	if l.trace {
		l.logger.Printf(l.traceLabel+format, v...)
	}
}
