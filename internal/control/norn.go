package control

import "net/http"

func (s *Server) nornTools(w http.ResponseWriter, r *http.Request) {
	token, err := s.sessions.Access(r.Context())
	if err != nil {
		s.refuse(w, r, err)

		return
	}

	s.toolkits.NornHandler(token).ServeHTTP(w, r)
}
