package server

func Run(server *Server) error {
	server.Start()
	return nil
}
