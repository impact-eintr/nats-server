package server

type Logger interface {
	// Log a notice err
	Noticef(format string, v ...interface{})

	// Log a fatal error
	Fatalf(format string, v ...interface{})

	// Log an error
	Errorf(format string, v ...interface{})

	// Log a debug statement
	Debugf(format string, v ...interface{})

	// Log a trace statement
	Tracef(format string, v ...interface{})
}

// Log a notice err
func (s *Server) Noticef(format string, v ...interface{})

// Log a fatal error
func (s *Server) Fatalf(format string, v ...interface{})

// Log an error
func (s *Server) Errorf(format string, v ...interface{})

// Log a debug statement
func (s *Server) Debugf(format string, v ...interface{})

// Log a trace statement
func (s *Server) Tracef(format string, v ...interface{})
