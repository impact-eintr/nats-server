package server

import (
	"strings"

	"golang.org/x/crypto/bcrypt"
)

type User struct {
	Username    string       `json:"user"`
	Password    string       `json:"password"`
	Permissions *Permissions `json:"permissions"`
}

func (u *User) clone() *User {
	if u == nil {
		return nil
	}
	clone := &User{}
	*clone = *u
	clone.Permissions = u.Permissions.clone()
	return clone
}

type Permissions struct {
	Publish   []string `json:"publish"`
	Subscribe []string `json:"subscribe"`
}

func (p *Permissions) clone() *Permissions {
	if p == nil {
		return nil
	}
	clone := &Permissions{}
	if p.Publish != nil {
		clone.Publish = make([]string, len(p.Publish))
		copy(clone.Publish, p.Publish)
	}

	if p.Subscribe != nil {
		clone.Subscribe = make([]string, len(p.Subscribe))
		copy(clone.Subscribe, p.Subscribe)
	}
	return clone
}

func (s *Server) configureAuthorization() {
	if s.opts == nil {
		return
	}

	// Snapshot server options
	opts := s.getOpts()

	// Check for mutiple users first
	// This just checks and sets up the user map if we have multiple users(多租户).
	if opts.Users != nil {
		s.users = make(map[string]*User)
		for _, u := range opts.Users {
			s.users[u.Username] = u
		}
		s.info.AuthRequired = true
	} else if opts.Username != "" || opts.Authorization != "" {
		s.info.AuthRequired = true
	} else {
		s.users = nil
		s.info.AuthRequired = false
	}
}

// checkAuthorization will check authorization based on client type and
// return boolean indicating if client is authorized.
func (s *Server) checkAuthorization(c *client) bool {
	switch c.typ {
	case CLIENT:
		return s.isClientAuthorized(c)
	case ROUTER:
		return s.isRouterAuthorized(c)
	default:
		return false
	}
}

func (s *Server) isClientAuthorized(c *client) bool {
	opts := s.getOpts()

	if s.users != nil {
		user, ok := s.users[c.opts.Username]
		if !ok {
			return false
		}
		ok = comparePasswords(user.Password, c.opts.Password)
		// If we are authorized, register the user which will properly
		// setup any permissions for pub/sub authorizatio
		if ok {
			c.RegisterUser(user)
		}
		return ok
	} else if opts.Authorization != "" {
		return comparePasswords(opts.Authorization, c.opts.Authorization)
	} else if opts.Username != "" {
		if opts.Username != c.opts.Username {
			return false
		}
		return comparePasswords(opts.Password, c.opts.Password)
	}
	return true

}

func (s *Server) isRouterAuthorized(c *client) bool {
	opts := s.getOpts()

	if opts.Cluster.Username == "" {
		return true
	}

	if opts.Cluster.Username != c.opts.Username {
		return false
	}
	return comparePasswords(opts.Cluster.Password, c.opts.Password)
}

func comparePasswords(serverPassword, clientPassword string) bool {
	// Check to see if the server password is a bcrypt hash
	if isBcrypt(serverPassword) {
		if err := bcrypt.CompareHashAndPassword([]byte(serverPassword), []byte(clientPassword)); err != nil {
			return false
		}
	} else if serverPassword != clientPassword {
		return false
	}
	return true
}

// Support for bcrypt stored passwords and tokens.
const bcryptPrefix = "$2a$"

// isBcrypt checks whether the given password or token is bcrypted.
func isBcrypt(password string) bool {
	return strings.HasPrefix(password, bcryptPrefix)
}
